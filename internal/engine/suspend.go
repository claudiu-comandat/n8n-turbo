package engine

import (
	"errors"
	"fmt"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

type SuspendError struct {
	ExecutionID string
	NodeName    string
	ResumeAt    time.Time
	Reason      string
	Output      dataplane.Output
}

func (e *SuspendError) Error() string {
	return fmt.Sprintf("execution %s suspended at %s: %s", e.ExecutionID, e.NodeName, e.Reason)
}

func AsSuspendError(err error) (*SuspendError, bool) {
	var suspend *SuspendError
	if errors.As(err, &suspend) {
		return suspend, true
	}
	return nil, false
}
