package api

import (
	"context"
	"time"
)

func (s *Server) executionContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = s.runtimeCtx
	}
	if parent == nil {
		parent = context.Background()
	}
	return s.executionTimeoutContext(parent, func() {})
}

func (s *Server) executionTimeoutContext(parent context.Context, parentCancel context.CancelFunc) (context.Context, context.CancelFunc) {
	seconds := s.config.Execution.TimeoutSeconds
	if seconds <= 0 {
		return parent, parentCancel
	}
	if max := s.config.Execution.MaxTimeoutSeconds; max > 0 && seconds > max {
		seconds = max
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(seconds)*time.Second)
	return ctx, func() {
		cancel()
		parentCancel()
	}
}
