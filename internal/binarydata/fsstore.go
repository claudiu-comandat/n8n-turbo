package binarydata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type FSStore struct {
	basePath string
}

type fsMetadata struct {
	Ref
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
}

func NewFSStore(basePath string) (*FSStore, error) {
	if strings.TrimSpace(basePath) == "" {
		basePath = filepath.Join(".", "storage", "binary")
	}
	absolute, err := filepath.Abs(basePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(absolute, "index"), 0o750); err != nil {
		return nil, fmt.Errorf("fsstore: create base directory: %w", err)
	}
	return &FSStore{basePath: absolute}, nil
}

func (s *FSStore) Open(ctx context.Context, ref Ref) (io.ReadCloser, error) {
	metadata, err := s.metadata(ref.ID)
	if err != nil {
		return nil, err
	}
	path, err := s.dataPath(metadata.Path)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("fsstore: binary %s not found", ref.ID)
		}
		return nil, fmt.Errorf("fsstore: open binary: %w", err)
	}
	return &contextReader{ReadCloser: file, ctx: ctx}, nil
}

func (s *FSStore) Stat(ctx context.Context, id string) (Ref, error) {
	select {
	case <-ctx.Done():
		return Ref{}, ctx.Err()
	default:
	}
	metadata, err := s.metadata(id)
	if err != nil {
		return Ref{}, err
	}
	return metadata.Ref, nil
}

func (s *FSStore) Put(ctx context.Context, mimeType string, fileName string, reader io.Reader) (Ref, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	relativeDir := filepath.Join(fmt.Sprintf("%04d", now.Year()), fmt.Sprintf("%02d", now.Month()), fmt.Sprintf("%02d", now.Day()))
	if err := os.MkdirAll(filepath.Join(s.basePath, relativeDir), 0o750); err != nil {
		return Ref{}, fmt.Errorf("fsstore: create data directory: %w", err)
	}
	relativePath := filepath.Join(relativeDir, id)
	finalPath := filepath.Join(s.basePath, relativePath)
	tmpPath := finalPath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
	if err != nil {
		return Ref{}, fmt.Errorf("fsstore: create temp file: %w", err)
	}
	written, copyErr := copyContext(ctx, file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return Ref{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return Ref{}, closeErr
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return Ref{}, fmt.Errorf("fsstore: commit binary: %w", err)
	}
	ref := Ref{ID: id, FileName: fileName, MimeType: mimeType, FileSize: written, Backend: "filesystem"}
	metadata := fsMetadata{Ref: ref, Path: filepath.ToSlash(relativePath), CreatedAt: now}
	if err := s.writeMetadata(metadata); err != nil {
		_ = os.Remove(finalPath)
		return Ref{}, err
	}
	return ref, nil
}

func (s *FSStore) Delete(ctx context.Context, ref Ref) error {
	metadata, err := s.metadata(ref.ID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	path, err := s.dataPath(metadata.Path)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("fsstore: delete binary: %w", err)
	}
	if err := os.Remove(s.indexPath(ref.ID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("fsstore: delete metadata: %w", err)
	}
	s.cleanEmptyDirs(filepath.Dir(path))
	return nil
}

func (s *FSStore) DeleteMany(ctx context.Context, refs []Ref) error {
	var errs []error
	for _, ref := range refs {
		if err := s.Delete(ctx, ref); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("fsstore: delete many: %v", errs)
	}
	return nil
}

func (s *FSStore) Exists(ctx context.Context, ref Ref) (bool, error) {
	metadata, err := s.metadata(ref.ID)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	path, err := s.dataPath(metadata.Path)
	if err != nil {
		return false, err
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *FSStore) CopyTo(ctx context.Context, ref Ref, target Store) (Ref, error) {
	reader, err := s.Open(ctx, ref)
	if err != nil {
		return Ref{}, err
	}
	defer reader.Close()
	stored, err := s.metadata(ref.ID)
	if err != nil {
		return Ref{}, err
	}
	return target.Put(ctx, stored.MimeType, stored.FileName, reader)
}

func (s *FSStore) metadata(id string) (fsMetadata, error) {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return fsMetadata{}, os.ErrNotExist
	}
	data, err := os.ReadFile(s.indexPath(id))
	if err != nil {
		return fsMetadata{}, err
	}
	var metadata fsMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return fsMetadata{}, fmt.Errorf("fsstore: decode metadata: %w", err)
	}
	return metadata, nil
}

func (s *FSStore) writeMetadata(metadata fsMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	tmpPath := s.indexPath(metadata.ID) + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o640); err != nil {
		return fmt.Errorf("fsstore: write metadata: %w", err)
	}
	if err := os.Rename(tmpPath, s.indexPath(metadata.ID)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsstore: commit metadata: %w", err)
	}
	return nil
}

func (s *FSStore) indexPath(id string) string {
	return filepath.Join(s.basePath, "index", id+".json")
}

func (s *FSStore) dataPath(relativePath string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("fsstore: invalid binary path")
	}
	path := filepath.Join(s.basePath, clean)
	relative, err := filepath.Rel(s.basePath, path)
	if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", fmt.Errorf("fsstore: binary path outside store")
	}
	return path, nil
}

func (s *FSStore) cleanEmptyDirs(path string) {
	for {
		if path == s.basePath || path == "." || path == string(filepath.Separator) {
			return
		}
		if err := os.Remove(path); err != nil {
			return
		}
		path = filepath.Dir(path)
	}
}

func copyContext(ctx context.Context, writer io.Writer, reader io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			w, writeErr := writer.Write(buffer[:n])
			written += int64(w)
			if writeErr != nil {
				return written, fmt.Errorf("fsstore: write binary: %w", writeErr)
			}
			if w != n {
				return written, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, fmt.Errorf("fsstore: read input: %w", readErr)
		}
	}
}

type contextReader struct {
	io.ReadCloser
	ctx context.Context
}

func (r *contextReader) Read(data []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.ReadCloser.Read(data)
	}
}
