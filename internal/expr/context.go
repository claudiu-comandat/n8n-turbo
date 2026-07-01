package expr

import (
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

type Context struct {
	Items         []dataplane.Item
	CurrentIndex  int
	RunData       dataplane.RunData
	Variables     map[string]any
	Secrets       map[string]map[string]string
	WorkflowID    string
	WorkflowName  string
	ExecutionID   string
	ExecutionMode string
	ResumeURL     string
	ResumeFormURL string
	ScheduledTime time.Time
	RunIndex      int
	Now           time.Time
	Extra         map[string]any
}

func (c Context) CurrentItem() dataplane.Item {
	if c.CurrentIndex >= 0 && c.CurrentIndex < len(c.Items) {
		return c.Items[c.CurrentIndex]
	}
	return dataplane.Item{JSON: map[string]any{}}
}
