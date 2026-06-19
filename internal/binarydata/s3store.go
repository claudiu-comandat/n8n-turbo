package binarydata

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type S3Config struct {
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Endpoint        string
	ForcePathStyle  bool
	KeyPrefix       string
	UseSSL          bool
}

type S3Store struct {
	client   *http.Client
	cfg      S3Config
	endpoint *url.URL
}

func NewS3Store(ctx context.Context, cfg S3Config) (*S3Store, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("s3store: bucket is required")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		cfg.Region = "us-east-1"
	}
	if strings.TrimSpace(cfg.KeyPrefix) == "" {
		cfg.KeyPrefix = "n8n-binary"
	}
	endpoint, err := s3Endpoint(cfg)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return &S3Store{client: http.DefaultClient, cfg: cfg, endpoint: endpoint}, nil
}

func (s *S3Store) Open(ctx context.Context, ref Ref) (io.ReadCloser, error) {
	req, err := s.newRequest(ctx, http.MethodGet, s.refKey(ref), nil)
	if err != nil {
		return nil, err
	}
	s.sign(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3store: get object: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("s3store: get object status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (s *S3Store) Stat(ctx context.Context, id string) (Ref, error) {
	req, err := s.newRequest(ctx, http.MethodHead, s.normalizeKey(id), nil)
	if err != nil {
		return Ref{}, err
	}
	s.sign(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return Ref{}, fmt.Errorf("s3store: head object: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Ref{}, fmt.Errorf("s3store: head object status %d", resp.StatusCode)
	}
	size := resp.ContentLength
	if size < 0 {
		size, _ = strconv.ParseInt(resp.Header.Get("X-Amz-Meta-File-Size"), 10, 64)
	}
	fileName := resp.Header.Get("X-Amz-Meta-File-Name")
	if fileName == "" {
		fileName = filepath.Base(id)
	}
	return Ref{ID: s.normalizeKey(id), FileName: fileName, MimeType: resp.Header.Get("Content-Type"), FileSize: size, Backend: "s3"}, nil
}

func (s *S3Store) Put(ctx context.Context, mimeType string, fileName string, reader io.Reader) (Ref, error) {
	key := s.objectKey(uuid.NewString())
	counter := &countReader{reader: reader}
	req, err := s.newRequest(ctx, http.MethodPut, key, counter)
	if err != nil {
		return Ref{}, err
	}
	if mimeType != "" {
		req.Header.Set("Content-Type", mimeType)
	}
	if fileName != "" {
		req.Header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(fileName)))
		req.Header.Set("X-Amz-Meta-File-Name", filepath.Base(fileName))
	}
	s.sign(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return Ref{}, fmt.Errorf("s3store: put object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Ref{}, fmt.Errorf("s3store: put object status %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return Ref{ID: key, FileName: fileName, MimeType: mimeType, FileSize: counter.n, Backend: "s3"}, nil
}

func (s *S3Store) Delete(ctx context.Context, ref Ref) error {
	req, err := s.newRequest(ctx, http.MethodDelete, s.refKey(ref), nil)
	if err != nil {
		return err
	}
	s.sign(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("s3store: delete object: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("s3store: delete object status %d", resp.StatusCode)
	}
	return nil
}

func (s *S3Store) DeleteMany(ctx context.Context, refs []Ref) error {
	var errs []error
	for _, ref := range refs {
		if err := s.Delete(ctx, ref); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("s3store: delete many: %v", errs)
	}
	return nil
}

func (s *S3Store) Exists(ctx context.Context, ref Ref) (bool, error) {
	req, err := s.newRequest(ctx, http.MethodHead, s.refKey(ref), nil)
	if err != nil {
		return false, err
	}
	s.sign(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("s3store: head object: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("s3store: head object status %d", resp.StatusCode)
	}
	return true, nil
}

func (s *S3Store) CopyTo(ctx context.Context, ref Ref, target Store) (Ref, error) {
	reader, err := s.Open(ctx, ref)
	if err != nil {
		return Ref{}, err
	}
	defer reader.Close()
	stored, err := s.Stat(ctx, s.refKey(ref))
	if err != nil {
		return Ref{}, err
	}
	return target.Put(ctx, stored.MimeType, stored.FileName, reader)
}

func (s *S3Store) newRequest(ctx context.Context, method string, key string, body io.Reader) (*http.Request, error) {
	u := *s.endpoint
	key = strings.TrimLeft(key, "/")
	escaped := escapeS3Key(key)
	if s.cfg.ForcePathStyle || s.cfg.Endpoint != "" {
		u.Path = joinURLPath(s.endpoint.Path, s.cfg.Bucket, escaped)
	} else {
		u.Host = s.cfg.Bucket + "." + u.Host
		u.Path = joinURLPath(s.endpoint.Path, escaped)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	return req, nil
}

func (s *S3Store) sign(req *http.Request) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	if req.Header.Get("Content-Type") == "" && req.Method == http.MethodPut {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	canonicalHeaders, signedHeaders := canonicalS3Headers(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		req.Header.Get("X-Amz-Content-Sha256"),
	}, "\n")
	scope := shortDate + "/" + s.cfg.Region + "/s3/aws4_request"
	hash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(hash[:]),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(s.signingKey(shortDate), stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.cfg.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func (s *S3Store) signingKey(shortDate string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+s.cfg.SecretAccessKey), shortDate)
	kRegion := hmacSHA256(kDate, s.cfg.Region)
	kService := hmacSHA256(kRegion, "s3")
	return hmacSHA256(kService, "aws4_request")
}

func (s *S3Store) objectKey(id string) string {
	now := time.Now().UTC()
	prefix := strings.Trim(strings.ReplaceAll(s.cfg.KeyPrefix, "\\", "/"), "/")
	parts := []string{fmt.Sprintf("%04d", now.Year()), fmt.Sprintf("%02d", now.Month()), fmt.Sprintf("%02d", now.Day()), id}
	if prefix != "" {
		parts = append([]string{prefix}, parts...)
	}
	return strings.Join(parts, "/")
}

func (s *S3Store) refKey(ref Ref) string {
	return s.normalizeKey(ref.ID)
}

func (s *S3Store) normalizeKey(id string) string {
	return strings.TrimLeft(strings.ReplaceAll(id, "\\", "/"), "/")
}

func s3Endpoint(cfg S3Config) (*url.URL, error) {
	raw := strings.TrimSpace(cfg.Endpoint)
	if raw == "" {
		region := strings.TrimSpace(cfg.Region)
		if region == "" {
			region = "us-east-1"
		}
		raw = "https://s3." + region + ".amazonaws.com"
	} else if !strings.Contains(raw, "://") {
		if cfg.UseSSL {
			raw = "https://" + raw
		} else {
			raw = "http://" + raw
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("s3store: parse endpoint: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("s3store: invalid endpoint")
	}
	return u, nil
}

func canonicalS3Headers(req *http.Request) (string, string) {
	values := map[string]string{"host": req.URL.Host}
	for key, entries := range req.Header {
		lower := strings.ToLower(key)
		cleaned := make([]string, 0, len(entries))
		for _, entry := range entries {
			cleaned = append(cleaned, strings.Join(strings.Fields(entry), " "))
		}
		values[lower] = strings.Join(cleaned, ",")
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var headers strings.Builder
	for _, key := range keys {
		headers.WriteString(key)
		headers.WriteByte(':')
		headers.WriteString(values[key])
		headers.WriteByte('\n')
	}
	return headers.String(), strings.Join(keys, ";")
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func escapeS3Key(key string) string {
	parts := strings.Split(strings.TrimLeft(key, "/"), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func joinURLPath(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			clean = append(clean, part)
		}
	}
	return "/" + strings.Join(clean, "/")
}

type countReader struct {
	reader io.Reader
	n      int64
}

func (r *countReader) Read(data []byte) (int, error) {
	n, err := r.reader.Read(data)
	r.n += int64(n)
	return n, err
}
