package nodes

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/andybalholm/cascadia"
	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/xuri/excelize/v2"
	nethtml "golang.org/x/net/html"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	unicodeenc "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type ExtractFromFile struct{}

const (
	lazyCSVMetaKey           = "__n8nTurboLazyCSV"
	lazyCSVPreviewRowLimit   = 8
	lazyCSVStreamingNodeType = "n8n-nodes-base.splitInBatches"
	lazyCSVLoopNodeType      = "n8n-nodes-base.loopOverItems"
)

type extractParams struct {
	operation           string
	binaryProperty      string
	delimiter           string
	quoteChar           string
	escapeChar          string
	headerRow           bool
	headerRowIndex      int
	skipRows            int
	commentChar         string
	trimLeadingSpace    bool
	outputFormat        string
	convertTypes        bool
	emptyValues         string
	encoding            string
	sheetName           string
	sheetIndex          int
	xlsxRange           string
	dateFormat          string
	streamThreshold     int
	htmlOperation       string
	selector            string
	returnAll           bool
	tableIndex          int
	trimText            bool
	linkBase            string
	onlyInternal        bool
	onlyWithAlt         bool
	componentTypes      []string
	timezone            string
	includeMetadata     bool
	trimWhitespace      bool
	lineOutputField     string
	outputFieldName     string
	splitIntoItems      bool
	includeInputFields  bool
	pdfJoinPages        bool
	pdfMaxPages         int
	pdfPassword         string
	includeSourceBinary bool
	keepOtherBinary     bool
}

func (ExtractFromFile) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	params := newExtractParams(in.Node.Parameters)
	items := firstInput(in.InputData)
	result := make([]dataplane.Item, 0, len(items))
	for itemIndex, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		binary, ok := item.Binary[params.binaryProperty]
		if !ok {
			return nil, fmt.Errorf("extractFromFile: binary property %s not found", params.binaryProperty)
		}
		operation := params.operation
		if operation == "" || operation == "auto" {
			operation = inferExtractOperation(binary)
		}
		operation = normalizeExtractOperation(operation)
		if canUseLazyCSVExtraction(ctx, in, binary, operation, params) {
			next, err := buildLazyCSVPlaceholder(ctx, in, item, itemIndex, binary, params)
			if err != nil {
				return nil, err
			}
			result = append(result, next)
			continue
		}
		rows, err := extractBinaryDataStreaming(ctx, in.BinaryStore, binary, operation, params)
		if err != nil {
			data, readErr := binarydata.Read(ctx, in.BinaryStore, binary)
			if readErr != nil {
				return nil, readErr
			}
			rows, err = extractBinaryData(ctx, data, operation, params, binary)
		}
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			result = append(result, extractedRowItem(item, itemIndex, row, params))
		}
	}
	return dataplane.MainOutput(result), nil
}

func extractBinaryDataStreaming(ctx context.Context, store binarydata.Store, binary dataplane.Binary, operation string, params extractParams) ([]map[string]any, error) {
	if !canStreamExtract(operation, params) {
		return nil, fmt.Errorf("extractFromFile: streaming unavailable for operation %s", operation)
	}
	reader, err := binarydata.Open(ctx, store, binary)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	switch strings.ToLower(operation) {
	case "csv":
		return extractCSVDataFromReader(reader, params)
	case "text", "txt":
		return extractTextDataFromReader(reader, params)
	case "binary":
		return extractBinaryReferenceStream(reader, binary, params)
	default:
		return nil, fmt.Errorf("extractFromFile: unsupported streaming operation %s", operation)
	}
}

func canStreamExtract(operation string, params extractParams) bool {
	switch strings.ToLower(operation) {
	case "csv":
		return params.quoteChar == `"` && params.escapeChar == `"`
	case "text", "txt":
		return true
	case "binary":
		return strings.EqualFold(params.outputFormat, "reference") || params.includeMetadata
	default:
		return false
	}
}

func canUseLazyCSVExtraction(ctx context.Context, in engine.ExecuteInput, binary dataplane.Binary, operation string, params extractParams) bool {
	if !strings.EqualFold(operation, "csv") {
		return false
	}
	if strings.EqualFold(params.outputFormat, "arrays") {
		return false
	}
	if !lazyCSVCompatibleNextNodes(in.NextNodes) {
		return false
	}
	return params.streamThreshold > 0 && estimatedBinarySize(ctx, in.BinaryStore, binary) >= int64(params.streamThreshold)
}

func lazyCSVCompatibleNextNodes(nextNodes []dataplane.Node) bool {
	if len(nextNodes) == 0 {
		return false
	}
	for _, node := range nextNodes {
		if node.Type != lazyCSVStreamingNodeType && node.Type != lazyCSVLoopNodeType {
			return false
		}
	}
	return true
}

func estimatedBinarySize(ctx context.Context, store binarydata.Store, binary dataplane.Binary) int64 {
	if binary.FileSize > 0 {
		return binary.FileSize
	}
	if binary.Data != "" {
		decodedLen := base64.StdEncoding.DecodedLen(len(binary.Data))
		if decodedLen > 0 {
			return int64(decodedLen)
		}
		return int64(len(binary.Data))
	}
	if store == nil || binary.ID == "" {
		return 0
	}
	ref, err := store.Stat(ctx, binary.ID)
	if err != nil {
		return 0
	}
	return ref.FileSize
}

func buildLazyCSVPlaceholder(ctx context.Context, in engine.ExecuteInput, item dataplane.Item, itemIndex int, binary dataplane.Binary, params extractParams) (dataplane.Item, error) {
	preview, err := previewLazyCSVRows(ctx, in.BinaryStore, binary, params, item, itemIndex, lazyCSVPreviewRowLimit)
	if err != nil {
		return dataplane.Item{}, err
	}
	next := dataplane.Item{
		JSON:       map[string]any{},
		Binary:     item.Binary,
		PairedItem: &dataplane.PairedItem{Item: itemIndex},
	}
	if params.includeInputFields {
		next = cloneItem(item)
		next.PairedItem = &dataplane.PairedItem{Item: itemIndex}
	}
	next.JSON[lazyCSVMetaKey] = map[string]any{
		"binaryProperty":     params.binaryProperty,
		"delimiter":          params.delimiter,
		"quoteChar":          params.quoteChar,
		"escapeChar":         params.escapeChar,
		"headerRow":          params.headerRow,
		"headerRowIndex":     params.headerRowIndex,
		"skipRows":           params.skipRows,
		"commentChar":        params.commentChar,
		"trimLeadingSpace":   params.trimLeadingSpace,
		"convertTypes":       params.convertTypes,
		"emptyValues":        params.emptyValues,
		"encoding":           params.encoding,
		"includeInputFields": params.includeInputFields,
		"sourceItemIndex":    itemIndex,
		"preview":            preview,
		"truncated":          true,
	}
	return next, nil
}

func previewLazyCSVRows(ctx context.Context, store binarydata.Store, binary dataplane.Binary, params extractParams, sourceItem dataplane.Item, itemIndex int, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		return nil, nil
	}
	reader, err := binarydata.Open(ctx, store, binary)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decoded, err := decodedReader(reader, params.encoding)
	if err != nil {
		return nil, err
	}
	buffered := bufio.NewReader(decoded)
	preview, _ := buffered.Peek(64 * 1024)
	delimiter := detectCSVDelimiter(string(preview), params.delimiter)
	csvReader := csv.NewReader(buffered)
	csvReader.Comma = []rune(delimiter)[0]
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true
	csvReader.TrimLeadingSpace = params.trimLeadingSpace
	if params.commentChar != "" {
		csvReader.Comment = []rune(params.commentChar)[0]
	}
	stream := newCSVObjectStream(csvReader, params)
	rows := make([]map[string]any, 0, limit)
	for len(rows) < limit {
		entry, err := stream.Next()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		if entry == nil {
			continue
		}
		rows = append(rows, extractedRowItem(sourceItem, itemIndex, entry, params).JSON)
	}
	return rows, nil
}

func extractedRowItem(sourceItem dataplane.Item, itemIndex int, row map[string]any, params extractParams) dataplane.Item {
	next := dataplane.Item{JSON: row, PairedItem: &dataplane.PairedItem{Item: itemIndex}}
	if !params.includeInputFields {
		if params.includeSourceBinary {
			next.Binary = sourceItem.Binary
		}
		return next
	}
	next = cloneItem(sourceItem)
	next.PairedItem = &dataplane.PairedItem{Item: itemIndex}
	if !params.includeSourceBinary && params.keepOtherBinary {
		next.Binary = cloneBinaryMap(sourceItem.Binary)
		delete(next.Binary, params.binaryProperty)
		if len(next.Binary) == 0 {
			next.Binary = nil
		}
	} else if !params.includeSourceBinary {
		next.Binary = nil
	}
	for key, value := range row {
		next.JSON[key] = value
	}
	return next
}

func newExtractParams(params map[string]any) extractParams {
	options := mergeObject(params["options"])
	keepSource := stringParam(options, "keepSource")
	operation := strings.TrimSpace(stringParam(params, "operation", "fileFormat"))
	if keepSource == "" && isExtractMoveToOperation(operation) {
		keepSource = "json"
	}
	parsed := extractParams{
		operation:           operation,
		binaryProperty:      stringParam(params, "binaryProperty", "binaryPropertyName", "dataPropertyName"),
		delimiter:           firstNonEmptyNode(stringParam(params, "delimiter"), stringParam(options, "delimiter")),
		quoteChar:           firstNonEmptyNode(stringParam(params, "quoteChar"), stringParam(options, "quoteChar")),
		escapeChar:          firstNonEmptyNode(stringParam(params, "escapeChar"), stringParam(options, "escapeChar")),
		headerRow:           boolParam(params, "headerRow", true),
		headerRowIndex:      intParam(params, "headerRowIndex", 0),
		skipRows:            intParam(params, "skipRows", 0),
		commentChar:         firstNonEmptyNode(stringParam(params, "commentChar"), stringParam(options, "commentChar")),
		trimLeadingSpace:    boolParam(params, "trimLeadingSpace", true),
		outputFormat:        firstNonEmptyNode(stringParam(params, "outputFormat"), stringParam(options, "outputFormat")),
		convertTypes:        boolParam(params, "convertTypes", true),
		emptyValues:         stringParam(params, "emptyValues", "emptyCells"),
		encoding:            firstNonEmptyNode(stringParam(params, "encoding"), stringParam(options, "encoding")),
		sheetName:           stringParam(params, "sheetName"),
		sheetIndex:          intParam(params, "sheetIndex", 0),
		xlsxRange:           stringParam(params, "range"),
		dateFormat:          stringParam(params, "dateFormat"),
		streamThreshold:     intParam(params, "streamThreshold", 100*1024*1024),
		htmlOperation:       stringParam(params, "htmlOperation", "extractOperation", "mode"),
		selector:            stringParam(params, "selector", "cssSelector"),
		returnAll:           boolParam(params, "returnAll", false),
		tableIndex:          intParam(params, "tableIndex", 0),
		trimText:            boolParam(params, "trimText", true),
		linkBase:            stringParam(params, "linkBase", "baseURL", "baseUrl"),
		onlyInternal:        boolParam(params, "onlyInternal", false),
		onlyWithAlt:         boolParam(params, "onlyWithAlt", false),
		componentTypes:      stringList(params["componentTypes"]),
		timezone:            stringParam(params, "timezone"),
		includeMetadata:     boolParam(params, "includeMetadata", false),
		trimWhitespace:      boolParam(options, "trimWhitespace", boolParam(params, "trimWhitespace", false)),
		lineOutputField:     stringParam(params, "lineOutputField"),
		outputFieldName:     stringParam(params, "outputFieldName", "destinationKey"),
		splitIntoItems:      boolParam(params, "splitIntoItems", false) || boolParam(params, "splitIntoLines", false),
		includeInputFields:  boolParam(params, "includeInputFields", false) || (keepSource != "" && keepSource != "binary"),
		pdfJoinPages:        boolParam(options, "joinPages", true),
		pdfMaxPages:         intParam(options, "maxPages", 0),
		pdfPassword:         stringParam(options, "password"),
		includeSourceBinary: keepSource == "binary" || keepSource == "both",
		keepOtherBinary:     keepSource == "json",
	}
	if parsed.binaryProperty == "" {
		parsed.binaryProperty = "data"
	}
	if parsed.delimiter == "" {
		parsed.delimiter = "auto"
	}
	if parsed.quoteChar == "" {
		parsed.quoteChar = `"`
	}
	if parsed.escapeChar == "" {
		parsed.escapeChar = `"`
	}
	if parsed.commentChar == "" {
		parsed.commentChar = "#"
	}
	if parsed.outputFormat == "" {
		parsed.outputFormat = "objects"
	}
	if parsed.emptyValues == "" {
		parsed.emptyValues = "null"
	}
	if parsed.encoding == "" {
		parsed.encoding = "auto"
	}
	if parsed.outputFieldName == "" {
		parsed.outputFieldName = "data"
	}
	if parsed.lineOutputField == "" {
		parsed.lineOutputField = "line"
	}
	return parsed
}

func isExtractMoveToOperation(operation string) bool {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "binarytopropery", "binarytoproperty", "fromjson", "text", "fromics", "xml":
		return true
	default:
		return false
	}
}

func normalizeExtractOperation(operation string) string {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "binarytopropery", "binarytoproperty":
		return "binary"
	case "xls":
		return "xlsx"
	case "fromics":
		return "ical"
	case "fromjson":
		return "json"
	default:
		return operation
	}
}

func inferExtractOperation(binary dataplane.Binary) string {
	value := strings.ToLower(firstNonEmptyNode(binary.FileExtension, binary.FileType, binary.MimeType, binary.FileName))
	switch {
	case strings.Contains(value, "xlsx") || strings.HasSuffix(value, ".xlsx"):
		return "xlsx"
	case strings.Contains(value, "csv") || strings.HasSuffix(value, ".csv"):
		return "csv"
	case strings.Contains(value, "html") || strings.HasSuffix(value, ".html") || strings.HasSuffix(value, ".htm"):
		return "html"
	case strings.Contains(value, "calendar") || strings.Contains(value, "ical") || strings.HasSuffix(value, ".ics"):
		return "ical"
	case strings.Contains(value, "opendocument") || strings.Contains(value, "ods") || strings.HasSuffix(value, ".ods"):
		return "ods"
	case strings.Contains(value, "text") || strings.HasSuffix(value, ".txt") || strings.HasSuffix(value, ".log"):
		return "text"
	default:
		return "binary"
	}
}

func extractBinaryData(ctx context.Context, data []byte, operation string, params extractParams, binary dataplane.Binary) ([]map[string]any, error) {
	switch strings.ToLower(operation) {
	case "csv":
		return extractCSVData(data, params)
	case "xlsx", "spreadsheet":
		return extractXLSXData(ctx, data, params)
	case "ods":
		return extractODSData(data, params)
	case "ical", "ics":
		return extractICalData(data, params)
	case "json":
		return extractJSONData(data, params)
	case "pdf":
		return extractPDFData(ctx, data, params)
	case "text", "txt":
		return extractTextData(data, params)
	case "rtf":
		return extractRTFData(data, params), nil
	case "xml":
		return extractXMLData(data, params)
	case "html", "htm":
		return extractHTMLData(data, params)
	case "binary":
		return extractBinaryReference(data, binary, params), nil
	default:
		return nil, fmt.Errorf("extractFromFile: unsupported operation %s", operation)
	}
}

func extractXMLData(data []byte, params extractParams) ([]map[string]any, error) {
	converted, err := xmlToJSON(string(data), xmlNodeOptions{
		TextNodeKey:   "_",
		ParseNumbers:  true,
		ParseBooleans: true,
		ExplicitRoot:  true,
		MergeAttrs:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("xml: parse data: %w", err)
	}
	return []map[string]any{{params.outputFieldName: converted}}, nil
}

func extractRTFData(data []byte, params extractParams) []map[string]any {
	return []map[string]any{{params.outputFieldName: rtfPlainText(string(data))}}
}

func rtfPlainText(raw string) string {
	var builder strings.Builder
	depth := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case '\\':
			i = skipRTFControl(raw, i+1, &builder)
		default:
			if depth >= 0 {
				builder.WriteByte(raw[i])
			}
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func skipRTFControl(raw string, index int, builder *strings.Builder) int {
	if index >= len(raw) {
		return index
	}
	switch raw[index] {
	case '\\', '{', '}':
		builder.WriteByte(raw[index])
		return index
	case '\'':
		if index+2 < len(raw) {
			if value, err := strconv.ParseUint(raw[index+1:index+3], 16, 8); err == nil {
				builder.WriteByte(byte(value))
			}
			return index + 2
		}
		return index
	case '~':
		builder.WriteByte(' ')
		return index
	case 'n':
		return index
	}
	for index < len(raw) && ((raw[index] >= 'a' && raw[index] <= 'z') || (raw[index] >= 'A' && raw[index] <= 'Z')) {
		index++
	}
	if index < len(raw) && raw[index] == '-' {
		index++
	}
	for index < len(raw) && raw[index] >= '0' && raw[index] <= '9' {
		index++
	}
	if index < len(raw) && raw[index] == ' ' {
		return index
	}
	return index - 1
}

func extractJSONData(data []byte, params extractParams) ([]map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return []map[string]any{{params.outputFieldName: map[string]any{}}}, nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("json: parse data: %w", err)
	}
	return []map[string]any{{params.outputFieldName: value}}, nil
}

func extractPDFData(ctx context.Context, data []byte, params extractParams) ([]map[string]any, error) {
	file, err := os.CreateTemp("", "n8n-turbo-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("pdf: create temp file: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return nil, fmt.Errorf("pdf: write temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("pdf: close temp file: %w", err)
	}
	args := []string{"-layout"}
	if params.pdfJoinPages {
		args = append(args, "-nopgbrk")
	}
	if params.pdfMaxPages > 0 {
		args = append(args, "-f", "1", "-l", fmt.Sprint(params.pdfMaxPages))
	}
	if params.pdfPassword != "" {
		args = append(args, "-opw", params.pdfPassword, "-upw", params.pdfPassword)
	}
	args = append(args, path, "-")
	output, err := exec.CommandContext(ctx, "pdftotext", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pdf: extract text: %w: %s", err, strings.TrimSpace(string(output)))
	}
	text := strings.TrimRight(string(output), "\x00")
	if params.pdfJoinPages {
		return []map[string]any{{"text": strings.TrimRight(text, "\f\r\n")}}, nil
	}
	pages := strings.Split(strings.TrimRight(text, "\f\r\n"), "\f")
	for index := range pages {
		pages[index] = strings.TrimRight(pages[index], "\r\n")
	}
	return []map[string]any{{"text": pages}}, nil
}

func extractCSVData(data []byte, params extractParams) ([]map[string]any, error) {
	reader, err := decodedReader(bytes.NewReader(data), params.encoding)
	if err != nil {
		return nil, err
	}
	text, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("csv: read data: %w", err)
	}
	textValue := normalizeCSVQuotes(string(text), params)
	delimiter := detectCSVDelimiter(textValue, params.delimiter)
	csvReader := csv.NewReader(strings.NewReader(textValue))
	csvReader.Comma = []rune(delimiter)[0]
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true
	csvReader.TrimLeadingSpace = params.trimLeadingSpace
	if params.commentChar != "" {
		csvReader.Comment = []rune(params.commentChar)[0]
	}
	rows, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv: parse data: %w", err)
	}
	rows = skipRows(rows, params.skipRows)
	if strings.EqualFold(params.outputFormat, "arrays") {
		return csvRowsAsArrays(rows, params), nil
	}
	return tabularRowsAsObjects(rows, params, "csv")
}

func extractCSVDataFromReader(reader io.Reader, params extractParams) ([]map[string]any, error) {
	decoded, err := decodedReader(reader, params.encoding)
	if err != nil {
		return nil, err
	}
	buffered := bufio.NewReader(decoded)
	preview, _ := buffered.Peek(64 * 1024)
	delimiter := detectCSVDelimiter(string(preview), params.delimiter)
	csvReader := csv.NewReader(buffered)
	csvReader.Comma = []rune(delimiter)[0]
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true
	csvReader.TrimLeadingSpace = params.trimLeadingSpace
	if params.commentChar != "" {
		csvReader.Comment = []rune(params.commentChar)[0]
	}
	if strings.EqualFold(params.outputFormat, "arrays") {
		all := make([]any, 0)
		rowIndex := 0
		for {
			row, err := csvReader.Read()
			if err == io.EOF {
				return []map[string]any{{"rows": all}}, nil
			}
			if err != nil {
				return nil, fmt.Errorf("csv: parse data: %w", err)
			}
			if rowIndex < params.skipRows {
				rowIndex++
				continue
			}
			values := make([]any, 0, len(row))
			for _, value := range row {
				converted := convertExtractValue(value, params)
				if !isSkipExtractValue(converted) {
					values = append(values, converted)
				}
			}
			all = append(all, values)
			rowIndex++
		}
	}
	stream := newCSVObjectStream(csvReader, params)
	result := make([]map[string]any, 0)
	for {
		entry, err := stream.Next()
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		if entry != nil {
			result = append(result, entry)
		}
	}
}

type csvObjectStream struct {
	reader        *csv.Reader
	params        extractParams
	headerIndex   int
	rowIndex      int
	rowsAfterSkip int
	headers       []string
}

func newCSVObjectStream(reader *csv.Reader, params extractParams) *csvObjectStream {
	return &csvObjectStream{
		reader:      reader,
		params:      params,
		headerIndex: max(0, params.headerRowIndex),
	}
}

func (s *csvObjectStream) Next() (map[string]any, error) {
	for {
		row, err := s.reader.Read()
		if err == io.EOF {
			if s.params.headerRow && s.rowsAfterSkip <= s.headerIndex {
				if s.rowsAfterSkip == 0 {
					return nil, io.EOF
				}
				return nil, fmt.Errorf("csv: header row %d out of range", s.headerIndex)
			}
			return nil, io.EOF
		}
		if err != nil {
			return nil, fmt.Errorf("csv: parse data: %w", err)
		}
		if s.rowIndex < s.params.skipRows {
			s.rowIndex++
			continue
		}
		if s.params.headerRow {
			if s.rowsAfterSkip < s.headerIndex {
				s.rowsAfterSkip++
				s.rowIndex++
				continue
			}
			if s.rowsAfterSkip == s.headerIndex {
				s.headers = normalizeHeaders(row)
				s.rowsAfterSkip++
				s.rowIndex++
				continue
			}
		} else if s.headers == nil {
			s.headers = generatedHeaders(len(row))
		}
		s.rowsAfterSkip++
		s.rowIndex++
		if len(row) == 0 {
			continue
		}
		entry := make(map[string]any, len(s.headers))
		for columnIndex, header := range s.headers {
			if columnIndex >= len(row) {
				value := emptyExtractValue(s.params.emptyValues)
				if !isSkipExtractValue(value) {
					entry[header] = value
				}
				continue
			}
			value := convertExtractValue(row[columnIndex], s.params)
			if !isSkipExtractValue(value) {
				entry[header] = value
			}
		}
		for columnIndex := len(s.headers); columnIndex < len(row); columnIndex++ {
			value := convertExtractValue(row[columnIndex], s.params)
			if !isSkipExtractValue(value) {
				entry[fmt.Sprintf("extra_%d", columnIndex-len(s.headers)+1)] = value
			}
		}
		return entry, nil
	}
}

func extractXLSXData(ctx context.Context, data []byte, params extractParams) ([]map[string]any, error) {
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("xlsx: open workbook: %w", err)
	}
	defer file.Close()
	sheet, err := resolveXLSXSheet(file, params)
	if err != nil {
		return nil, err
	}
	var rows [][]string
	if params.streamThreshold > 0 && len(data) >= params.streamThreshold {
		rows, err = xlsxRowsFromIterator(ctx, file, sheet)
	} else {
		rows, err = file.GetRows(sheet, excelize.Options{RawCellValue: false})
	}
	if err != nil {
		return nil, fmt.Errorf("xlsx: read rows: %w", err)
	}
	rows, baseRow, baseColumn, err := applyXLSXRange(rows, params.xlsxRange)
	if err != nil {
		return nil, err
	}
	rows = skipRows(rows, params.skipRows)
	if strings.EqualFold(params.outputFormat, "arrays") {
		return xlsxRowsAsArrays(rows, params, file, sheet, baseRow+params.skipRows, baseColumn), nil
	}
	return xlsxRowsAsObjects(rows, params, file, sheet, baseRow+params.skipRows, baseColumn)
}

func xlsxRowsFromIterator(ctx context.Context, file *excelize.File, sheet string) ([][]string, error) {
	iterator, err := file.Rows(sheet)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	rows := make([][]string, 0)
	for iterator.Next() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		row, err := iterator.Columns()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err := iterator.Error(); err != nil {
		return nil, err
	}
	return rows, nil
}

func resolveXLSXSheet(file *excelize.File, params extractParams) (string, error) {
	sheets := file.GetSheetList()
	if len(sheets) == 0 {
		return "", fmt.Errorf("xlsx: workbook has no sheets")
	}
	if params.sheetName != "" {
		for _, sheet := range sheets {
			if sheet == params.sheetName {
				return sheet, nil
			}
		}
		return "", fmt.Errorf("xlsx: sheet %s not found", params.sheetName)
	}
	if params.sheetIndex < 0 || params.sheetIndex >= len(sheets) {
		return "", fmt.Errorf("xlsx: sheet index %d out of range", params.sheetIndex)
	}
	return sheets[params.sheetIndex], nil
}

func applyXLSXRange(rows [][]string, rawRange string) ([][]string, int, int, error) {
	if strings.TrimSpace(rawRange) == "" {
		return rows, 1, 1, nil
	}
	startColumn, startRow, endColumn, endRow, err := parseXLSXRange(rawRange)
	if err != nil {
		return nil, 0, 0, err
	}
	if startRow > len(rows) {
		return [][]string{}, startRow, startColumn, nil
	}
	if endRow > len(rows) {
		endRow = len(rows)
	}
	width := endColumn - startColumn + 1
	filtered := make([][]string, 0, endRow-startRow+1)
	for rowIndex := startRow - 1; rowIndex < endRow; rowIndex++ {
		source := rows[rowIndex]
		row := make([]string, width)
		for column := 0; column < width; column++ {
			sourceIndex := startColumn - 1 + column
			if sourceIndex < len(source) {
				row[column] = source[sourceIndex]
			}
		}
		filtered = append(filtered, row)
	}
	return filtered, startRow, startColumn, nil
}

func parseXLSXRange(rawRange string) (int, int, int, int, error) {
	parts := strings.Split(strings.TrimSpace(rawRange), ":")
	if len(parts) == 0 || len(parts) > 2 {
		return 0, 0, 0, 0, fmt.Errorf("xlsx: invalid range %s", rawRange)
	}
	startColumn, startRow, err := excelize.CellNameToCoordinates(parts[0])
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("xlsx: invalid range start %s: %w", rawRange, err)
	}
	endColumn, endRow := startColumn, startRow
	if len(parts) == 2 {
		endColumn, endRow, err = excelize.CellNameToCoordinates(parts[1])
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("xlsx: invalid range end %s: %w", rawRange, err)
		}
	}
	if endColumn < startColumn || endRow < startRow {
		return 0, 0, 0, 0, fmt.Errorf("xlsx: invalid range %s", rawRange)
	}
	return startColumn, startRow, endColumn, endRow, nil
}

func xlsxRowsAsObjects(rows [][]string, params extractParams, file *excelize.File, sheet string, baseRow int, baseColumn int) ([]map[string]any, error) {
	if len(rows) == 0 {
		return []map[string]any{}, nil
	}
	headerIndex := params.headerRowIndex
	if headerIndex < 0 {
		headerIndex = 0
	}
	if params.headerRow && headerIndex >= len(rows) {
		return nil, fmt.Errorf("xlsx: header row %d out of range", headerIndex)
	}
	headers := make([]string, 0)
	dataStart := 0
	if params.headerRow {
		headers = normalizeHeaders(rows[headerIndex])
		dataStart = headerIndex + 1
	} else {
		headers = generatedXLSXHeaders(maxRowWidth(rows))
	}
	result := make([]map[string]any, 0, max(0, len(rows)-dataStart))
	for rowIndex, row := range rows[dataStart:] {
		absoluteRow := baseRow + dataStart + rowIndex
		if isEmptyExtractRow(row) {
			continue
		}
		entry := make(map[string]any, len(headers))
		for columnIndex, header := range headers {
			absoluteColumn := baseColumn + columnIndex
			if columnIndex >= len(row) {
				value := emptyExtractValue(params.emptyValues)
				if !isSkipExtractValue(value) {
					entry[header] = value
				}
				continue
			}
			value := convertXLSXValue(file, sheet, absoluteRow, absoluteColumn, row[columnIndex], params)
			if !isSkipExtractValue(value) {
				entry[header] = value
			}
		}
		for columnIndex := len(headers); columnIndex < len(row); columnIndex++ {
			absoluteColumn := baseColumn + columnIndex
			value := convertXLSXValue(file, sheet, absoluteRow, absoluteColumn, row[columnIndex], params)
			if !isSkipExtractValue(value) {
				entry[fmt.Sprintf("extra_%d", columnIndex-len(headers)+1)] = value
			}
		}
		result = append(result, entry)
	}
	return result, nil
}

func xlsxRowsAsArrays(rows [][]string, params extractParams, file *excelize.File, sheet string, baseRow int, baseColumn int) []map[string]any {
	all := make([]any, 0, len(rows))
	for rowIndex, row := range rows {
		values := make([]any, 0, len(row))
		for columnIndex, value := range row {
			converted := convertXLSXValue(file, sheet, baseRow+rowIndex, baseColumn+columnIndex, value, params)
			if !isSkipExtractValue(converted) {
				values = append(values, converted)
			}
		}
		all = append(all, values)
	}
	return []map[string]any{{"rows": all}}
}

func convertXLSXValue(file *excelize.File, sheet string, row int, column int, value string, params extractParams) any {
	if value == "" {
		return emptyExtractValue(params.emptyValues)
	}
	if !params.convertTypes {
		return value
	}
	cell, err := excelize.CoordinatesToCellName(column, row)
	if err == nil {
		cellType, typeErr := file.GetCellType(sheet, cell)
		if typeErr == nil {
			switch cellType {
			case excelize.CellTypeBool:
				return strings.EqualFold(value, "true") || value == "1"
			case excelize.CellTypeDate:
				return convertExtractDate(value, params)
			}
		}
	}
	return convertExtractValue(value, params)
}

func convertExtractDate(value string, params extractParams) any {
	trimmed := strings.TrimSpace(value)
	for _, format := range extractDateLayouts() {
		if parsed, err := time.Parse(format, trimmed); err == nil {
			return parsed.UTC().Format(extractDateFormat(params))
		}
	}
	return value
}

func extractDateFormat(params extractParams) string {
	if params.dateFormat != "" {
		return params.dateFormat
	}
	return time.RFC3339
}

func extractDateLayouts() []string {
	return []string{
		time.RFC3339,
		"2006-01-02",
		"2006-01-02 15:04:05",
		"1/2/06",
		"01-02-06",
		"1/2/06 15:04",
		"1/2/2006",
		"1/2/2006 15:04:05",
		"02.01.2006",
		"02/01/2006",
		"January 2, 2006",
	}
}

func generatedXLSXHeaders(width int) []string {
	headers := make([]string, 0, width)
	for index := 0; index < width; index++ {
		name, err := excelize.ColumnNumberToName(index + 1)
		if err != nil {
			name = fmt.Sprintf("column_%d", index+1)
		}
		headers = append(headers, name)
	}
	return headers
}

func isEmptyExtractRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

type odsTable struct {
	Name string
	Rows [][]odsCell
}

type odsCell struct {
	ValueType    string
	Value        string
	DateValue    string
	BooleanValue string
	Text         string
}

func extractODSData(data []byte, params extractParams) ([]map[string]any, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("ods: open document: %w", err)
	}
	var content io.ReadCloser
	for _, file := range reader.File {
		if file.Name == "content.xml" {
			content, err = file.Open()
			if err != nil {
				return nil, fmt.Errorf("ods: open content.xml: %w", err)
			}
			defer content.Close()
			break
		}
	}
	if content == nil {
		return nil, fmt.Errorf("ods: content.xml not found")
	}
	tables, err := parseODSTables(content)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return []map[string]any{}, nil
	}
	tableIndex := params.sheetIndex
	if params.sheetName != "" {
		tableIndex = -1
		for index, table := range tables {
			if table.Name == params.sheetName {
				tableIndex = index
				break
			}
		}
	}
	if tableIndex < 0 || tableIndex >= len(tables) {
		return nil, fmt.Errorf("ods: sheet %s index %d not found", params.sheetName, params.sheetIndex)
	}
	return odsTableToMaps(tables[tableIndex], params), nil
}

func parseODSTables(reader io.Reader) ([]odsTable, error) {
	decoder := xml.NewDecoder(reader)
	tables := []odsTable{}
	var table *odsTable
	var row []odsCell
	var cell odsCell
	var cellRepeat int
	var rowRepeat int
	var inCell bool
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ods: parse content.xml: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "table":
				if table == nil {
					table = &odsTable{Name: xmlAttr(typed.Attr, "name")}
				}
			case "table-row":
				if table != nil {
					row = []odsCell{}
					rowRepeat = positiveRepeat(xmlAttr(typed.Attr, "number-rows-repeated"))
				}
			case "table-cell", "covered-table-cell":
				if table != nil {
					cell = odsCell{
						ValueType:    xmlAttr(typed.Attr, "value-type"),
						Value:        xmlAttr(typed.Attr, "value"),
						DateValue:    xmlAttr(typed.Attr, "date-value"),
						BooleanValue: xmlAttr(typed.Attr, "boolean-value"),
					}
					cellRepeat = positiveRepeat(xmlAttr(typed.Attr, "number-columns-repeated"))
					text.Reset()
					inCell = true
				}
			}
		case xml.CharData:
			if inCell {
				text.Write([]byte(typed))
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "table-cell", "covered-table-cell":
				if inCell {
					cell.Text = strings.TrimSpace(text.String())
					for index := 0; index < cellRepeat; index++ {
						row = append(row, cell)
					}
					inCell = false
				}
			case "table-row":
				if table != nil {
					repeat := min(rowRepeat, 10000)
					for index := 0; index < repeat; index++ {
						table.Rows = append(table.Rows, append([]odsCell(nil), row...))
					}
				}
			case "table":
				if table != nil {
					tables = append(tables, *table)
					table = nil
				}
			}
		}
	}
	return tables, nil
}

func odsTableToMaps(table odsTable, params extractParams) []map[string]any {
	rows := trimEmptyODSTail(table.Rows)
	if len(rows) == 0 {
		return []map[string]any{}
	}
	if strings.EqualFold(params.outputFormat, "arrays") {
		return odsRowsAsArrays(rows, params)
	}
	headerIndex := params.headerRowIndex
	if headerIndex < 0 {
		headerIndex = 0
	}
	headers := []string{}
	dataStart := 0
	if params.headerRow && headerIndex < len(rows) {
		rawHeaders := make([]string, 0, len(rows[headerIndex]))
		for _, cell := range rows[headerIndex] {
			rawHeaders = append(rawHeaders, odsCellText(cell))
		}
		headers = normalizeHeaders(rawHeaders)
		dataStart = headerIndex + 1
	} else {
		headers = generatedHeaders(maxODSWidth(rows))
	}
	result := make([]map[string]any, 0, max(0, len(rows)-dataStart))
	for _, row := range rows[dataStart:] {
		if isEmptyODSRow(row) {
			continue
		}
		entry := make(map[string]any, len(headers))
		for index, header := range headers {
			if index < len(row) {
				entry[header] = convertODSCell(row[index], params)
			} else {
				entry[header] = emptyExtractValue(params.emptyValues)
			}
		}
		result = append(result, entry)
	}
	return result
}

func odsRowsAsArrays(rows [][]odsCell, params extractParams) []map[string]any {
	all := make([]any, 0, len(rows))
	for _, row := range rows {
		values := make([]any, 0, len(row))
		for _, cell := range row {
			values = append(values, convertODSCell(cell, params))
		}
		all = append(all, values)
	}
	return []map[string]any{{"rows": all}}
}

func convertODSCell(cell odsCell, params extractParams) any {
	text := odsCellText(cell)
	if !params.convertTypes {
		return text
	}
	switch strings.ToLower(cell.ValueType) {
	case "float", "currency", "percentage":
		if cell.Value != "" {
			if value, err := strconv.ParseFloat(cell.Value, 64); err == nil {
				return value
			}
		}
	case "boolean":
		return strings.EqualFold(cell.BooleanValue, "true")
	case "date":
		if cell.DateValue != "" {
			if parsed, ok := parseODSDate(cell.DateValue, params); ok {
				return parsed
			}
		}
	}
	if text == "" {
		return emptyExtractValue(params.emptyValues)
	}
	return convertExtractValue(text, params)
}

func parseODSDate(value string, params extractParams) (string, bool) {
	layouts := []string{"2006-01-02T15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			if params.dateFormat != "" {
				return parsed.Format(params.dateFormat), true
			}
			if layout == "2006-01-02" {
				return parsed.Format("2006-01-02"), true
			}
			return parsed.Format(time.RFC3339), true
		}
	}
	return "", false
}

func odsCellText(cell odsCell) string {
	if cell.Text != "" {
		return cell.Text
	}
	return firstNonEmptyNode(cell.Value, cell.DateValue, cell.BooleanValue)
}

func trimEmptyODSTail(rows [][]odsCell) [][]odsCell {
	end := len(rows)
	for end > 0 && isEmptyODSRow(rows[end-1]) {
		end--
	}
	return rows[:end]
}

func isEmptyODSRow(row []odsCell) bool {
	for _, cell := range row {
		if strings.TrimSpace(odsCellText(cell)) != "" {
			return false
		}
	}
	return true
}

func maxODSWidth(rows [][]odsCell) int {
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	return width
}

func xmlAttr(attrs []xml.Attr, name string) string {
	for _, attr := range attrs {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func positiveRepeat(raw string) int {
	if raw == "" {
		return 1
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 1
	}
	return value
}

type icalProperty struct {
	Value  string
	Params map[string]string
}

type icalComponent struct {
	Type       string
	Properties map[string][]icalProperty
}

func extractICalData(data []byte, params extractParams) ([]map[string]any, error) {
	components, err := parseICalComponents(string(data), params)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(components))
	for _, component := range components {
		if strings.EqualFold(params.outputFormat, "structured") {
			result = append(result, structuredICalComponent(component))
			continue
		}
		result = append(result, flatICalComponent(component, params))
	}
	return result, nil
}

func parseICalComponents(content string, params extractParams) ([]icalComponent, error) {
	wanted := map[string]bool{}
	types := params.componentTypes
	if len(types) == 0 {
		types = []string{"VEVENT"}
	}
	for _, value := range types {
		wanted[strings.ToUpper(strings.TrimSpace(value))] = true
	}
	lines := unfoldICalLines(content)
	components := []icalComponent{}
	stack := []string{}
	var current *icalComponent
	for _, line := range lines {
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, propertyParams, value := parseICalLine(line)
		name = strings.ToUpper(name)
		switch name {
		case "BEGIN":
			componentType := strings.ToUpper(value)
			stack = append(stack, componentType)
			if wanted[componentType] {
				current = &icalComponent{Type: componentType, Properties: map[string][]icalProperty{}}
			}
		case "END":
			componentType := strings.ToUpper(value)
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if wanted[componentType] && current != nil {
				components = append(components, *current)
				current = nil
			}
		default:
			if current != nil {
				current.Properties[name] = append(current.Properties[name], icalProperty{Value: value, Params: propertyParams})
			}
		}
	}
	return components, nil
}

func unfoldICalLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	rawLines := strings.Split(content, "\n")
	lines := []string{}
	var current strings.Builder
	for _, line := range rawLines {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			current.WriteString(strings.TrimLeft(line, " \t"))
			continue
		}
		if current.Len() > 0 {
			lines = append(lines, current.String())
			current.Reset()
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func parseICalLine(line string) (string, map[string]string, string) {
	params := map[string]string{}
	index := strings.Index(line, ":")
	if index < 0 {
		return line, params, ""
	}
	left := line[:index]
	value := line[index+1:]
	parts := strings.Split(left, ";")
	name := parts[0]
	for _, part := range parts[1:] {
		key, raw, ok := strings.Cut(part, "=")
		if ok {
			params[strings.ToUpper(key)] = strings.Trim(raw, `"`)
		}
	}
	return name, params, value
}

func flatICalComponent(component icalComponent, params extractParams) map[string]any {
	output := map[string]any{"type": component.Type}
	for name, properties := range component.Properties {
		key := normalizeICalKey(name)
		if len(properties) == 1 {
			output[key] = convertICalValue(name, properties[0], params)
			continue
		}
		values := make([]any, 0, len(properties))
		for _, property := range properties {
			values = append(values, convertICalValue(name, property, params))
		}
		output[key] = values
	}
	return output
}

func structuredICalComponent(component icalComponent) map[string]any {
	properties := map[string]any{}
	for name, values := range component.Properties {
		entries := make([]any, 0, len(values))
		for _, property := range values {
			entries = append(entries, map[string]any{"value": unescapeICalText(property.Value), "params": property.Params})
		}
		properties[name] = entries
	}
	return map[string]any{"type": component.Type, "properties": properties}
}

func convertICalValue(name string, property icalProperty, params extractParams) any {
	upper := strings.ToUpper(name)
	switch upper {
	case "DTSTART", "DTEND", "DTSTAMP", "CREATED", "LAST-MODIFIED", "COMPLETED", "DUE", "RECURRENCE-ID":
		timezone := params.timezone
		if value := property.Params["TZID"]; value != "" {
			timezone = value
		}
		if parsed, err := parseICalDate(property.Value, timezone); err == nil {
			return parsed.Format(time.RFC3339)
		}
	case "CATEGORIES", "RESOURCES":
		parts := strings.Split(property.Value, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			values = append(values, unescapeICalText(strings.TrimSpace(part)))
		}
		return values
	}
	return unescapeICalText(property.Value)
}

func parseICalDate(value string, timezone string) (time.Time, error) {
	if strings.HasSuffix(value, "Z") {
		return time.Parse("20060102T150405Z", value)
	}
	if strings.Contains(value, "T") {
		if timezone != "" {
			location, err := time.LoadLocation(timezone)
			if err != nil {
				return time.Time{}, err
			}
			return time.ParseInLocation("20060102T150405", value, location)
		}
		return time.Parse("20060102T150405", value)
	}
	return time.Parse("20060102", value)
}

func normalizeICalKey(key string) string {
	parts := strings.Split(strings.ToLower(key), "-")
	if len(parts) == 1 {
		return parts[0]
	}
	var builder strings.Builder
	builder.WriteString(parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		builder.WriteString(part[1:])
	}
	return builder.String()
}

func unescapeICalText(value string) string {
	value = strings.ReplaceAll(value, `\n`, "\n")
	value = strings.ReplaceAll(value, `\N`, "\n")
	value = strings.ReplaceAll(value, `\,`, ",")
	value = strings.ReplaceAll(value, `\;`, ";")
	value = strings.ReplaceAll(value, `\\`, `\`)
	return value
}

func extractTextData(data []byte, params extractParams) ([]map[string]any, error) {
	reader, err := decodedReader(bytes.NewReader(data), params.encoding)
	if err != nil {
		return nil, err
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("text: read data: %w", err)
	}
	text := string(decoded)
	if params.trimWhitespace {
		text = strings.TrimSpace(text)
	}
	if !params.splitIntoItems {
		return []map[string]any{{params.outputFieldName: text}}, nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	result := make([]map[string]any, 0, len(lines))
	for index, line := range lines {
		line = strings.TrimRight(line, "\r")
		if params.trimWhitespace {
			line = strings.TrimSpace(line)
		}
		field := firstNonEmptyNode(params.lineOutputField, params.outputFieldName)
		result = append(result, map[string]any{field: line, "lineIndex": index})
	}
	return result, nil
}

func extractTextDataFromReader(reader io.Reader, params extractParams) ([]map[string]any, error) {
	decoded, err := decodedReader(reader, params.encoding)
	if err != nil {
		return nil, err
	}
	if !params.splitIntoItems {
		var builder strings.Builder
		if _, err := io.Copy(&builder, decoded); err != nil {
			return nil, fmt.Errorf("text: read data: %w", err)
		}
		text := builder.String()
		if params.trimWhitespace {
			text = strings.TrimSpace(text)
		}
		return []map[string]any{{params.outputFieldName: text}}, nil
	}
	buffered := bufio.NewReader(decoded)
	field := firstNonEmptyNode(params.lineOutputField, params.outputFieldName)
	result := make([]map[string]any, 0)
	lineIndex := 0
	for {
		line, err := buffered.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("text: read line: %w", err)
		}
		if err == io.EOF && line == "" {
			return result, nil
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimRight(line, "\r")
		if params.trimWhitespace {
			line = strings.TrimSpace(line)
		}
		result = append(result, map[string]any{field: line, "lineIndex": lineIndex})
		lineIndex++
		if err == io.EOF {
			return result, nil
		}
	}
}

func extractBinaryReference(data []byte, binary dataplane.Binary, params extractParams) []map[string]any {
	output := map[string]any{params.outputFieldName: base64.StdEncoding.EncodeToString(data)}
	if strings.EqualFold(params.outputFormat, "reference") {
		output[params.outputFieldName] = binaryMetadata(binary, len(data))
	}
	if params.includeMetadata {
		metadata := binaryMetadata(binary, len(data))
		output["binary"] = metadata
		output["fileName"] = metadata["fileName"]
		output["mimeType"] = metadata["mimeType"]
		output["fileSize"] = metadata["fileSize"]
		output["fileExtension"] = metadata["fileExtension"]
	}
	return []map[string]any{output}
}

func extractBinaryReferenceStream(reader io.Reader, binary dataplane.Binary, params extractParams) ([]map[string]any, error) {
	if !strings.EqualFold(params.outputFormat, "reference") {
		return nil, fmt.Errorf("binary: streaming only supports reference output")
	}
	output := map[string]any{params.outputFieldName: binaryMetadata(binary, 0)}
	if params.includeMetadata {
		metadata := binaryMetadata(binary, 0)
		output["binary"] = metadata
		output["fileName"] = metadata["fileName"]
		output["mimeType"] = metadata["mimeType"]
		output["fileSize"] = metadata["fileSize"]
		output["fileExtension"] = metadata["fileExtension"]
	}
	return []map[string]any{output}, nil
}

func binaryMetadata(binary dataplane.Binary, size int) map[string]any {
	fileSize := binary.FileSize
	if fileSize == 0 {
		fileSize = int64(size)
	}
	return map[string]any{
		"id":            binary.ID,
		"fileName":      binary.FileName,
		"mimeType":      binary.MimeType,
		"fileType":      binary.FileType,
		"fileSize":      fileSize,
		"fileExtension": binary.FileExtension,
		"directory":     binary.Directory,
	}
}

func extractHTMLData(data []byte, params extractParams) ([]map[string]any, error) {
	document, err := nethtml.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("html: parse document: %w", err)
	}
	operation := strings.ToLower(firstNonEmptyNode(params.htmlOperation, "fullText"))
	switch operation {
	case "fulltext", "extracttext", "text":
		return extractHTMLText(document, params)
	case "extracttable", "table":
		return extractHTMLTable(document, params)
	case "extractlinks", "links":
		return extractHTMLLinks(document, params), nil
	case "extractimages", "images":
		return extractHTMLImages(document, params), nil
	case "cssselector", "selector":
		return extractHTMLSelector(document, params)
	case "structureddata", "metadata":
		return extractHTMLStructuredData(document), nil
	default:
		return nil, fmt.Errorf("html: unsupported operation %s", params.htmlOperation)
	}
}

func extractHTMLText(document *nethtml.Node, params extractParams) ([]map[string]any, error) {
	if params.selector != "" {
		nodes, err := htmlSelect(document, params.selector)
		if err != nil {
			return nil, err
		}
		if len(nodes) == 0 {
			return []map[string]any{{"text": ""}}, nil
		}
		if params.returnAll {
			values := make([]string, 0, len(nodes))
			for _, node := range nodes {
				values = append(values, htmlNodeText(node, params.trimText))
			}
			return []map[string]any{{"texts": values, "text": strings.Join(values, "\n")}}, nil
		}
		return []map[string]any{{"text": htmlNodeText(nodes[0], params.trimText)}}, nil
	}
	body := htmlFirst(document, "body")
	if body == nil {
		body = document
	}
	return []map[string]any{{"text": htmlNodeText(body, params.trimText)}}, nil
}

func extractHTMLTable(document *nethtml.Node, params extractParams) ([]map[string]any, error) {
	tables := htmlFindAll(document, "table")
	if len(tables) == 0 {
		return nil, fmt.Errorf("html: no tables found")
	}
	if params.tableIndex < 0 || params.tableIndex >= len(tables) {
		return nil, fmt.Errorf("html: table index %d out of range", params.tableIndex)
	}
	rows := htmlTableRows(tables[params.tableIndex])
	if len(rows) == 0 {
		return []map[string]any{}, nil
	}
	if params.headerRow {
		headers := normalizeHeaders(rows[0])
		output := make([]map[string]any, 0, len(rows)-1)
		for _, row := range rows[1:] {
			item := make(map[string]any, len(headers))
			for index, header := range headers {
				if index < len(row) {
					item[header] = row[index]
				} else {
					item[header] = ""
				}
			}
			output = append(output, item)
		}
		return output, nil
	}
	headers := generatedHeaders(maxRowWidth(rows))
	output := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := make(map[string]any, len(headers))
		for index, header := range headers {
			if index < len(row) {
				item[header] = row[index]
			} else {
				item[header] = ""
			}
		}
		output = append(output, item)
	}
	return output, nil
}

func extractHTMLLinks(document *nethtml.Node, params extractParams) []map[string]any {
	anchors := htmlFindAll(document, "a")
	output := make([]map[string]any, 0, len(anchors))
	for _, anchor := range anchors {
		href := htmlAttr(anchor, "href")
		if href == "" {
			continue
		}
		href = resolveHTMLURL(href, params.linkBase)
		if params.onlyInternal && !htmlIsInternalURL(href, params.linkBase) {
			continue
		}
		output = append(output, map[string]any{
			"url":   href,
			"text":  htmlNodeText(anchor, true),
			"title": htmlAttr(anchor, "title"),
			"rel":   htmlAttr(anchor, "rel"),
		})
	}
	return output
}

func extractHTMLImages(document *nethtml.Node, params extractParams) []map[string]any {
	images := htmlFindAll(document, "img")
	output := make([]map[string]any, 0, len(images))
	for _, image := range images {
		src := firstNonEmptyNode(htmlAttr(image, "src"), htmlAttr(image, "data-src"))
		if src == "" {
			continue
		}
		alt := htmlAttr(image, "alt")
		if params.onlyWithAlt && alt == "" {
			continue
		}
		output = append(output, map[string]any{
			"src":    resolveHTMLURL(src, params.linkBase),
			"alt":    alt,
			"title":  htmlAttr(image, "title"),
			"width":  htmlAttr(image, "width"),
			"height": htmlAttr(image, "height"),
		})
	}
	return output
}

func extractHTMLSelector(document *nethtml.Node, params extractParams) ([]map[string]any, error) {
	if params.selector == "" {
		return nil, fmt.Errorf("html: selector is required")
	}
	nodes, err := htmlSelect(document, params.selector)
	if err != nil {
		return nil, err
	}
	if !params.returnAll && len(nodes) > 1 {
		nodes = nodes[:1]
	}
	output := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		output = append(output, map[string]any{
			"text":       htmlNodeText(node, params.trimText),
			"innerHTML":  htmlInnerHTML(node),
			"attributes": htmlAttributes(node),
			"tagName":    node.Data,
		})
	}
	return output, nil
}

func extractHTMLStructuredData(document *nethtml.Node) []map[string]any {
	output := make([]map[string]any, 0)
	for _, script := range htmlFindAll(document, "script") {
		if !strings.EqualFold(htmlAttr(script, "type"), "application/ld+json") || script.FirstChild == nil {
			continue
		}
		var decoded any
		if json.Unmarshal([]byte(script.FirstChild.Data), &decoded) != nil {
			continue
		}
		switch typed := decoded.(type) {
		case map[string]any:
			output = append(output, typed)
		case []any:
			for _, value := range typed {
				if object, ok := value.(map[string]any); ok {
					output = append(output, object)
				}
			}
		}
	}
	openGraph := map[string]any{}
	for _, meta := range htmlFindAll(document, "meta") {
		property := htmlAttr(meta, "property")
		content := htmlAttr(meta, "content")
		if content == "" {
			continue
		}
		if strings.HasPrefix(property, "og:") || strings.HasPrefix(property, "twitter:") {
			openGraph[strings.ReplaceAll(property, ":", "_")] = content
		}
	}
	if len(openGraph) > 0 {
		openGraph["@type"] = "OpenGraph"
		output = append(output, openGraph)
	}
	return output
}

func htmlSelect(document *nethtml.Node, selector string) ([]*nethtml.Node, error) {
	parsed, err := cascadia.ParseGroup(selector)
	if err != nil {
		return nil, fmt.Errorf("html: invalid selector %s: %w", selector, err)
	}
	return cascadia.QueryAll(document, parsed), nil
}

func htmlFirst(node *nethtml.Node, tag string) *nethtml.Node {
	if node.Type == nethtml.ElementNode && node.Data == tag {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := htmlFirst(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func htmlFindAll(node *nethtml.Node, tag string) []*nethtml.Node {
	output := make([]*nethtml.Node, 0)
	var visit func(*nethtml.Node)
	visit = func(current *nethtml.Node) {
		if current.Type == nethtml.ElementNode && current.Data == tag {
			output = append(output, current)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return output
}

func htmlTableRows(table *nethtml.Node) [][]string {
	rows := make([][]string, 0)
	for _, tr := range htmlFindAll(table, "tr") {
		cells := make([]string, 0)
		for child := tr.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == nethtml.ElementNode && (child.Data == "th" || child.Data == "td") {
				cells = append(cells, htmlNodeText(child, true))
			}
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	return rows
}

func htmlNodeText(node *nethtml.Node, trim bool) string {
	var builder strings.Builder
	htmlTextRecursive(node, &builder)
	text := builder.String()
	if !trim {
		return text
	}
	lines := make([]string, 0)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func htmlTextRecursive(node *nethtml.Node, builder *strings.Builder) {
	if node.Type == nethtml.TextNode {
		builder.WriteString(node.Data)
		return
	}
	if node.Type == nethtml.ElementNode {
		switch node.Data {
		case "script", "style", "head", "noscript", "svg", "canvas":
			return
		case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "section", "article":
			builder.WriteString("\n")
			defer builder.WriteString("\n")
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		htmlTextRecursive(child, builder)
	}
}

func htmlAttr(node *nethtml.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func htmlAttributes(node *nethtml.Node) map[string]any {
	attributes := make(map[string]any, len(node.Attr))
	for _, attr := range node.Attr {
		attributes[attr.Key] = attr.Val
	}
	return attributes
}

func htmlInnerHTML(node *nethtml.Node) string {
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		_ = nethtml.Render(&builder, child)
	}
	return builder.String()
}

func resolveHTMLURL(raw string, base string) string {
	if base == "" {
		return raw
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return baseURL.ResolveReference(ref).String()
}

func htmlIsInternalURL(raw string, base string) bool {
	if base == "" {
		return true
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return true
	}
	ref, err := url.Parse(raw)
	if err != nil || ref.Host == "" {
		return true
	}
	return strings.EqualFold(ref.Host, baseURL.Host)
}

func skipRows(rows [][]string, count int) [][]string {
	if count <= 0 {
		return rows
	}
	if count >= len(rows) {
		return [][]string{}
	}
	return rows[count:]
}

func tabularRowsAsObjects(rows [][]string, params extractParams, format string) ([]map[string]any, error) {
	if len(rows) == 0 {
		return []map[string]any{}, nil
	}
	headerIndex := params.headerRowIndex
	if headerIndex < 0 {
		headerIndex = 0
	}
	if params.headerRow && headerIndex >= len(rows) {
		return nil, fmt.Errorf("%s: header row %d out of range", format, headerIndex)
	}
	headers := make([]string, 0)
	dataStart := 0
	if params.headerRow {
		headers = normalizeHeaders(rows[headerIndex])
		dataStart = headerIndex + 1
	}
	maxColumns := maxRowWidth(rows)
	if !params.headerRow {
		headers = generatedHeaders(maxColumns)
	}
	result := make([]map[string]any, 0, max(0, len(rows)-dataStart))
	for _, row := range rows[dataStart:] {
		if len(row) == 0 {
			continue
		}
		entry := make(map[string]any, len(headers))
		for columnIndex, header := range headers {
			if columnIndex >= len(row) {
				value := emptyExtractValue(params.emptyValues)
				if !isSkipExtractValue(value) {
					entry[header] = value
				}
				continue
			}
			value := convertExtractValue(row[columnIndex], params)
			if !isSkipExtractValue(value) {
				entry[header] = value
			}
		}
		for columnIndex := len(headers); columnIndex < len(row); columnIndex++ {
			value := convertExtractValue(row[columnIndex], params)
			if !isSkipExtractValue(value) {
				entry[fmt.Sprintf("extra_%d", columnIndex-len(headers)+1)] = value
			}
		}
		result = append(result, entry)
	}
	return result, nil
}

func csvRowsAsArrays(rows [][]string, params extractParams) []map[string]any {
	all := make([]any, 0, len(rows))
	for _, row := range rows {
		values := make([]any, 0, len(row))
		for _, value := range row {
			converted := convertExtractValue(value, params)
			if !isSkipExtractValue(converted) {
				values = append(values, converted)
			}
		}
		all = append(all, values)
	}
	return []map[string]any{{"rows": all}}
}

func normalizeHeaders(values []string) []string {
	seen := map[string]int{}
	headers := make([]string, 0, len(values))
	for index, value := range values {
		header := strings.TrimSpace(value)
		if header == "" {
			header = fmt.Sprintf("column_%d", index)
		}
		header = sanitizeExtractHeader(header)
		count := seen[header]
		seen[header] = count + 1
		if count > 0 {
			header = fmt.Sprintf("%s_%d", header, count+1)
		}
		headers = append(headers, header)
	}
	return headers
}

func sanitizeExtractHeader(header string) string {
	header = strings.ReplaceAll(strings.TrimSpace(header), " ", "_")
	var builder strings.Builder
	for _, char := range header {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' {
			builder.WriteRune(char)
		}
	}
	result := builder.String()
	if result == "" {
		return "column"
	}
	return result
}

func generatedHeaders(width int) []string {
	headers := make([]string, 0, width)
	for index := 0; index < width; index++ {
		headers = append(headers, fmt.Sprintf("field_%d", index))
	}
	return headers
}

func maxRowWidth(rows [][]string) int {
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	return width
}

type skipValue struct{}

func isSkipExtractValue(value any) bool {
	_, ok := value.(skipValue)
	return ok
}

func convertExtractValue(value string, params extractParams) any {
	if value == "" {
		return emptyExtractValue(params.emptyValues)
	}
	if !params.convertTypes {
		return value
	}
	trimmed := strings.TrimSpace(value)
	if strings.EqualFold(trimmed, "true") {
		return true
	}
	if strings.EqualFold(trimmed, "false") {
		return false
	}
	if intValue, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return intValue
	}
	if floatValue, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return floatValue
	}
	if parsed := convertExtractDate(trimmed, params); parsed != trimmed {
		return parsed
	}
	return value
}

func emptyExtractValue(mode string) any {
	switch strings.ToLower(mode) {
	case "skip":
		return skipValue{}
	case "empty-string", "emptystring", "string":
		return ""
	default:
		return nil
	}
}

func detectCSVDelimiter(sample string, configured string) string {
	switch configured {
	case "", "auto":
	case "\\t", "tab":
		return "\t"
	default:
		return configured
	}
	bestDelimiter := ","
	bestScore := -1
	for _, delimiter := range []string{",", ";", "\t", "|", ":"} {
		score := delimiterScore(sample, delimiter)
		if score > bestScore {
			bestScore = score
			bestDelimiter = delimiter
		}
	}
	return bestDelimiter
}

func delimiterScore(sample string, delimiter string) int {
	score := 0
	lines := strings.Split(sample, "\n")
	if len(lines) > 10 {
		lines = lines[:10]
	}
	expected := -1
	consistent := true
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		count := strings.Count(line, delimiter)
		if expected == -1 {
			expected = count
		} else if count != expected {
			consistent = false
		}
		if count > 0 {
			score += count * 10
		}
	}
	if consistent && expected > 0 {
		score *= 2
	}
	return score
}

func normalizeCSVQuotes(text string, params extractParams) string {
	quote := []rune(params.quoteChar)
	if len(quote) == 1 && quote[0] != '"' {
		text = strings.ReplaceAll(text, params.quoteChar, `"`)
	}
	escape := []rune(params.escapeChar)
	if len(escape) == 1 && escape[0] != '"' {
		text = strings.ReplaceAll(text, params.escapeChar+`"`, `""`)
	}
	return text
}

func decodedReader(reader io.Reader, encodingName string) (io.Reader, error) {
	preview := make([]byte, 4)
	read, err := reader.Read(preview)
	if err != nil && err != io.EOF {
		return nil, err
	}
	preview = preview[:read]
	enc, bomLength := detectEncodingFromBOM(preview)
	fullReader := io.MultiReader(bytes.NewReader(preview[bomLength:]), reader)
	if enc != nil {
		return transform.NewReader(fullReader, enc.NewDecoder()), nil
	}
	enc, err = namedEncoding(encodingName)
	if err != nil {
		return nil, err
	}
	if enc == nil {
		return fullReader, nil
	}
	return transform.NewReader(fullReader, enc.NewDecoder()), nil
}

func detectEncodingFromBOM(data []byte) (encoding.Encoding, int) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return nil, 3
	}
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		return unicodeenc.UTF16(unicodeenc.LittleEndian, unicodeenc.UseBOM), 2
	}
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return unicodeenc.UTF16(unicodeenc.BigEndian, unicodeenc.UseBOM), 2
	}
	return nil, 0
}

func namedEncoding(name string) (encoding.Encoding, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), "_", ""))
	switch normalized {
	case "", "AUTO", "UTF8":
		return nil, nil
	case "UTF16", "UTF16LE":
		return unicodeenc.UTF16(unicodeenc.LittleEndian, unicodeenc.IgnoreBOM), nil
	case "UTF16BE":
		return unicodeenc.UTF16(unicodeenc.BigEndian, unicodeenc.IgnoreBOM), nil
	case "LATIN1", "ISO88591":
		return charmap.ISO8859_1, nil
	case "WINDOWS1252", "CP1252":
		return charmap.Windows1252, nil
	case "WINDOWS1250", "CP1250":
		return charmap.Windows1250, nil
	case "ISO88592":
		return charmap.ISO8859_2, nil
	default:
		return nil, fmt.Errorf("encoding: unsupported encoding %s", name)
	}
}
