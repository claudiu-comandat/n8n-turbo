package nodes

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/transform"
)

type Compression struct{}

type HTML struct{}

type XML struct{}

type Markdown struct{}

type ConvertToFile struct{}

func (Compression) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	operation := firstNonEmptyNode(stringParam(in.Node.Parameters, "operation"), "decompress")
	inputFields := compressionInputFields(in.Node.Parameters, operation)
	field := inputFields[0]
	if field == "" {
		field = "data"
	}
	outputFormat := firstNonEmptyNode(stringParam(in.Node.Parameters, "outputFormat"), "zip")
	outputField := firstNonEmptyNode(stringParam(in.Node.Parameters, "binaryPropertyOutput"), field)
	outputFileName := stringParam(in.Node.Parameters, "fileName")
	outputPrefix := firstNonEmptyNode(stringParam(in.Node.Parameters, "outputPrefix"), "file_")
	inputItems := firstInput(in.InputData)
	result := make([]dataplane.Item, 0, len(inputItems))
	for itemIndex, item := range inputItems {
		next := itemWithPairedIndex(cloneItem(item), itemIndex, true)
		if operation == "compress" && len(item.Binary) > 0 {
			outputs, metadata, err := compressOfficialBinaryFields(ctx, in, item, inputFields, outputFormat, outputField, outputFileName)
			if err != nil {
				return nil, err
			}
			_ = metadata
			next = dataplane.Item{JSON: deepCopySetMap(item.JSON), Binary: outputs, PairedItem: &dataplane.PairedItem{Item: itemIndex}, Error: item.Error}
			result = append(result, next)
			continue
		}
		if operation == "decompress" {
			if binary, ok := firstAvailableBinary(item.Binary, inputFields); ok {
				if in.BinaryStore != nil {
					outputs, err := decompressBinaryFieldsToStore(ctx, in.BinaryStore, binary, outputPrefix)
					if err != nil {
						return nil, err
					}
					next = dataplane.Item{JSON: deepCopySetMap(item.JSON), Binary: outputs, PairedItem: &dataplane.PairedItem{Item: itemIndex}, Error: item.Error}
					result = append(result, next)
					continue
				}
			}
		}
		if binary, ok := item.Binary[field]; ok {
			if in.BinaryStore != nil {
				outBinary, fileName, mimeType, err := transformBinaryInStore(ctx, in.BinaryStore, binary, operation)
				if err == nil {
					if next.Binary == nil {
						next.Binary = map[string]dataplane.Binary{}
					}
					next.Binary[field] = outBinary
					next.JSON["fileName"] = fileName
					next.JSON["mimeType"] = mimeType
					next.JSON["fileSize"] = outBinary.FileSize
					result = append(result, next)
					continue
				}
			}
			payload, err := binarydata.Read(ctx, in.BinaryStore, binary)
			if err != nil {
				return nil, err
			}
			var transformed []byte
			fileName := firstNonEmptyNode(binary.FileName, "data")
			mimeType := binary.MimeType
			if operation == "decompress" {
				reader, err := gzip.NewReader(bytes.NewReader(payload))
				if err == nil {
					transformed, err = io.ReadAll(reader)
					_ = reader.Close()
					if err != nil {
						return nil, err
					}
					fileName = strings.TrimSuffix(fileName, ".gz")
					if fileName == "" {
						fileName = "data"
					}
					mimeType = http.DetectContentType(transformed)
				} else {
					archive, zipErr := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
					if zipErr != nil {
						return nil, err
					}
					var extracted bool
					for _, entry := range archive.File {
						if entry.FileInfo().IsDir() {
							continue
						}
						stream, openErr := entry.Open()
						if openErr != nil {
							return nil, openErr
						}
						transformed, err = io.ReadAll(stream)
						_ = stream.Close()
						if err != nil {
							return nil, err
						}
						fileName = entry.Name
						mimeType = http.DetectContentType(transformed)
						extracted = true
						break
					}
					if !extracted {
						return nil, fmt.Errorf("compression: zip archive is empty")
					}
				}
			} else {
				var buffer bytes.Buffer
				writer := zip.NewWriter(&buffer)
				entryName := fileName
				if entryName == "" {
					entryName = "data"
				}
				zipEntry, err := writer.Create(entryName)
				if err != nil {
					return nil, err
				}
				if _, err := zipEntry.Write(payload); err != nil {
					return nil, err
				}
				if err := writer.Close(); err != nil {
					return nil, err
				}
				transformed = buffer.Bytes()
				if !strings.HasSuffix(strings.ToLower(fileName), ".zip") {
					fileName += ".zip"
				}
				mimeType = "application/zip"
			}
			outBinary := dataplane.Binary{
				Data:          base64.StdEncoding.EncodeToString(transformed),
				MimeType:      mimeType,
				FileName:      fileName,
				FileSize:      int64(len(transformed)),
				FileExtension: strings.TrimPrefix(filepath.Ext(fileName), "."),
			}
			if in.BinaryStore != nil {
				ref, err := in.BinaryStore.Put(ctx, mimeType, fileName, bytes.NewReader(transformed))
				if err != nil {
					return nil, err
				}
				outBinary = binarydata.BinaryFromRef(ref)
				outBinary.FileExtension = strings.TrimPrefix(filepath.Ext(fileName), ".")
			}
			outField := field
			if operation == "decompress" {
				outField = outputPrefix + "0"
			}
			next = dataplane.Item{
				JSON:       deepCopySetMap(item.JSON),
				Binary:     map[string]dataplane.Binary{outField: outBinary},
				PairedItem: &dataplane.PairedItem{Item: itemIndex},
				Error:      item.Error,
			}
			result = append(result, next)
			continue
		}
		value := fmt.Sprint(item.JSON[field])
		if operation == "decompress" {
			decoded, err := base64.StdEncoding.DecodeString(value)
			if err != nil {
				return nil, err
			}
			reader, err := gzip.NewReader(bytes.NewReader(decoded))
			if err != nil {
				return nil, err
			}
			decompressed, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil {
				return nil, err
			}
			next.JSON[field] = string(decompressed)
		} else {
			var buffer bytes.Buffer
			writer := gzip.NewWriter(&buffer)
			if _, err := writer.Write([]byte(value)); err != nil {
				return nil, err
			}
			if err := writer.Close(); err != nil {
				return nil, err
			}
			next.JSON[field] = base64.StdEncoding.EncodeToString(buffer.Bytes())
		}
		result = append(result, next)
	}
	return dataplane.MainOutput(result), nil
}

func compressionInputFields(parameters map[string]any, operation string) []string {
	raw := firstNonEmptyNode(
		stringParam(parameters, "inputBinaryFieldNames"),
		stringParam(parameters, "binaryPropertyName"),
		stringParam(parameters, "inputBinaryPropertyName"),
		stringParam(parameters, "fieldName"),
	)
	if raw == "" {
		raw = "data"
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		result = append(result, raw)
	}
	return result
}

func compressOfficialBinaryFields(ctx context.Context, in engine.ExecuteInput, item dataplane.Item, inputFields []string, outputFormat string, outputField string, outputFileName string) (map[string]dataplane.Binary, map[string]any, error) {
	outputFormat = strings.ToLower(firstNonEmptyNode(outputFormat, "zip"))
	if outputField == "" {
		outputField = "data"
	}
	switch outputFormat {
	case "gzip", "gz":
		return gzipOfficialBinaryFields(ctx, in, item, inputFields, outputField, outputFileName)
	default:
		return zipOfficialBinaryFields(ctx, in, item, inputFields, outputField, outputFileName)
	}
}

func zipOfficialBinaryFields(ctx context.Context, in engine.ExecuteInput, item dataplane.Item, inputFields []string, outputField string, outputFileName string) (map[string]dataplane.Binary, map[string]any, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	count := 0
	for _, field := range inputFields {
		binary, ok := item.Binary[field]
		if !ok {
			continue
		}
		payload, err := binarydata.Read(ctx, in.BinaryStore, binary)
		if err != nil {
			_ = writer.Close()
			return nil, nil, err
		}
		entryName := firstNonEmptyNode(binary.FileName, field)
		entry, err := writer.Create(entryName)
		if err != nil {
			_ = writer.Close()
			return nil, nil, err
		}
		if _, err := entry.Write(payload); err != nil {
			_ = writer.Close()
			return nil, nil, err
		}
		count++
	}
	if count == 0 {
		_ = writer.Close()
		return nil, nil, fmt.Errorf("compression: none of the input binary fields were found")
	}
	if err := writer.Close(); err != nil {
		return nil, nil, err
	}
	fileName := firstNonEmptyNode(outputFileName, "data.zip")
	if !strings.HasSuffix(strings.ToLower(fileName), ".zip") {
		fileName += ".zip"
	}
	binary, err := convertOutputBinary(ctx, in, buffer.Bytes(), "application/zip", fileName)
	if err != nil {
		return nil, nil, err
	}
	return map[string]dataplane.Binary{outputField: binary}, map[string]any{"fileName": fileName, "mimeType": "application/zip", "fileSize": int64(buffer.Len())}, nil
}

func gzipOfficialBinaryFields(ctx context.Context, in engine.ExecuteInput, item dataplane.Item, inputFields []string, outputField string, outputFileName string) (map[string]dataplane.Binary, map[string]any, error) {
	outputs := map[string]dataplane.Binary{}
	index := 0
	var lastName string
	var lastSize int64
	for _, field := range inputFields {
		binary, ok := item.Binary[field]
		if !ok {
			continue
		}
		payload, err := binarydata.Read(ctx, in.BinaryStore, binary)
		if err != nil {
			return nil, nil, err
		}
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		if _, err := writer.Write(payload); err != nil {
			_ = writer.Close()
			return nil, nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, nil, err
		}
		fileName := gzipOutputFileName(binary, outputFileName)
		property := outputField
		if index > 0 {
			property = fmt.Sprintf("%s%d", outputField, index)
		}
		out, err := convertOutputBinary(ctx, in, buffer.Bytes(), "application/gzip", fileName)
		if err != nil {
			return nil, nil, err
		}
		outputs[property] = out
		lastName = fileName
		lastSize = int64(buffer.Len())
		index++
	}
	if len(outputs) == 0 {
		return nil, nil, fmt.Errorf("compression: none of the input binary fields were found")
	}
	return outputs, map[string]any{"fileName": lastName, "mimeType": "application/gzip", "fileSize": lastSize}, nil
}

func gzipOutputFileName(binary dataplane.Binary, explicit string) string {
	if explicit != "" {
		explicit = strings.TrimSuffix(strings.TrimSuffix(explicit, ".gzip"), ".gz")
	}
	base := firstNonEmptyNode(explicit, binary.FileName, "data")
	if !strings.HasSuffix(strings.ToLower(base), ".gz") && !strings.HasSuffix(strings.ToLower(base), ".gzip") {
		base += ".gz"
	}
	return base
}

func firstAvailableBinary(binary map[string]dataplane.Binary, fields []string) (dataplane.Binary, bool) {
	if len(binary) == 0 {
		return dataplane.Binary{}, false
	}
	for _, field := range fields {
		if value, ok := binary[field]; ok {
			return value, true
		}
	}
	for _, value := range binary {
		return value, true
	}
	return dataplane.Binary{}, false
}

func transformBinaryInStore(ctx context.Context, store binarydata.Store, binary dataplane.Binary, operation string) (dataplane.Binary, string, string, error) {
	switch operation {
	case "decompress":
		return decompressBinaryToStore(ctx, store, binary)
	default:
		return compressBinaryToStore(ctx, store, binary)
	}
}

func compressBinaryToStore(ctx context.Context, store binarydata.Store, binary dataplane.Binary) (dataplane.Binary, string, string, error) {
	reader, err := binarydata.Open(ctx, store, binary)
	if err != nil {
		return dataplane.Binary{}, "", "", err
	}
	fileName := firstNonEmptyNode(binary.FileName, "data")
	entryName := fileName
	if entryName == "" {
		entryName = "data"
	}
	outName := fileName
	if !strings.HasSuffix(strings.ToLower(outName), ".zip") {
		outName += ".zip"
	}
	if outName == "" {
		outName = "data.zip"
	}
	pipeReader, pipeWriter := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		defer reader.Close()
		writer := zip.NewWriter(pipeWriter)
		entry, err := writer.Create(entryName)
		if err == nil {
			_, err = io.Copy(entry, reader)
		}
		closeErr := writer.Close()
		if err == nil {
			err = closeErr
		}
		errCh <- err
		_ = pipeWriter.CloseWithError(err)
	}()
	ref, err := store.Put(ctx, "application/zip", outName, pipeReader)
	_ = pipeReader.Close()
	if err == nil {
		err = <-errCh
	} else {
		<-errCh
	}
	if err != nil {
		return dataplane.Binary{}, "", "", err
	}
	outBinary := binarydata.BinaryFromRef(ref)
	outBinary.FileExtension = strings.TrimPrefix(filepath.Ext(outName), ".")
	return outBinary, outName, "application/zip", nil
}

func decompressBinaryToStore(ctx context.Context, store binarydata.Store, binary dataplane.Binary) (dataplane.Binary, string, string, error) {
	if isGzipBinary(binary) {
		outBinary, fileName, mimeType, err := gunzipBinaryToStore(ctx, store, binary)
		if err == nil {
			return outBinary, fileName, mimeType, nil
		}
		if looksLikeZipBinary(binary) {
			return unzipBinaryToStore(ctx, store, binary)
		}
		return dataplane.Binary{}, "", "", err
	}
	return unzipBinaryToStore(ctx, store, binary)
}

func decompressBinaryFieldsToStore(ctx context.Context, store binarydata.Store, binary dataplane.Binary, outputPrefix string) (map[string]dataplane.Binary, error) {
	if outputPrefix == "" {
		outputPrefix = "file_"
	}
	if isGzipBinary(binary) && !looksLikeZipBinary(binary) {
		outBinary, _, _, err := gunzipBinaryToStore(ctx, store, binary)
		if err != nil {
			return nil, err
		}
		return map[string]dataplane.Binary{outputPrefix + "0": outBinary}, nil
	}
	reader, err := binarydata.Open(ctx, store, binary)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	file, err := os.CreateTemp("", "n8n-turbo-zip-*")
	if err != nil {
		return nil, err
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(file, reader); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	archive, err := zip.OpenReader(tmpPath)
	if err != nil {
		if isGzipBinary(binary) {
			outBinary, _, _, gzipErr := gunzipBinaryToStore(ctx, store, binary)
			if gzipErr == nil {
				return map[string]dataplane.Binary{outputPrefix + "0": outBinary}, nil
			}
		}
		return nil, err
	}
	defer archive.Close()
	outputs := map[string]dataplane.Binary{}
	index := 0
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		stream, err := entry.Open()
		if err != nil {
			return nil, err
		}
		fileName := firstNonEmptyNode(entry.Name, fmt.Sprintf("file-%d", index))
		mimeType := mimeTypeFromFileName(fileName, "application/octet-stream")
		ref, putErr := store.Put(ctx, mimeType, fileName, stream)
		closeErr := stream.Close()
		if putErr == nil {
			putErr = closeErr
		}
		if putErr != nil {
			return nil, putErr
		}
		outBinary := binarydata.BinaryFromRef(ref)
		outBinary.FileExtension = strings.TrimPrefix(filepath.Ext(fileName), ".")
		outputs[fmt.Sprintf("%s%d", outputPrefix, index)] = outBinary
		index++
	}
	if len(outputs) == 0 {
		return nil, fmt.Errorf("compression: zip archive is empty")
	}
	return outputs, nil
}

func gunzipBinaryToStore(ctx context.Context, store binarydata.Store, binary dataplane.Binary) (dataplane.Binary, string, string, error) {
	reader, err := binarydata.Open(ctx, store, binary)
	if err != nil {
		return dataplane.Binary{}, "", "", err
	}
	defer reader.Close()
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return dataplane.Binary{}, "", "", err
	}
	defer gzipReader.Close()
	fileName := strings.TrimSuffix(firstNonEmptyNode(binary.FileName, "data"), ".gz")
	if fileName == "" {
		fileName = "data"
	}
	mimeType := mimeTypeFromFileName(fileName, firstNonEmptyNode(binary.MimeType, "application/octet-stream"))
	ref, err := store.Put(ctx, mimeType, fileName, gzipReader)
	if err != nil {
		return dataplane.Binary{}, "", "", err
	}
	outBinary := binarydata.BinaryFromRef(ref)
	outBinary.FileExtension = strings.TrimPrefix(filepath.Ext(fileName), ".")
	return outBinary, fileName, mimeType, nil
}

func unzipBinaryToStore(ctx context.Context, store binarydata.Store, binary dataplane.Binary) (dataplane.Binary, string, string, error) {
	reader, err := binarydata.Open(ctx, store, binary)
	if err != nil {
		return dataplane.Binary{}, "", "", err
	}
	defer reader.Close()
	file, err := os.CreateTemp("", "n8n-turbo-zip-*")
	if err != nil {
		return dataplane.Binary{}, "", "", err
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	written, err := io.Copy(file, reader)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return dataplane.Binary{}, "", "", err
	}
	archive, err := zip.OpenReader(tmpPath)
	if err != nil {
		return dataplane.Binary{}, "", "", err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		stream, err := entry.Open()
		if err != nil {
			return dataplane.Binary{}, "", "", err
		}
		fileName := firstNonEmptyNode(entry.Name, strings.TrimSuffix(firstNonEmptyNode(binary.FileName, "data"), ".zip"))
		mimeType := mimeTypeFromFileName(fileName, "application/octet-stream")
		ref, putErr := store.Put(ctx, mimeType, fileName, stream)
		closeStreamErr := stream.Close()
		if putErr == nil {
			putErr = closeStreamErr
		}
		if putErr != nil {
			return dataplane.Binary{}, "", "", putErr
		}
		outBinary := binarydata.BinaryFromRef(ref)
		outBinary.FileExtension = strings.TrimPrefix(filepath.Ext(fileName), ".")
		return outBinary, fileName, mimeType, nil
	}
	if written == 0 {
		return dataplane.Binary{}, "", "", fmt.Errorf("compression: zip archive is empty")
	}
	return dataplane.Binary{}, "", "", fmt.Errorf("compression: zip archive is empty")
}

func isGzipBinary(binary dataplane.Binary) bool {
	name := strings.ToLower(firstNonEmptyNode(binary.FileName, ""))
	mimeType := strings.ToLower(binary.MimeType)
	return strings.HasSuffix(name, ".gz") || strings.Contains(mimeType, "gzip")
}

func looksLikeZipBinary(binary dataplane.Binary) bool {
	name := strings.ToLower(firstNonEmptyNode(binary.FileName, ""))
	mimeType := strings.ToLower(binary.MimeType)
	return strings.HasSuffix(name, ".zip") || strings.Contains(mimeType, "zip")
}

func mimeTypeFromFileName(fileName string, fallback string) string {
	if detected := mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName))); detected != "" {
		return detected
	}
	return fallback
}

func (HTML) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return executeHTMLNode(ctx, in)
}

func (XML) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return executeXMLNode(ctx, in)
}

func (Markdown) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return executeMarkdownNode(ctx, in)
}

func (ConvertToFile) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	operation := normalizeConvertToFileOperation(firstNonEmptyNode(stringParam(in.Node.Parameters, "operation"), "csv"))
	binaryProperty := stringParam(in.Node.Parameters, "binaryPropertyName", "binaryProperty", "dataPropertyName")
	if binaryProperty == "" {
		binaryProperty = "data"
	}
	items := firstInput(in.InputData)
	switch operation {
	case "tojson":
		return convertToFileOfficialJSON(ctx, in, items, binaryProperty)
	case "totext":
		return convertToFileOfficialText(ctx, in, items, binaryProperty)
	case "tobinary":
		return convertToFileOfficialBinary(ctx, in, items, binaryProperty)
	case "ical":
		return convertToFileOfficialICal(ctx, in, items, binaryProperty)
	}
	if operation == "binary" {
		return convertToFileBinary(ctx, in, items, binaryProperty)
	}
	jsonItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		jsonItems = append(jsonItems, item.JSON)
	}
	content, mimeType, defaultFileName, err := convertItemsToFile(operation, jsonItems, in.Node.Parameters)
	if err != nil {
		return nil, err
	}
	fileName := firstNonEmptyNode(stringParam(in.Node.Parameters, "outputFileName", "fileName"), defaultFileName)
	if explicitMime := stringParam(in.Node.Parameters, "mimeType"); explicitMime != "" {
		mimeType = explicitMime
	}
	binary := dataplane.Binary{Data: base64.StdEncoding.EncodeToString(content), MimeType: mimeType, FileName: fileName, FileSize: int64(len(content)), FileExtension: strings.TrimPrefix(filepath.Ext(fileName), ".")}
	if in.BinaryStore != nil {
		ref, err := in.BinaryStore.Put(ctx, mimeType, fileName, bytes.NewReader(content))
		if err != nil {
			return nil, err
		}
		binary = binarydata.BinaryFromRef(ref)
		binary.FileExtension = strings.TrimPrefix(filepath.Ext(fileName), ".")
	}
	output := dataplane.Item{
		JSON: map[string]any{"fileName": fileName, "mimeType": mimeType, "fileSize": int64(len(content))},
		Binary: map[string]dataplane.Binary{
			binaryProperty: binary,
		},
	}
	return dataplane.MainOutput([]dataplane.Item{output}), nil
}

func normalizeConvertToFileOperation(operation string) string {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "tojson":
		return "tojson"
	case "totext":
		return "totext"
	case "tobinary":
		return "tobinary"
	case "ical":
		return "ical"
	case "xls":
		return "xlsx"
	default:
		return strings.ToLower(strings.TrimSpace(operation))
	}
}

func convertToFileOfficialJSON(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item, binaryProperty string) (dataplane.Output, error) {
	mode := firstNonEmptyNode(stringParam(in.Node.Parameters, "mode"), "once")
	options := convertFileOptions(in.Node.Parameters, "jsonOptions")
	if mode == "each" {
		result := make([]dataplane.Item, 0, len(items))
		for index, item := range items {
			content, err := convertOfficialJSONBytes(item.JSON, options)
			if err != nil {
				return nil, err
			}
			binary, err := convertOutputBinary(ctx, in, content, "application/json", firstNonEmptyNode(stringParam(options, "fileName"), "file.json"))
			if err != nil {
				return nil, err
			}
			result = append(result, dataplane.Item{JSON: map[string]any{}, Binary: map[string]dataplane.Binary{binaryProperty: binary}, PairedItem: &dataplane.PairedItem{Item: index}})
		}
		return dataplane.MainOutput(result), nil
	}
	jsonItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		jsonItems = append(jsonItems, item.JSON)
	}
	content, err := convertOfficialJSONBytes(jsonItems, options)
	if err != nil {
		return nil, err
	}
	binary, err := convertOutputBinary(ctx, in, content, "application/json", firstNonEmptyNode(stringParam(options, "fileName"), "file.json"))
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{}, Binary: map[string]dataplane.Binary{binaryProperty: binary}, PairedItem: &dataplane.PairedItem{Item: 0}}}), nil
}

func convertOfficialJSONBytes(value any, options map[string]any) ([]byte, error) {
	var content []byte
	var err error
	if boolParam(options, "format", boolParam(options, "indent", false)) {
		content, err = json.MarshalIndent(value, "", "  ")
	} else {
		content, err = json.Marshal(value)
	}
	if err != nil {
		return nil, err
	}
	if boolParam(options, "addBOM", false) {
		content = append([]byte{0xEF, 0xBB, 0xBF}, content...)
	}
	return encodeOutputBytes(content, stringParam(options, "encoding"))
}

func convertToFileOfficialText(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item, binaryProperty string) (dataplane.Output, error) {
	options := convertFileOptions(in.Node.Parameters, "textOptions")
	sourceProperty := stringParam(in.Node.Parameters, "sourceProperty", "fieldName")
	result := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		value := convertJSONFieldValue(item.JSON, sourceProperty)
		content, err := encodeOutputBytes([]byte(fmt.Sprint(value)), stringParam(options, "encoding"))
		if err != nil {
			return nil, err
		}
		if boolParam(options, "addBOM", false) {
			content = append([]byte{0xEF, 0xBB, 0xBF}, content...)
		}
		binary, err := convertOutputBinary(ctx, in, content, "text/plain", firstNonEmptyNode(stringParam(options, "fileName"), "file.txt"))
		if err != nil {
			return nil, err
		}
		result = append(result, dataplane.Item{JSON: map[string]any{}, Binary: map[string]dataplane.Binary{binaryProperty: binary}, PairedItem: &dataplane.PairedItem{Item: index}})
	}
	return dataplane.MainOutput(result), nil
}

func convertToFileOfficialBinary(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item, binaryProperty string) (dataplane.Output, error) {
	options := convertFileOptions(in.Node.Parameters, "binaryOptions")
	sourceProperty := stringParam(in.Node.Parameters, "sourceProperty", "fieldName")
	dataIsBase64 := true
	if in.Node.TypeVersion == 1 {
		dataIsBase64 = boolParam(options, "dataIsBase64", true)
	}
	result := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		value := fmt.Sprint(convertJSONFieldValue(item.JSON, sourceProperty))
		var content []byte
		var err error
		if dataIsBase64 {
			content, err = base64.StdEncoding.DecodeString(value)
			if err != nil {
				return nil, fmt.Errorf("convertToFile toBinary: decode base64: %w", err)
			}
		} else {
			content, err = encodeOutputBytes([]byte(value), stringParam(options, "encoding"))
			if err != nil {
				return nil, err
			}
			if boolParam(options, "addBOM", false) {
				content = append([]byte{0xEF, 0xBB, 0xBF}, content...)
			}
		}
		binary, err := convertOutputBinary(ctx, in, content, firstNonEmptyNode(stringParam(options, "mimeType"), "application/octet-stream"), firstNonEmptyNode(stringParam(options, "fileName"), "file.bin"))
		if err != nil {
			return nil, err
		}
		result = append(result, dataplane.Item{JSON: map[string]any{}, Binary: map[string]dataplane.Binary{binaryProperty: binary}, PairedItem: &dataplane.PairedItem{Item: index}})
	}
	return dataplane.MainOutput(result), nil
}

func convertToFileOfficialICal(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item, binaryProperty string) (dataplane.Output, error) {
	options := convertFileOptions(in.Node.Parameters, "icalOptions")
	result := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		content := []byte(convertItemToICal(item.JSON))
		binary, err := convertOutputBinary(ctx, in, content, "text/calendar", firstNonEmptyNode(stringParam(options, "fileName"), "event.ics"))
		if err != nil {
			return nil, err
		}
		result = append(result, dataplane.Item{JSON: map[string]any{}, Binary: map[string]dataplane.Binary{binaryProperty: binary}, PairedItem: &dataplane.PairedItem{Item: index}})
	}
	return dataplane.MainOutput(result), nil
}

func convertItemToICal(item map[string]any) string {
	summary := firstNonEmptyNode(fmt.Sprint(convertJSONFieldValue(item, "summary")), fmt.Sprint(convertJSONFieldValue(item, "title")), "Event")
	start := firstNonEmptyNode(fmt.Sprint(convertJSONFieldValue(item, "start")), fmt.Sprint(convertJSONFieldValue(item, "startDate")))
	end := firstNonEmptyNode(fmt.Sprint(convertJSONFieldValue(item, "end")), fmt.Sprint(convertJSONFieldValue(item, "endDate")))
	uid := firstNonEmptyNode(fmt.Sprint(convertJSONFieldValue(item, "uid")), fmt.Sprintf("%s@n8n-turbo", strings.ReplaceAll(summary, " ", "-")))
	var builder strings.Builder
	builder.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//n8n-turbo//EN\r\nBEGIN:VEVENT\r\n")
	builder.WriteString("UID:" + escapeICalText(uid) + "\r\n")
	builder.WriteString("SUMMARY:" + escapeICalText(summary) + "\r\n")
	if start != "" && start != "<nil>" {
		builder.WriteString("DTSTART:" + formatICalDate(start) + "\r\n")
	}
	if end != "" && end != "<nil>" {
		builder.WriteString("DTEND:" + formatICalDate(end) + "\r\n")
	}
	builder.WriteString("END:VEVENT\r\nEND:VCALENDAR\r\n")
	return builder.String()
}

func escapeICalText(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, ",", `\,`)
	value = strings.ReplaceAll(value, ";", `\;`)
	return value
}

func formatICalDate(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format("20060102T150405Z")
	}
	return strings.NewReplacer("-", "", ":", "", " ", "T").Replace(value)
}

func convertJSONFieldValue(item map[string]any, field string) any {
	if field == "" {
		return item
	}
	if strings.Contains(field, ".") {
		return nestedMergeValue(item, field)
	}
	return item[field]
}

func convertOutputBinary(ctx context.Context, in engine.ExecuteInput, content []byte, mimeType string, fileName string) (dataplane.Binary, error) {
	binary := dataplane.Binary{Data: base64.StdEncoding.EncodeToString(content), MimeType: mimeType, FileName: fileName, FileSize: int64(len(content)), FileExtension: strings.TrimPrefix(filepath.Ext(fileName), ".")}
	if in.BinaryStore == nil {
		return binary, nil
	}
	ref, err := in.BinaryStore.Put(ctx, mimeType, fileName, bytes.NewReader(content))
	if err != nil {
		return dataplane.Binary{}, err
	}
	binary = binarydata.BinaryFromRef(ref)
	binary.FileExtension = strings.TrimPrefix(filepath.Ext(fileName), ".")
	return binary, nil
}

func convertItemsToFile(operation string, items []map[string]any, params map[string]any) ([]byte, string, string, error) {
	switch operation {
	case "csv":
		content, err := convertToCSV(items, params)
		return content, "text/csv; charset=UTF-8", "export.csv", err
	case "ods":
		content, err := convertToODS(items, params)
		return content, "application/vnd.oasis.opendocument.spreadsheet", "export.ods", err
	case "xlsx":
		content, err := convertToXLSX(items, params)
		return content, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "export.xlsx", err
	case "html":
		content, err := convertToHTML(items, params)
		return content, "text/html; charset=UTF-8", "export.html", err
	case "rtf":
		content, err := convertToRTF(items, params)
		return content, "application/rtf", "export.rtf", err
	case "text", "txt":
		content, err := convertToText(items, params)
		return content, "text/plain; charset=UTF-8", "export.txt", err
	case "json":
		content, err := convertToJSON(items, params)
		return content, "application/json", "export.json", err
	default:
		return nil, "", "", fmt.Errorf("convertToFile: unsupported operation %s", operation)
	}
}

func convertToCSV(items []map[string]any, params map[string]any) ([]byte, error) {
	options := convertFileOptions(params, "csvOptions")
	delimiter := firstNonEmptyNode(stringParam(options, "delimiter"), stringParam(params, "delimiter"), ",")
	if delimiter == "\\t" {
		delimiter = "\t"
	}
	includeHeader := boolParam(options, "includeHeader", boolParam(params, "includeHeader", true))
	bom := boolParam(options, "bom", boolParam(params, "bom", false))
	quoteAll := boolParam(options, "quoteAllFields", boolParam(params, "quoteAllFields", false))
	lineTerminator := firstNonEmptyNode(stringParam(options, "lineTerminator"), stringParam(params, "lineTerminator"), "\n")
	if lineTerminator == "\\r\\n" {
		lineTerminator = "\r\n"
	}
	emptyValue := firstNonEmptyNode(stringParam(options, "emptyFieldValue"), stringParam(params, "emptyFieldValue"))
	headers := collectConvertKeys(items)
	var buffer bytes.Buffer
	if bom {
		buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	}
	rows := make([][]string, 0, len(items)+1)
	if includeHeader {
		rows = append(rows, headers)
	}
	for _, item := range items {
		row := make([]string, len(headers))
		for index, key := range headers {
			row[index] = formatConvertValue(item[key], emptyValue)
		}
		rows = append(rows, row)
	}
	if quoteAll {
		for _, row := range rows {
			writeQuotedCSVRow(&buffer, row, delimiter, lineTerminator)
		}
	} else {
		writer := csv.NewWriter(&buffer)
		writer.Comma = []rune(delimiter)[0]
		writer.UseCRLF = lineTerminator == "\r\n"
		for _, row := range rows {
			if err := writer.Write(row); err != nil {
				return nil, fmt.Errorf("csv: write row: %w", err)
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, fmt.Errorf("csv: flush: %w", err)
		}
	}
	return encodeOutputBytes(buffer.Bytes(), firstNonEmptyNode(stringParam(options, "encoding"), stringParam(params, "encoding")))
}

func convertToXLSX(items []map[string]any, params map[string]any) ([]byte, error) {
	options := convertFileOptions(params, "xlsxOptions")
	sheetName := firstNonEmptyNode(stringParam(options, "sheetName"), stringParam(params, "sheetName"), "Sheet1")
	headerRow := boolParam(options, "headerRow", boolParam(params, "headerRow", true))
	autoFilter := boolParam(options, "autoFilter", boolParam(params, "autoFilter", false))
	freezePanes := boolParam(options, "freezePanes", boolParam(params, "freezePanes", false))
	file := excelize.NewFile()
	defer file.Close()
	defaultSheet := file.GetSheetName(0)
	if err := file.SetSheetName(defaultSheet, sheetName); err != nil {
		return nil, fmt.Errorf("xlsx: set sheet name: %w", err)
	}
	headers := collectConvertKeys(items)
	headerStyle, _ := file.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#D9E1F2"}}})
	rowIndex := 1
	if headerRow {
		for columnIndex, key := range headers {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex)
			_ = file.SetCellValue(sheetName, cell, key)
			if headerStyle != 0 {
				_ = file.SetCellStyle(sheetName, cell, cell, headerStyle)
			}
		}
		rowIndex++
	}
	for _, item := range items {
		for columnIndex, key := range headers {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex)
			setConvertXLSXCell(file, sheetName, cell, item[key])
		}
		rowIndex++
	}
	if len(headers) > 0 {
		lastColumn, _ := excelize.ColumnNumberToName(len(headers))
		if autoFilter && headerRow {
			_ = file.AutoFilter(sheetName, fmt.Sprintf("A1:%s1", lastColumn), []excelize.AutoFilterOptions{})
		}
		if freezePanes && headerRow {
			_ = file.SetPanes(sheetName, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
		}
		for columnIndex, key := range headers {
			column, _ := excelize.ColumnNumberToName(columnIndex + 1)
			width := len(key) + 2
			for _, item := range items {
				length := len(formatConvertValue(item[key], ""))
				if length+2 > width {
					width = length + 2
				}
			}
			if width > 52 {
				width = 52
			}
			_ = file.SetColWidth(sheetName, column, column, float64(width))
		}
	}
	var buffer bytes.Buffer
	if err := file.Write(&buffer); err != nil {
		return nil, fmt.Errorf("xlsx: write workbook: %w", err)
	}
	return buffer.Bytes(), nil
}

func convertToODS(items []map[string]any, params map[string]any) ([]byte, error) {
	options := convertFileOptions(params, "odsOptions")
	sheetName := firstNonEmptyNode(stringParam(options, "sheetName"), stringParam(params, "sheetName"), "Sheet1")
	includeHeader := boolParam(options, "includeHeader", boolParam(params, "includeHeader", true))
	headers := collectConvertKeys(items)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	mimeHeader := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	mime, err := writer.CreateHeader(mimeHeader)
	if err != nil {
		return nil, err
	}
	if _, err := mime.Write([]byte("application/vnd.oasis.opendocument.spreadsheet")); err != nil {
		return nil, err
	}
	if err := writeZipFile(writer, "META-INF/manifest.xml", odsManifestXML()); err != nil {
		return nil, err
	}
	if err := writeZipFile(writer, "content.xml", odsContentXML(sheetName, headers, items, includeHeader)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeZipFile(writer *zip.Writer, name string, content string) error {
	file, err := writer.Create(name)
	if err != nil {
		return err
	}
	_, err = file.Write([]byte(content))
	return err
}

func odsManifestXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.2">` +
		`<manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.spreadsheet"/>` +
		`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>` +
		`</manifest:manifest>`
}

func odsContentXML(sheetName string, headers []string, items []map[string]any, includeHeader bool) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	builder.WriteString(`<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:officeooo="http://openoffice.org/2009/office" office:version="1.2">`)
	builder.WriteString(`<office:body><office:spreadsheet><table:table table:name="`)
	builder.WriteString(html.EscapeString(sheetName))
	builder.WriteString(`">`)
	if includeHeader {
		odsWriteRow(&builder, headers)
	}
	for _, item := range items {
		values := make([]string, 0, len(headers))
		for _, key := range headers {
			values = append(values, formatConvertValue(item[key], ""))
		}
		odsWriteRow(&builder, values)
	}
	builder.WriteString(`</table:table></office:spreadsheet></office:body></office:document-content>`)
	return builder.String()
}

func odsWriteRow(builder *strings.Builder, values []string) {
	builder.WriteString(`<table:table-row>`)
	for _, value := range values {
		builder.WriteString(`<table:table-cell office:value-type="string"><text:p>`)
		builder.WriteString(html.EscapeString(value))
		builder.WriteString(`</text:p></table:table-cell>`)
	}
	builder.WriteString(`</table:table-row>`)
}

func convertToRTF(items []map[string]any, params map[string]any) ([]byte, error) {
	options := convertFileOptions(params, "rtfOptions")
	headers := collectConvertKeys(items)
	includeHeader := boolParam(options, "includeHeader", boolParam(params, "includeHeader", true))
	var builder strings.Builder
	builder.WriteString(`{\rtf1\ansi\deff0`)
	if includeHeader && len(headers) > 0 {
		builder.WriteString(`\b `)
		builder.WriteString(rtfEscape(strings.Join(headers, "\t")))
		builder.WriteString(`\b0\par `)
	}
	for _, item := range items {
		values := make([]string, 0, len(headers))
		for _, key := range headers {
			values = append(values, formatConvertValue(item[key], ""))
		}
		builder.WriteString(rtfEscape(strings.Join(values, "\t")))
		builder.WriteString(`\par `)
	}
	builder.WriteString(`}`)
	return []byte(builder.String()), nil
}

func rtfEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `{`, `\{`)
	value = strings.ReplaceAll(value, `}`, `\}`)
	value = strings.ReplaceAll(value, "\n", `\line `)
	return value
}

func convertToHTML(items []map[string]any, params map[string]any) ([]byte, error) {
	options := convertFileOptions(params, "htmlOptions")
	customTemplate := firstNonEmptyNode(stringParam(options, "template"), stringParam(params, "template"))
	title := firstNonEmptyNode(stringParam(options, "title"), stringParam(params, "title"), "Export")
	if customTemplate != "" {
		tmpl, err := template.New("convertToFile").Funcs(template.FuncMap{"escape": html.EscapeString}).Parse(customTemplate)
		if err != nil {
			return nil, fmt.Errorf("html: parse template: %w", err)
		}
		var buffer bytes.Buffer
		if err := tmpl.Execute(&buffer, map[string]any{"items": items, "count": len(items), "title": title}); err != nil {
			return nil, fmt.Errorf("html: execute template: %w", err)
		}
		return buffer.Bytes(), nil
	}
	charset := firstNonEmptyNode(stringParam(options, "charset"), stringParam(params, "charset"), "UTF-8")
	tableClass := firstNonEmptyNode(stringParam(options, "tableClass"), stringParam(params, "tableClass"), "n8n-table")
	headers := collectConvertKeys(items)
	var buffer bytes.Buffer
	buffer.WriteString("<!DOCTYPE html>\n<html><head><meta charset=\"")
	buffer.WriteString(html.EscapeString(charset))
	buffer.WriteString("\"><title>")
	buffer.WriteString(html.EscapeString(title))
	buffer.WriteString("</title></head><body><h1>")
	buffer.WriteString(html.EscapeString(title))
	buffer.WriteString("</h1><table class=\"")
	buffer.WriteString(html.EscapeString(tableClass))
	buffer.WriteString("\"><thead><tr>")
	for _, key := range headers {
		buffer.WriteString("<th>")
		buffer.WriteString(html.EscapeString(key))
		buffer.WriteString("</th>")
	}
	buffer.WriteString("</tr></thead><tbody>")
	for _, item := range items {
		buffer.WriteString("<tr>")
		for _, key := range headers {
			buffer.WriteString("<td>")
			buffer.WriteString(html.EscapeString(formatConvertValue(item[key], "")))
			buffer.WriteString("</td>")
		}
		buffer.WriteString("</tr>")
	}
	buffer.WriteString("</tbody></table></body></html>")
	return buffer.Bytes(), nil
}

func convertToText(items []map[string]any, params map[string]any) ([]byte, error) {
	options := convertFileOptions(params, "textOptions")
	fieldName := firstNonEmptyNode(stringParam(options, "fieldName"), stringParam(params, "fieldName"))
	separator := firstNonEmptyNode(stringParam(options, "separator"), stringParam(params, "separator"), "\n")
	if separator == "\\n" {
		separator = "\n"
	}
	parts := make([]string, 0, len(items))
	for index, item := range items {
		if fieldName != "" {
			value, ok := item[fieldName]
			if !ok {
				return nil, fmt.Errorf("text: field %s missing in item %d", fieldName, index)
			}
			parts = append(parts, fmt.Sprint(value))
			continue
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("text: encode item %d: %w", index, err)
		}
		parts = append(parts, string(encoded))
	}
	return encodeOutputBytes([]byte(strings.Join(parts, separator)), firstNonEmptyNode(stringParam(options, "encoding"), stringParam(params, "encoding")))
}

func convertToJSON(items []map[string]any, params map[string]any) ([]byte, error) {
	options := convertFileOptions(params, "jsonOptions")
	indent := boolParam(options, "indent", boolParam(params, "indent", false))
	wrapInArray := boolParam(options, "wrapInArray", boolParam(params, "wrapInArray", true))
	var data any = items
	if !wrapInArray && len(items) == 1 {
		data = items[0]
	}
	if indent {
		return json.MarshalIndent(data, "", "  ")
	}
	return json.Marshal(data)
}

func convertToFileBinary(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item, outputProperty string) (dataplane.Output, error) {
	sourceProperty := firstNonEmptyNode(stringParam(in.Node.Parameters, "sourceBinaryPropertyName", "inputBinaryPropertyName"), "data")
	fileName := stringParam(in.Node.Parameters, "outputFileName", "fileName")
	result := make([]dataplane.Item, 0, len(items))
	for _, item := range items {
		binary, ok := item.Binary[sourceProperty]
		if !ok {
			return nil, fmt.Errorf("convertToFile: binary property %s not found", sourceProperty)
		}
		if fileName != "" {
			binary.FileName = fileName
			binary.FileExtension = strings.TrimPrefix(filepath.Ext(fileName), ".")
		}
		if in.BinaryStore != nil && binary.ID == "" {
			data, err := binarydata.Read(ctx, in.BinaryStore, binary)
			if err != nil {
				return nil, err
			}
			ref, err := in.BinaryStore.Put(ctx, firstNonEmptyNode(binary.MimeType, "application/octet-stream"), firstNonEmptyNode(binary.FileName, "data.bin"), bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			binary = binarydata.BinaryFromRef(ref)
		}
		next := cloneItem(item)
		next.Binary = cloneBinaryMap(next.Binary)
		next.Binary[outputProperty] = binary
		next.JSON["fileName"] = binary.FileName
		next.JSON["mimeType"] = binary.MimeType
		next.JSON["fileSize"] = binary.FileSize
		result = append(result, next)
	}
	return dataplane.MainOutput(result), nil
}

func collectConvertKeys(items []map[string]any) []string {
	seen := map[string]bool{}
	for _, item := range items {
		for key := range item {
			seen[key] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatConvertValue(value any, emptyValue string) string {
	if value == nil {
		return emptyValue
	}
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return strconvFormatInt(int64(typed))
	case int64:
		return strconvFormatInt(typed)
	case float64:
		return strconvFormatFloat(typed)
	case []any, map[string]any:
		encoded, err := json.Marshal(typed)
		if err == nil {
			return string(encoded)
		}
	}
	return fmt.Sprint(value)
}

func strconvFormatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func strconvFormatFloat(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func writeQuotedCSVRow(buffer *bytes.Buffer, row []string, delimiter string, lineTerminator string) {
	for index, value := range row {
		if index > 0 {
			buffer.WriteString(delimiter)
		}
		buffer.WriteString(`"`)
		buffer.WriteString(strings.ReplaceAll(value, `"`, `""`))
		buffer.WriteString(`"`)
	}
	buffer.WriteString(lineTerminator)
}

func setConvertXLSXCell(file *excelize.File, sheet string, cell string, value any) {
	switch typed := value.(type) {
	case nil:
		_ = file.SetCellValue(sheet, cell, "")
	case string:
		_ = file.SetCellValue(sheet, cell, typed)
	case bool:
		_ = file.SetCellBool(sheet, cell, typed)
	case int:
		_ = file.SetCellInt(sheet, cell, int64(typed))
	case int64:
		_ = file.SetCellInt(sheet, cell, typed)
	case float64:
		_ = file.SetCellFloat(sheet, cell, typed, -1, 64)
	default:
		_ = file.SetCellValue(sheet, cell, formatConvertValue(value, ""))
	}
}

func convertFileOptions(params map[string]any, key string) map[string]any {
	if raw, ok := params[key]; ok {
		if typed, ok := raw.(map[string]any); ok {
			return typed
		}
	}
	if raw, ok := params["options"]; ok {
		if typed, ok := raw.(map[string]any); ok {
			return typed
		}
	}
	return map[string]any{}
}

func encodeOutputBytes(data []byte, encodingName string) ([]byte, error) {
	enc, err := namedEncoding(encodingName)
	if err != nil {
		return nil, err
	}
	if enc == nil {
		return data, nil
	}
	var buffer bytes.Buffer
	writer := transform.NewWriter(&buffer, enc.NewEncoder())
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func cloneBinaryMap(input map[string]dataplane.Binary) map[string]dataplane.Binary {
	result := make(map[string]dataplane.Binary, len(input)+1)
	for key, value := range input {
		result[key] = value
	}
	return result
}
