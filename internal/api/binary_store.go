package api

import (
	"context"
	"fmt"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/config"
)

func newBinaryStore(cfg config.Config) (binarydata.Store, error) {
	switch cfg.BinaryData.Mode {
	case "", "filesystem", "default":
		return binarydata.NewFSStore(cfg.BinaryData.Path)
	case "memory":
		return binarydata.NewMemStore(), nil
	case "s3":
		return binarydata.NewS3Store(context.Background(), binarydata.S3Config{
			Bucket:          cfg.BinaryData.S3Bucket,
			Region:          cfg.BinaryData.S3Region,
			AccessKeyID:     cfg.BinaryData.S3AccessKeyID,
			SecretAccessKey: cfg.BinaryData.S3SecretAccessKey,
			Endpoint:        cfg.BinaryData.S3Endpoint,
			ForcePathStyle:  cfg.BinaryData.S3ForcePathStyle,
			KeyPrefix:       cfg.BinaryData.S3KeyPrefix,
			UseSSL:          cfg.BinaryData.S3UseSSL,
		})
	default:
		return nil, fmt.Errorf("unsupported binary data mode %q", cfg.BinaryData.Mode)
	}
}
