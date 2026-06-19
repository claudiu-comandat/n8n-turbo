package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

func SecurePath(basePath string, userPath string) (string, error) {
	if strings.TrimSpace(basePath) == "" {
		return "", fmt.Errorf("base path is required")
	}
	if filepath.IsAbs(userPath) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	absoluteBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}
	candidate := filepath.Clean(filepath.Join(absoluteBase, userPath))
	relative, err := filepath.Rel(absoluteBase, candidate)
	if err != nil {
		return "", err
	}
	if relative == "." {
		return candidate, nil
	}
	if strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path traversal detected")
	}
	return candidate, nil
}
