package sourcecontrol

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type ConfigRepository interface {
	Load(ctx context.Context) (*Config, error)
	Save(ctx context.Context, cfg Config) error
}

type FileConfigRepository struct {
	path string
}

func NewFileConfigRepository(path string) *FileConfigRepository {
	return &FileConfigRepository{path: path}
}

func (r *FileConfigRepository) Load(ctx context.Context) (*Config, error) {
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *FileConfigRepository) Save(ctx context.Context, cfg Config) error {
	now := time.Now().UTC()
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	cfg.UpdatedAt = now
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0600)
}
