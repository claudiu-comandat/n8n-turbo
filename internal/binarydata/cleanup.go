package binarydata

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type CleanupManager struct {
	mu      sync.Mutex
	stores  map[string]*ExecutionStore
	created map[string]time.Time
	logger  *slog.Logger
}

func NewCleanupManager(logger *slog.Logger) *CleanupManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &CleanupManager{stores: make(map[string]*ExecutionStore), created: make(map[string]time.Time), logger: logger}
}

func (m *CleanupManager) RegisterExecution(executionID string, store *ExecutionStore) {
	if executionID == "" || store == nil {
		return
	}
	m.mu.Lock()
	m.stores[executionID] = store
	m.created[executionID] = time.Now()
	m.mu.Unlock()
}

func (m *CleanupManager) CleanupExecution(ctx context.Context, executionID string) error {
	m.mu.Lock()
	store, ok := m.stores[executionID]
	if ok {
		delete(m.stores, executionID)
		delete(m.created, executionID)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	refs := store.GetAllRefs()
	if len(refs) == 0 {
		return nil
	}
	m.logger.Info("binary cleanup", "executionID", executionID, "files", len(refs))
	if err := store.CleanupAll(ctx); err != nil {
		return fmt.Errorf("binary cleanup %s: %w", executionID, err)
	}
	return nil
}

func (m *CleanupManager) CleanupAllExpired(ctx context.Context, maxAge time.Duration) error {
	now := time.Now()
	m.mu.Lock()
	ids := make([]string, 0, len(m.stores))
	for id, created := range m.created {
		if maxAge <= 0 || now.Sub(created) >= maxAge {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	var errs []error
	for _, id := range ids {
		if err := m.CleanupExecution(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("binary cleanup expired: %v", errs)
	}
	return nil
}
