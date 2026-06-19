package binarydata

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"sync"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

type Ref struct {
	ID       string `json:"id"`
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType"`
	FileSize int64  `json:"fileSize"`
	Backend  string `json:"backend"`
}

type Store interface {
	Open(ctx context.Context, ref Ref) (io.ReadCloser, error)
	Stat(ctx context.Context, id string) (Ref, error)
	Put(ctx context.Context, mimeType string, fileName string, reader io.Reader) (Ref, error)
	Delete(ctx context.Context, ref Ref) error
	DeleteMany(ctx context.Context, refs []Ref) error
	Exists(ctx context.Context, ref Ref) (bool, error)
	CopyTo(ctx context.Context, ref Ref, target Store) (Ref, error)
}

type ExecutionStore struct {
	store       Store
	executionID string
	refs        []Ref
	mu          sync.Mutex
}

func NewExecutionStore(store Store, executionID string) *ExecutionStore {
	return &ExecutionStore{store: store, executionID: executionID}
}

func (s *ExecutionStore) Open(ctx context.Context, ref Ref) (io.ReadCloser, error) {
	return s.store.Open(ctx, ref)
}

func (s *ExecutionStore) Stat(ctx context.Context, id string) (Ref, error) {
	return s.store.Stat(ctx, id)
}

func (s *ExecutionStore) Put(ctx context.Context, mimeType string, fileName string, reader io.Reader) (Ref, error) {
	ref, err := s.store.Put(ctx, mimeType, fileName, reader)
	if err != nil {
		return Ref{}, err
	}
	s.mu.Lock()
	s.refs = append(s.refs, ref)
	s.mu.Unlock()
	return ref, nil
}

func (s *ExecutionStore) Delete(ctx context.Context, ref Ref) error {
	if err := s.store.Delete(ctx, ref); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, stored := range s.refs {
		if stored.ID == ref.ID {
			s.refs = append(s.refs[:index], s.refs[index+1:]...)
			break
		}
	}
	return nil
}

func (s *ExecutionStore) DeleteMany(ctx context.Context, refs []Ref) error {
	if err := s.store.DeleteMany(ctx, refs); err != nil {
		return err
	}
	remove := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		remove[ref.ID] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.refs[:0]
	for _, ref := range s.refs {
		if _, ok := remove[ref.ID]; !ok {
			kept = append(kept, ref)
		}
	}
	s.refs = kept
	return nil
}

func (s *ExecutionStore) Exists(ctx context.Context, ref Ref) (bool, error) {
	return s.store.Exists(ctx, ref)
}

func (s *ExecutionStore) CopyTo(ctx context.Context, ref Ref, target Store) (Ref, error) {
	return s.store.CopyTo(ctx, ref, target)
}

func (s *ExecutionStore) CleanupAll(ctx context.Context) error {
	s.mu.Lock()
	refs := append([]Ref(nil), s.refs...)
	s.mu.Unlock()
	if err := s.store.DeleteMany(ctx, refs); err != nil {
		return err
	}
	s.mu.Lock()
	s.refs = nil
	s.mu.Unlock()
	return nil
}

func (s *ExecutionStore) GetAllRefs() []Ref {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Ref(nil), s.refs...)
}

func RefFromBinary(binary dataplane.Binary) Ref {
	return Ref{
		ID:       binary.ID,
		FileName: binary.FileName,
		MimeType: binary.MimeType,
		FileSize: binary.FileSize,
		Backend:  binary.Directory,
	}
}

func BinaryFromRef(ref Ref) dataplane.Binary {
	return dataplane.Binary{
		ID:        ref.ID,
		MimeType:  ref.MimeType,
		FileName:  ref.FileName,
		FileSize:  ref.FileSize,
		Directory: ref.Backend,
	}
}

func Open(ctx context.Context, store Store, binary dataplane.Binary) (io.ReadCloser, error) {
	if binary.Data != "" {
		data, err := base64.StdEncoding.DecodeString(binary.Data)
		if err != nil {
			return nil, fmt.Errorf("binarydata: decode inline binary: %w", err)
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	if store == nil || binary.ID == "" {
		return nil, fmt.Errorf("binarydata: binary %s has no readable data", binary.FileName)
	}
	return store.Open(ctx, RefFromBinary(binary))
}

func Read(ctx context.Context, store Store, binary dataplane.Binary) ([]byte, error) {
	reader, err := Open(ctx, store, binary)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("binarydata: read stored binary: %w", err)
	}
	return data, nil
}
