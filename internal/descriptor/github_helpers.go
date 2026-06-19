package descriptor

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func DecodeGitHubFileContent(item map[string]any) (string, error) {
	content, _ := item["content"].(string)
	encoding, _ := item["encoding"].(string)
	if !strings.EqualFold(encoding, "base64") {
		return content, nil
	}
	content = strings.ReplaceAll(content, "\n", "")
	content = strings.ReplaceAll(content, "\r", "")
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return "", fmt.Errorf("github file content decode: %w", err)
	}
	return string(decoded), nil
}

func EncodeGitHubFileContent(content string) string {
	return base64.StdEncoding.EncodeToString([]byte(content))
}
