package nodes

import (
	"context"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

// GmailTrigger is a polling trigger. The Gmail polling itself is driven by the
// scheduler (see internal/api/gmail_poll.go); this node body only forwards the
// messages the poller injected as trigger items. A manual run yields nothing.
type GmailTrigger struct{}

func (GmailTrigger) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	if len(in.InputData) > 0 && len(in.InputData[0]) > 0 {
		return dataplane.MainOutput(in.InputData[0]), nil
	}
	return dataplane.EmptyOutput(), nil
}
