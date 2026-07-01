package nodes

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/xuri/excelize/v2"
)

func TestConvertToFileAcceptsOfficialToTextPerItem(t *testing.T) {
	t.Parallel()

	out, err := (ConvertToFile{}).Execute(context.Background(), testInput(map[string]any{
		"operation":          "toText",
		"sourceProperty":     "message",
		"binaryPropertyName": "file",
		"options":            map[string]any{"fileName": "message.txt"},
	}, []dataplane.Item{
		{JSON: map[string]any{"message": "hello"}},
		{JSON: map[string]any{"message": "world"}},
	}))
	if err != nil {
		t.Fatalf("convert to text execute: %v", err)
	}
	if len(out[0]) != 2 {
		t.Fatalf("expected one output per input item, got %#v", out[0])
	}
	decoded, _ := base64.StdEncoding.DecodeString(out[0][0].Binary["file"].Data)
	if string(decoded) != "hello" {
		t.Fatalf("unexpected text file content: %q", decoded)
	}
}

func TestConvertToFileDefaultsToOfficialCSVOperation(t *testing.T) {
	t.Parallel()

	out, err := (ConvertToFile{}).Execute(context.Background(), testInput(map[string]any{}, []dataplane.Item{
		{JSON: map[string]any{"id": 1, "name": "Ana"}},
	}))
	if err != nil {
		t.Fatalf("convert default execute: %v", err)
	}
	binary := out[0][0].Binary["data"]
	if binary.FileName != "export.csv" || !strings.Contains(binary.MimeType, "text/csv") {
		t.Fatalf("expected default CSV output, got %#v", binary)
	}
	decoded, _ := base64.StdEncoding.DecodeString(binary.Data)
	if !strings.Contains(string(decoded), "Ana") {
		t.Fatalf("unexpected CSV content: %q", decoded)
	}
}

func TestConvertToFileAcceptsOfficialToBinary(t *testing.T) {
	t.Parallel()

	out, err := (ConvertToFile{}).Execute(context.Background(), testInput(map[string]any{
		"operation":          "toBinary",
		"sourceProperty":     "payload",
		"binaryPropertyName": "data",
		"options": map[string]any{
			"fileName": "payload.bin",
			"mimeType": "application/octet-stream",
		},
	}, []dataplane.Item{{JSON: map[string]any{"payload": base64.StdEncoding.EncodeToString([]byte("payload"))}}}))
	if err != nil {
		t.Fatalf("convert to binary execute: %v", err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(out[0][0].Binary["data"].Data)
	if string(decoded) != "payload" {
		t.Fatalf("unexpected binary content: %q", decoded)
	}
}

func TestConvertToFileAcceptsOfficialToJsonEach(t *testing.T) {
	t.Parallel()

	out, err := (ConvertToFile{}).Execute(context.Background(), testInput(map[string]any{
		"operation":          "toJson",
		"mode":               "each",
		"binaryPropertyName": "data",
	}, []dataplane.Item{
		{JSON: map[string]any{"id": "1"}},
		{JSON: map[string]any{"id": "2"}},
	}))
	if err != nil {
		t.Fatalf("convert to json execute: %v", err)
	}
	if len(out[0]) != 2 {
		t.Fatalf("expected separate json files, got %#v", out[0])
	}
	decoded, _ := base64.StdEncoding.DecodeString(out[0][1].Binary["data"].Data)
	if !strings.Contains(string(decoded), `"id":"2"`) {
		t.Fatalf("unexpected json content: %q", decoded)
	}
}

func TestConvertToFileAcceptsMigrationXLSXOperation(t *testing.T) {
	t.Parallel()

	out, err := (ConvertToFile{}).Execute(context.Background(), testInput(map[string]any{
		"operation":          "xlsx",
		"binaryPropertyName": "data",
		"options": map[string]any{
			"fileName": "orders.xlsx",
		},
	}, []dataplane.Item{
		{JSON: map[string]any{"sku": "A1", "qty": 2}},
		{JSON: map[string]any{"sku": "B2", "qty": 3}},
	}))
	if err != nil {
		t.Fatalf("convert to xlsx execute: %v", err)
	}
	binary := out[0][0].Binary["data"]
	decoded, _ := base64.StdEncoding.DecodeString(binary.Data)
	file, err := excelize.OpenReader(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("open generated xlsx: %v", err)
	}
	defer file.Close()
	rows, err := file.GetRows(file.GetSheetName(0))
	if err != nil {
		t.Fatalf("read generated xlsx: %v", err)
	}
	if len(rows) != 3 || rows[1][0] != "2" || rows[1][1] != "A1" || rows[2][0] != "3" || rows[2][1] != "B2" {
		t.Fatalf("unexpected xlsx rows: %#v", rows)
	}
}

func TestConvertToFileAcceptsOfficialODSOperation(t *testing.T) {
	t.Parallel()

	content, mimeType, fileName, err := convertItemsToFile("ods", []map[string]any{
		{"sku": "A1", "qty": 2},
	}, map[string]any{})
	if err != nil {
		t.Fatalf("convert ods: %v", err)
	}
	if mimeType != "application/vnd.oasis.opendocument.spreadsheet" || fileName != "export.ods" {
		t.Fatalf("unexpected ods metadata: %s %s", mimeType, fileName)
	}
	rows, err := extractODSData(content, extractParams{headerRow: true, emptyValues: "null", convertTypes: true})
	if err != nil {
		t.Fatalf("extract generated ods: %v", err)
	}
	if len(rows) != 1 || rows[0]["sku"] != "A1" || rows[0]["qty"] != int64(2) {
		t.Fatalf("unexpected ods rows: %#v", rows)
	}
}

func TestConvertToFileAcceptsOfficialRTFOperation(t *testing.T) {
	t.Parallel()

	content, mimeType, fileName, err := convertItemsToFile("rtf", []map[string]any{
		{"name": "Ana", "note": `x\y`},
	}, map[string]any{})
	if err != nil {
		t.Fatalf("convert rtf: %v", err)
	}
	if mimeType != "application/rtf" || fileName != "export.rtf" {
		t.Fatalf("unexpected rtf metadata: %s %s", mimeType, fileName)
	}
	text := rtfPlainText(string(content))
	if !strings.Contains(text, "Ana") || !strings.Contains(text, `x\y`) {
		t.Fatalf("unexpected rtf content: %q", text)
	}
}
