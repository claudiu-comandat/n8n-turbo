package descriptor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Registry struct {
	mu          sync.RWMutex
	descriptors map[string]Descriptor
	executors   map[string]Executor
}

var globalRegistry = NewRegistry()

func NewRegistry() *Registry {
	return &Registry{descriptors: make(map[string]Descriptor), executors: make(map[string]Executor)}
}

func GetRegistry() *Registry {
	return globalRegistry
}

func Init() error {
	globalRegistry = NewRegistry()
	for _, descriptor := range Builtins() {
		globalRegistry.Register(descriptor)
	}
	return nil
}

func (r *Registry) Register(descriptor Descriptor) {
	descriptor.Normalize()
	r.mu.Lock()
	r.descriptors[descriptor.NodeType] = descriptor
	r.descriptors[descriptor.Name] = descriptor
	r.executors[descriptor.NodeType] = NewExecutor(descriptor)
	r.executors[descriptor.Name] = NewExecutor(descriptor)
	r.mu.Unlock()
}

func (r *Registry) RegisterFromFile(path string) error {
	descriptor, err := LoadFile(path)
	if err != nil {
		return err
	}
	r.Register(descriptor)
	return nil
}

func (r *Registry) LoadDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		if err := r.RegisterFromFile(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) GetExecutor(nodeName string) (Executor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	executor, ok := r.executors[nodeName]
	return executor, ok
}

func (r *Registry) GetDescriptor(nodeName string) (Descriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptor, ok := r.descriptors[nodeName]
	return descriptor, ok
}

func (r *Registry) ListDescriptors() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]struct{}{}
	result := make([]Descriptor, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		if _, ok := seen[descriptor.NodeType]; ok {
			continue
		}
		seen[descriptor.NodeType] = struct{}{}
		result = append(result, descriptor)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].NodeType < result[j].NodeType })
	return result
}

func (r *Registry) RegisterAllAsNodeExecutors(target engine.Registry) {
	for _, descriptor := range r.ListDescriptors() {
		target.Register(descriptor.NodeType, NewExecutor(descriptor))
	}
}

func MarshalDescriptor(descriptor Descriptor) ([]byte, error) {
	descriptor.Normalize()
	data, err := json.Marshal(descriptor)
	if err != nil {
		return nil, fmt.Errorf("descriptor marshal: %w", err)
	}
	return data, nil
}
