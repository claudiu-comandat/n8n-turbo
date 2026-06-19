package trigger

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type PollingTrigger struct {
	id         string
	workflowID string
	nodeID     string
	interval   time.Duration
	callback   TickCallback
	mu         sync.Mutex
	running    bool
	stop       context.CancelFunc
	done       chan struct{}
}

func NewPollingTrigger(id string, workflowID string, nodeID string, interval time.Duration, callback TickCallback) *PollingTrigger {
	return &PollingTrigger{id: id, workflowID: workflowID, nodeID: nodeID, interval: interval, callback: callback}
}

func (p *PollingTrigger) ID() string {
	return p.id
}

func (p *PollingTrigger) Type() Type {
	return TypePolling
}

func (p *PollingTrigger) WorkflowID() string {
	return p.workflowID
}

func (p *PollingTrigger) NodeID() string {
	return p.nodeID
}

func (p *PollingTrigger) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func (p *PollingTrigger) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	if p.interval <= 0 {
		return fmt.Errorf("polling interval must be positive")
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.stop = cancel
	p.done = make(chan struct{})
	p.running = true
	go p.loop(runCtx)
	return nil
}

func (p *PollingTrigger) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	stop := p.stop
	done := p.done
	p.mu.Unlock()
	if stop != nil {
		stop()
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	p.mu.Lock()
	p.running = false
	p.stop = nil
	p.done = nil
	p.mu.Unlock()
	return nil
}

func (p *PollingTrigger) loop(ctx context.Context) {
	defer close(p.done)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			if p.callback != nil {
				_ = p.callback(ctx, at)
			}
		}
	}
}
