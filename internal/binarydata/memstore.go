package binarydata

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/google/uuid"
)

type MemStore struct {
	mu   sync.RWMutex
	data map[string]memEntry
}

type memEntry struct {
	ref  Ref
	data []byte
}

func NewMemStore() *MemStore {
	return &MemStore{data: make(map[string]memEntry)}
}

func (s *MemStore) Open(ctx context.Context, ref Ref) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	s.mu.RLock()
	entry, ok := s.data[ref.ID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("memstore: binary %s not found", ref.ID)
	}
	return io.NopCloser(bytes.NewReader(entry.data)), nil
}

func (s *MemStore) Stat(ctx context.Context, id string) (Ref, error) {
	select {
	case <-ctx.Done():
		return Ref{}, ctx.Err()
	default:
	}
	s.mu.RLock()
	entry, ok := s.data[id]
	s.mu.RUnlock()
	if !ok {
		return Ref{}, fmt.Errorf("memstore: binary %s not found", id)
	}
	return entry.ref, nil
}

func (s *MemStore) Put(ctx context.Context, mimeType string, fileName string, reader io.Reader) (Ref, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return Ref{}, fmt.Errorf("memstore: read input: %w", err)
	}
	select {
	case <-ctx.Done():
		return Ref{}, ctx.Err()
	default:
	}
	ref := Ref{ID: uuid.NewString(), FileName: fileName, MimeType: mimeType, FileSize: int64(len(data)), Backend: "memory"}
	s.mu.Lock()
	s.data[ref.ID] = memEntry{ref: ref, data: append([]byte(nil), data...)}
	s.mu.Unlock()
	return ref, nil
}

func (s *MemStore) Delete(ctx context.Context, ref Ref) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	delete(s.data, ref.ID)
	s.mu.Unlock()
	return nil
}

func (s *MemStore) DeleteMany(ctx context.Context, refs []Ref) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	for _, ref := range refs {
		delete(s.data, ref.ID)
	}
	s.mu.Unlock()
	return nil
}

func (s *MemStore) Exists(ctx context.Context, ref Ref) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}
	s.mu.RLock()
	_, ok := s.data[ref.ID]
	s.mu.RUnlock()
	return ok, nil
}

func (s *MemStore) CopyTo(ctx context.Context, ref Ref, target Store) (Ref, error) {
	reader, err := s.Open(ctx, ref)
	if err != nil {
		return Ref{}, err
	}
	defer reader.Close()
	stored, err := s.Stat(ctx, ref.ID)
	if err != nil {
		return Ref{}, err
	}
	return target.Put(ctx, stored.MimeType, stored.FileName, reader)
}

func (s *MemStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}
