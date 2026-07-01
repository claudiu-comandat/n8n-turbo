package api

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"testing"
)

func TestParseWebhookPayloadMultipartFilesBecomeBinary(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	pdf, err := writer.CreateFormFile("pdfFile", "invoice.pdf")
	if err != nil {
		t.Fatalf("create pdf part: %v", err)
	}
	_, _ = pdf.Write([]byte("%PDF-1.7\ninvoice"))
	zipFile, err := writer.CreateFormFile("zipFile", "manifest.zip")
	if err != nil {
		t.Fatalf("create zip part: %v", err)
	}
	_, _ = zipFile.Write([]byte("PK\x03\x04zip"))
	if err := writer.WriteField("customer", "Acme"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, "/webhook/invoice", bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	payload, err := parseWebhookPayload(request, "invoice", nil, map[string]any{})
	if err != nil {
		t.Fatalf("parse webhook payload: %v", err)
	}
	if _, exists := payload.Item.JSON["rawBody"]; exists {
		t.Fatal("rawBody should not be stored in item JSON unless rawBody option is enabled")
	}
	if payload.Item.JSON["body"].(map[string]any)["customer"] != "Acme" {
		t.Fatalf("multipart fields missing from body: %#v", payload.Item.JSON["body"])
	}
	pdfBinary, ok := payload.Item.Binary["pdfFile"]
	if !ok {
		t.Fatalf("pdfFile binary missing: %#v", payload.Item.Binary)
	}
	decodedPDF, err := base64.StdEncoding.DecodeString(pdfBinary.Data)
	if err != nil {
		t.Fatalf("pdf binary is not base64: %v", err)
	}
	if string(decodedPDF) != "%PDF-1.7\ninvoice" {
		t.Fatalf("unexpected pdf payload: %q", decodedPDF)
	}
	if payload.Item.Binary["zipFile"].FileName != "manifest.zip" {
		t.Fatalf("zip metadata missing: %#v", payload.Item.Binary["zipFile"])
	}
}
