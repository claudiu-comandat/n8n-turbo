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
	operation := firstNonEmptyNode(stringParam(in.Node.Parameters, "operation"), "compress")
	inputFields := compressionInputFields(in.Node.Parameters, operation)
	field := inputFields[0]
	if field == "" {
		field = "data"
	}
	outputPrefix := firstNonEmptyNode(stringParam(in.Node.Parameters, "outputPrefix"), "file_")
	result := make([]dataplane.Item, 0, len(firstInput(in.InputData)))
	for _, item := range firstInput(in.InputData) {
		next := cloneItem(item)
		if operation == "decompress" {
			if binary, ok := firstAvailableBinary(item.Binary, inputFields); ok {
				if in.BinaryStore != nil {
					outputs, err := decompressBinaryFieldsToStore(ctx, in.BinaryStore, binary, outputPrefix)
					if err != nil {
						return nil, err
					}
					if next.Binary == nil {
						next.Binary = map[string]dataplane.Binary{}
					}
					for name, output := range outputs {
						next.Binary[name] = output
					}
					next.JSON["fileCount"] = len(outputs)
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
			if next.Binary == nil {
				next.Binary = map[string]dataplane.Binary{}
			}
			next.Binary[field] = outBinary
			next.JSON["fileName"] = fileName
			next.JSON["mimeType"] = mimeType
			next.JSON["fileSize"] = int64(len(transformed))
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
		if operation == "decompress" {
			raw = "zipFile"
		} else {
			raw = "data"
		}
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
	operation := strings.ToLower(firstNonEmptyNode(stringParam(in.Node.Parameters, "operation"), "json"))
	binaryProperty := stringParam(in.Node.Parameters, "binaryPropertyName", "binaryProperty", "dataPropertyName")
	if binaryProperty == "" {
		binaryProperty = "data"
	}
	items := firstInput(in.InputData)
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

func convertItemsToFile(operation string, items []map[string]any, params map[string]any) ([]byte, string, string, error) {
	switch operation {
	case "csv":
		content, err := convertToCSV(items, params)
		return content, "text/csv; charset=UTF-8", "export.csv", err
	case "xlsx":
		content, err := convertToXLSX(items, params)
		return content, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "export.xlsx", err
	case "html":
		content, err := convertToHTML(items, params)
		return content, "text/html; charset=UTF-8", "export.html", err
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
