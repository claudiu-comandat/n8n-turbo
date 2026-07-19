// Package cloudinary implements the community node n8n-nodes-cloudinary.cloudinary
// natively in Go. n8n-turbo cannot load JS community nodes, so the one operation
// the Comandat workflows use — uploadFile (signed upload) — is reimplemented here.
package cloudinary

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

const NodeType = "n8n-nodes-cloudinary.cloudinary"

type Node struct {
	client  *http.Client
	baseURL string // override for tests; default https://api.cloudinary.com
}

func New() *Node {
	return &Node{client: &http.Client{Timeout: 60 * time.Second}}
}

func NewWithBaseURL(baseURL string) *Node {
	n := New()
	n.baseURL = strings.TrimRight(baseURL, "/")
	return n
}

type credential struct {
	CloudName string
	APIKey    string
	APISecret string
}

func (n *Node) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	cred, err := extractCredential(in.Credentials)
	if err != nil {
		return nil, err
	}
	params := in.Node.Parameters
	operation := stringParam(params, "operation")
	if operation != "" && operation != "uploadFile" && operation != "upload" {
		return nil, fmt.Errorf("cloudinary operation %q not supported (only uploadFile)", operation)
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	out := make([]dataplane.Item, 0, len(items))
	for _, item := range items {
		result, err := n.upload(ctx, cred, params, item, in.BinaryStore)
		if err != nil {
			return nil, fmt.Errorf("cloudinary uploadFile: %w", err)
		}
		out = append(out, dataplane.Item{JSON: result})
	}
	return dataplane.MainOutput(out), nil
}

func (n *Node) upload(ctx context.Context, cred credential, params map[string]any, item dataplane.Item, store binarydata.Store) (map[string]any, error) {
	// Params that must be signed: everything sent except file, api_key, resource_type, cloud_name.
	signed := map[string]string{}
	for key, value := range additionalFields(params) {
		if value != "" {
			signed[key] = value
		}
	}
	signed["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	signature := sign(signed, cred.APISecret)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// File source: a URL/data-URI in params/json wins; otherwise the binary property (default "data").
	if fileURL := fileParam(params, item); fileURL != "" {
		if err := writer.WriteField("file", fileURL); err != nil {
			return nil, err
		}
	} else {
		reader, filename, err := openBinary(ctx, store, item, binaryPropertyName(params))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(part, reader); err != nil {
			return nil, err
		}
	}
	for key, value := range signed {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	if err := writer.WriteField("api_key", cred.APIKey); err != nil {
		return nil, err
	}
	if err := writer.WriteField("signature", signature); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.uploadURL(cred), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	response, err := n.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("cloudinary returned %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

func (n *Node) uploadURL(cred credential) string {
	base := n.baseURL
	if base == "" {
		base = "https://api.cloudinary.com"
	}
	return base + "/v1_1/" + cred.CloudName + "/image/upload"
}

// sign implements Cloudinary's signature: SHA1 of the sorted "k=v&k2=v2" params joined with the api_secret.
func sign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "&") + secret))
	return hex.EncodeToString(sum[:])
}

func extractCredential(credentials map[string]map[string]any) (credential, error) {
	pick := func(m map[string]any) credential {
		return credential{
			CloudName: stringValue(m, "cloudName", "cloud_name"),
			APIKey:    stringValue(m, "apiKey", "api_key"),
			APISecret: stringValue(m, "apiSecret", "api_secret"),
		}
	}
	if m, ok := credentials["cloudinaryApi"]; ok {
		if c := pick(m); c.valid() {
			return c, nil
		}
	}
	for _, m := range credentials {
		if c := pick(m); c.valid() {
			return c, nil
		}
	}
	return credential{}, fmt.Errorf("cloudinaryApi cloudName/apiKey/apiSecret are required")
}

func (c credential) valid() bool {
	return c.CloudName != "" && c.APIKey != "" && c.APISecret != ""
}

// additionalFields returns the signable upload options (public_id, folder, etc.).
func additionalFields(params map[string]any) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"additionalFieldsFile", "additionalFields", "options"} {
		if nested, ok := params[key].(map[string]any); ok {
			for field, value := range nested {
				if text := scalarString(value); text != "" {
					out[field] = text
				}
			}
		}
	}
	return out
}

func fileParam(params map[string]any, item dataplane.Item) string {
	// An explicit top-level `file` param always wins (user chose URL upload).
	if text := stringParam(params, "file"); text != "" && !strings.HasPrefix(text, "={{") {
		return text
	}
	// If the item carries binary, upload that — do NOT let an unrelated JSON `url`/`file`
	// field (common on scraped/DB items) silently override the attached image.
	if len(item.Binary) > 0 {
		return ""
	}
	for _, key := range []string{"file", "url", "secure_url", "imageUrl", "fileUrl"} {
		if item.JSON != nil {
			if text := scalarString(item.JSON[key]); strings.HasPrefix(text, "http") || strings.HasPrefix(text, "data:") {
				return text
			}
		}
	}
	return ""
}

func binaryPropertyName(params map[string]any) string {
	if name := stringParam(params, "binaryPropertyName", "binaryProperty"); name != "" {
		return name
	}
	return "data"
}

func openBinary(ctx context.Context, store binarydata.Store, item dataplane.Item, name string) (io.ReadCloser, string, error) {
	binary, ok := item.Binary[name]
	if !ok {
		for _, candidate := range item.Binary { // fall back to whatever binary is present
			binary, ok = candidate, true
			break
		}
	}
	if !ok {
		return nil, "", fmt.Errorf("no binary property %q on input item (and no file URL)", name)
	}
	reader, err := binarydata.Open(ctx, store, binary)
	if err != nil {
		return nil, "", err
	}
	filename := binary.FileName
	if filename == "" {
		filename = "file"
	}
	return reader, filename, nil
}

func firstInput(input dataplane.Output) []dataplane.Item {
	if len(input) == 0 || len(input[0]) == 0 {
		return nil
	}
	return input[0]
}

func stringParam(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			if text := scalarString(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func stringValue(m map[string]any, keys ...string) string { return stringParam(m, keys...) }

func scalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}
