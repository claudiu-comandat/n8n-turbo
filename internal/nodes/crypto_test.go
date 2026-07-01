package nodes

import (
	"context"
	"encoding/base64"
	"regexp"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestCryptoHashUsesOfficialTypeAndDataPropertyName(t *testing.T) {
	t.Parallel()

	out, err := (Crypto{}).Execute(context.Background(), testInput(map[string]any{
		"action":           "hash",
		"type":             "MD5",
		"value":            "hello",
		"dataPropertyName": "result.hash",
	}, []dataplane.Item{{JSON: map[string]any{"keep": "yes"}}}))
	if err != nil {
		t.Fatalf("crypto hash: %v", err)
	}
	result, ok := out[0][0].JSON["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested result, got %#v", out[0][0].JSON)
	}
	if result["hash"] != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("unexpected hash: %#v", result["hash"])
	}
	if out[0][0].PairedItem == nil || out[0][0].PairedItem.Item != 0 {
		t.Fatalf("expected paired item 0, got %#v", out[0][0].PairedItem)
	}
}

func TestCryptoGenerateUUIDUsesOfficialAction(t *testing.T) {
	t.Parallel()

	out, err := (Crypto{}).Execute(context.Background(), testInput(map[string]any{
		"action":           "generate",
		"dataPropertyName": "data",
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("crypto generate: %v", err)
	}
	value, ok := out[0][0].JSON["data"].(string)
	if !ok {
		t.Fatalf("expected generated string, got %#v", out[0][0].JSON)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(value) {
		t.Fatalf("expected UUID v4, got %q", value)
	}
	if out[0][0].PairedItem == nil || out[0][0].PairedItem.Item != 0 {
		t.Fatalf("expected paired item 0, got %#v", out[0][0].PairedItem)
	}
}

func TestCryptoHashSupportsOfficialSHA3Algorithms(t *testing.T) {
	t.Parallel()

	out, err := (Crypto{}).Execute(context.Background(), testInput(map[string]any{
		"action":           "hash",
		"type":             "SHA3-256",
		"value":            "hello",
		"dataPropertyName": "hash",
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("crypto sha3 hash: %v", err)
	}
	if out[0][0].JSON["hash"] != "3338be694f50c5f338814986cdf0686453a888b84f424d792af4b9202398f392" {
		t.Fatalf("unexpected sha3 hash: %#v", out[0][0].JSON["hash"])
	}
}

func TestCryptoHashBinaryDropsInputBinaryLikeOfficial(t *testing.T) {
	t.Parallel()

	out, err := (Crypto{}).Execute(context.Background(), testInput(map[string]any{
		"action":             "hash",
		"type":               "SHA256",
		"binaryData":         true,
		"binaryPropertyName": "data",
		"dataPropertyName":   "hash",
	}, []dataplane.Item{{
		JSON: map[string]any{"keep": "yes"},
		Binary: map[string]dataplane.Binary{
			"data": {Data: base64.StdEncoding.EncodeToString([]byte("hello")), FileName: "in.txt"},
		},
	}}))
	if err != nil {
		t.Fatalf("crypto hash binary: %v", err)
	}
	if out[0][0].JSON["hash"] != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("unexpected hash: %#v", out[0][0].JSON)
	}
	if out[0][0].Binary != nil {
		t.Fatalf("binary input should not be preserved for official binary hash output: %#v", out[0][0].Binary)
	}
}
