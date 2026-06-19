package nodes

import (
	"context"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type NoOp struct{}

func (NoOp) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return dataplane.MainOutput(firstInput(in.InputData)), nil
}
