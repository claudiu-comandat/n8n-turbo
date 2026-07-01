package nodes

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestExtractFromFileAcceptsOfficialMoveToAliases(t *testing.T) {
	t.Parallel()

	params := extractParams{outputFieldName: "data"}
	rows, err := extractBinaryData(context.Background(), []byte("hello"), normalizeExtractOperation("binaryToPropery"), params, dataplane.Binary{})
	if err != nil {
		t.Fatalf("binaryToPropery execute: %v", err)
	}
	want := []map[string]any{{"data": "aGVsbG8="}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("unexpected binaryToPropery output\n got: %#v\nwant: %#v", rows, want)
	}
}

func TestExtractFromFileTextKeepsSourceJsonByDefaultLikeOfficial(t *testing.T) {
	t.Parallel()

	out, err := (ExtractFromFile{}).Execute(context.Background(), testInput(map[string]any{
		"operation":          "text",
		"binaryPropertyName": "data",
		"destinationKey":     "text",
	}, []dataplane.Item{{JSON: map[string]any{"id": "1"}, Binary: map[string]dataplane.Binary{
		"data": {Data: base64.StdEncoding.EncodeToString([]byte("hello")), FileName: "hello.txt", FileExtension: "txt", MimeType: "text/plain"},
		"keep": {Data: base64.StdEncoding.EncodeToString([]byte("sidecar")), FileName: "sidecar.txt", FileExtension: "txt", MimeType: "text/plain"},
	}}}))
	if err != nil {
		t.Fatalf("extract text execute: %v", err)
	}
	got := out[0][0]
	if got.JSON["id"] != "1" || got.JSON["text"] != "hello" {
		t.Fatalf("expected source json plus extracted text, got %#v", got.JSON)
	}
	if got.Binary == nil || got.Binary["keep"].FileName != "sidecar.txt" {
		t.Fatalf("default keepSource=json should keep non-processed binary fields, got %#v", got.Binary)
	}
	if _, ok := got.Binary["data"]; ok {
		t.Fatalf("default keepSource=json should remove processed binary field, got %#v", got.Binary)
	}
}

func TestExtractFromFileTextCanKeepSourceBinaryLikeOfficial(t *testing.T) {
	t.Parallel()

	out, err := (ExtractFromFile{}).Execute(context.Background(), testInput(map[string]any{
		"operation":          "text",
		"binaryPropertyName": "data",
		"destinationKey":     "text",
		"options":            map[string]any{"keepSource": "binary"},
	}, []dataplane.Item{{JSON: map[string]any{"id": "drop"}, Binary: map[string]dataplane.Binary{
		"data": {Data: base64.StdEncoding.EncodeToString([]byte("hello")), FileName: "hello.txt", FileExtension: "txt", MimeType: "text/plain"},
	}}}))
	if err != nil {
		t.Fatalf("extract text execute: %v", err)
	}
	got := out[0][0]
	if _, ok := got.JSON["id"]; ok || got.JSON["text"] != "hello" {
		t.Fatalf("keepSource=binary should keep only extracted json, got %#v", got.JSON)
	}
	if got.Binary == nil || got.Binary["data"].FileName != "hello.txt" {
		t.Fatalf("keepSource=binary should preserve source binary, got %#v", got.Binary)
	}
}

func TestExtractFromFilePDFUsesPopplerWhenAvailable(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext is not installed")
	}
	rows, err := extractPDFData(context.Background(), testPDF("Hello PDF"), extractParams{pdfJoinPages: true})
	if err != nil {
		t.Fatalf("pdf execute: %v", err)
	}
	text, _ := rows[0]["text"].(string)
	if !strings.Contains(text, "Hello PDF") {
		t.Fatalf("expected extracted PDF text, got %#v", rows)
	}
}

func testPDF(text string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\nBT /F1 24 Tf 100 700 Td (%s) Tj ET\nendstream", len("BT /F1 24 Tf 100 700 Td ("+text+") Tj ET\n"), text),
	}
	var builder strings.Builder
	builder.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects))
	for index, object := range objects {
		offsets = append(offsets, builder.Len())
		builder.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", index+1, object))
	}
	xrefOffset := builder.Len()
	builder.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(objects)+1))
	for _, offset := range offsets {
		builder.WriteString(fmt.Sprintf("%010d 00000 n \n", offset))
	}
	builder.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset))
	return []byte(builder.String())
}

func TestExtractFromFileAcceptsOfficialFromJsonAlias(t *testing.T) {
	t.Parallel()

	params := extractParams{outputFieldName: "data"}
	rows, err := extractBinaryData(context.Background(), []byte(`{"ok":true}`), normalizeExtractOperation("fromJson"), params, dataplane.Binary{})
	if err != nil {
		t.Fatalf("fromJson execute: %v", err)
	}
	want := []map[string]any{{"data": map[string]any{"ok": true}}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("unexpected fromJson output\n got: %#v\nwant: %#v", rows, want)
	}
}

func TestExtractFromFileAcceptsOfficialXLSAlias(t *testing.T) {
	t.Parallel()

	data, err := convertToXLSX([]map[string]any{{"sku": "A1"}}, nil)
	if err != nil {
		t.Fatalf("make xlsx fixture: %v", err)
	}
	rows, err := extractBinaryData(context.Background(), data, normalizeExtractOperation("xls"), extractParams{headerRow: true, emptyValues: "null", convertTypes: true}, dataplane.Binary{})
	if err != nil {
		t.Fatalf("xls alias execute: %v", err)
	}
	if len(rows) != 1 || rows[0]["sku"] != "A1" {
		t.Fatalf("unexpected xls alias rows: %#v", rows)
	}
}

func TestExtractFromFileAcceptsOfficialXMLAndRTFOperations(t *testing.T) {
	t.Parallel()

	xmlRows, err := extractBinaryData(context.Background(), []byte(`<root><name>Ana</name></root>`), "xml", extractParams{outputFieldName: "data"}, dataplane.Binary{})
	if err != nil {
		t.Fatalf("xml execute: %v", err)
	}
	if root, ok := xmlRows[0]["data"].(map[string]any)["root"].(map[string]any); !ok || root["name"] != "Ana" {
		t.Fatalf("unexpected xml rows: %#v", xmlRows)
	}

	rtfRows, err := extractBinaryData(context.Background(), []byte(`{\rtf1\ansi Hello \b Ana\b0}`), "rtf", extractParams{outputFieldName: "data"}, dataplane.Binary{})
	if err != nil {
		t.Fatalf("rtf execute: %v", err)
	}
	if rtfRows[0]["data"] != "Hello Ana" {
		t.Fatalf("unexpected rtf rows: %#v", rtfRows)
	}
}
