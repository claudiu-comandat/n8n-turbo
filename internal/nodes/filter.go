package nodes

import (
	"context"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Filter struct{}

func (Filter) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	items := firstInput(in.InputData)
	result := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		if conditionMatches(in, items, index, item, in.Node.Parameters) {
			result = append(result, item)
		}
	}
	return dataplane.MainOutput(result), nil
}
