package nodes

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestCompressionAcceptsOfficialGzipOutputField(t *testing.T) {
	t.Parallel()

	out, err := (Compression{}).Execute(context.Background(), testInput(map[string]any{
		"operation":            "compress",
		"outputFormat":         "gzip",
		"binaryPropertyName":   "data",
		"binaryPropertyOutput": "compressed",
		"fileName":             "payload.txt",
	}, []dataplane.Item{{JSON: map[string]any{}, Binary: map[string]dataplane.Binary{
		"data": {Data: base64.StdEncoding.EncodeToString([]byte("payload")), FileName: "payload.txt", FileExtension: "txt", MimeType: "text/plain"},
	}}}))
	if err != nil {
		t.Fatalf("compression execute: %v", err)
	}
	binary, ok := out[0][0].Binary["compressed"]
	if !ok {
		t.Fatalf("expected compressed output field, got %#v", out[0][0].Binary)
	}
	payload, _ := base64.StdEncoding.DecodeString(binary.Data)
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer reader.Close()
	var decoded bytes.Buffer
	if _, err := decoded.ReadFrom(reader); err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	if decoded.String() != "payload" {
		t.Fatalf("unexpected gzip payload: %q", decoded.String())
	}
}

func TestCompressionDecompressDefaultsToDataField(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, _ = writer.Write([]byte("hello"))
	_ = writer.Close()

	out, err := (Compression{}).Execute(context.Background(), testInput(map[string]any{
		"operation": "decompress",
	}, []dataplane.Item{{JSON: map[string]any{}, Binary: map[string]dataplane.Binary{
		"data": {Data: base64.StdEncoding.EncodeToString(buffer.Bytes()), FileName: "hello.txt.gz", FileExtension: "gz", MimeType: "application/gzip"},
	}}}))
	if err != nil {
		t.Fatalf("decompress execute: %v", err)
	}
	binary := out[0][0].Binary["file_0"]
	decoded, _ := base64.StdEncoding.DecodeString(binary.Data)
	if string(decoded) != "hello" {
		t.Fatalf("unexpected decompressed payload: %q", decoded)
	}
}

func TestCompressionDefaultsToOfficialDecompressOperation(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, _ = writer.Write([]byte("hello"))
	_ = writer.Close()

	out, err := (Compression{}).Execute(context.Background(), testInput(map[string]any{}, []dataplane.Item{{JSON: map[string]any{}, Binary: map[string]dataplane.Binary{
		"data": {Data: base64.StdEncoding.EncodeToString(buffer.Bytes()), FileName: "hello.txt.gz", FileExtension: "gz", MimeType: "application/gzip"},
	}}}))
	if err != nil {
		t.Fatalf("compression execute: %v", err)
	}
	if _, ok := out[0][0].Binary["file_0"]; !ok {
		t.Fatalf("default operation should decompress into file_0, got %#v", out[0][0].Binary)
	}
	if out[0][0].PairedItem == nil || out[0][0].PairedItem.Item != 0 {
		t.Fatalf("Compression should set paired item like n8n original: %#v", out[0][0].PairedItem)
	}
}
