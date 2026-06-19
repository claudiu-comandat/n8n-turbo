package nodes

import (
	"context"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type ManualTrigger struct{}

func (ManualTrigger) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if len(in.InputData) > 0 && len(in.InputData[0]) > 0 {
		return dataplane.MainOutput(in.InputData[0]), nil
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{}}}), nil
}
