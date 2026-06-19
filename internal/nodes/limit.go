package nodes

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Limit struct{}

type limitParams struct {
	MaxItems int
	Keep     string
}

func (Limit) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	items := cloneItems(firstInput(in.InputData))
	params := newLimitParams(in.Node.Parameters)
	limited, err := limitItems(items, params)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(limited), nil
}

func newLimitParams(raw map[string]any) limitParams {
	return limitParams{
		MaxItems: intParamLimit(firstNonNil(raw["maxItems"], raw["limit"]), 1),
		Keep:     normalizeLimitKeep(firstNonEmptyNode(stringParam(raw, "keep"), "firstItems")),
	}
}

func limitItems(items []dataplane.Item, params limitParams) ([]dataplane.Item, error) {
	if params.MaxItems <= 0 || len(items) == 0 {
		return []dataplane.Item{}, nil
	}
	if len(items) <= params.MaxItems {
		return items, nil
	}
	switch params.Keep {
	case "firstItems":
		return items[:params.MaxItems], nil
	case "lastItems":
		return items[len(items)-params.MaxItems:], nil
	default:
		return nil, fmt.Errorf("limit: unsupported keep value %s", params.Keep)
	}
}

func normalizeLimitKeep(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "first", "firstitems":
		return "firstItems"
	case "last", "lastitems":
		return "lastItems"
	default:
		return value
	}
}

func intParamLimit(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return fallback
}
