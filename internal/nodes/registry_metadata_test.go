package nodes

import (
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

func TestRegisteredNodesExposeMetadataAndExactFileIcons(t *testing.T) {
	t.Parallel()

	registry := engine.NewRegistry()
	RegisterBuiltins(registry)
	known := registry.KnownTypes()
	byName := map[string]metadata.NodeType{}
	for _, node := range metadata.NodeTypes(known) {
		byName[node.Name] = node
	}

	for _, nodeType := range known {
		node, ok := byName[nodeType]
		if !ok {
			if nodeType == "n8n-nodes-base.sqlite" {
				continue
			}
			t.Errorf("%s has a Go executor but no original node metadata", nodeType)
			continue
		}
		if strings.HasPrefix(node.Icon, "file:") && node.IconURL == "" {
			t.Errorf("%s uses original file icon %q but has no exact iconUrl", nodeType, node.Icon)
		}
		if node.Icon == "fa:circle" {
			t.Errorf("%s uses generic fallback icon fa:circle", nodeType)
		}
		if node.Raw == nil {
			t.Errorf("%s has a Go executor but is not backed by original n8n metadata", nodeType)
		}
	}
}
