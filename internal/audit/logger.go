package audit

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Logger interface {
	Log(ctx context.Context, event Event) error
	GetEvents(ctx context.Context, filter Filter) ([]Event, int, error)
}

type CompositeLogger struct {
	loggers []Logger
}

func NewCompositeLogger(loggers ...Logger) *CompositeLogger {
	return &CompositeLogger{loggers: loggers}
}

func (c *CompositeLogger) Log(ctx context.Context, event Event) error {
	failures := []error{}
	for _, logger := range c.loggers {
		if logger == nil {
			continue
		}
		if err := logger.Log(ctx, event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (c *CompositeLogger) GetEvents(ctx context.Context, filter Filter) ([]Event, int, error) {
	for _, logger := range c.loggers {
		if logger == nil {
			continue
		}
		events, total, err := logger.GetEvents(ctx, filter)
		if err == nil && total > 0 {
			return events, total, nil
		}
	}
	return []Event{}, 0, nil
}

type RingBufferLogger struct {
	mu       sync.RWMutex
	events   []Event
	capacity int
	head     int
	count    int
}

func NewRingBufferLogger(capacity int) *RingBufferLogger {
	if capacity <= 0 {
		capacity = 1000
	}
	return &RingBufferLogger{events: make([]Event, capacity), capacity: capacity}
}

func (r *RingBufferLogger) Log(ctx context.Context, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[r.head] = event
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
	return nil
}

func (r *RingBufferLogger) GetEvents(ctx context.Context, filter Filter) ([]Event, int, error) {
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	default:
	}
	r.mu.RLock()
	all := make([]Event, 0, r.count)
	for i := 0; i < r.count; i++ {
		index := (r.head - r.count + i + r.capacity) % r.capacity
		event := r.events[index]
		if matchesFilter(event, filter) {
			all = append(all, event)
		}
	}
	r.mu.RUnlock()
	sort.Slice(all, func(i int, j int) bool {
		return all[i].Timestamp.After(all[j].Timestamp)
	})
	total := len(all)
	limit := filter.Limit
	if limit <= 0 || limit > 250 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []Event{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func matchesFilter(event Event, filter Filter) bool {
	if filter.StartDate != nil && event.Timestamp.Before(*filter.StartDate) {
		return false
	}
	if filter.EndDate != nil && event.Timestamp.After(*filter.EndDate) {
		return false
	}
	if len(filter.EventTypes) > 0 {
		found := false
		for _, eventType := range filter.EventTypes {
			if event.EventType == eventType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if filter.UserID != "" && event.UserID != filter.UserID {
		return false
	}
	if filter.ResourceType != "" && event.ResourceType != filter.ResourceType {
		return false
	}
	if filter.ResourceID != "" && event.ResourceID != filter.ResourceID {
		return false
	}
	return true
}
