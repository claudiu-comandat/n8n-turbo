package logstream

import (
	"sync"
	"time"
)

type EventType string

const (
	EventHTTPRequest     EventType = "n8n.http.request"
	EventWorkflowStarted EventType = "n8n.workflow.started"
	EventWorkflowSuccess EventType = "n8n.workflow.success"
	EventWorkflowFailed  EventType = "n8n.workflow.failed"
	EventNodeSuccess     EventType = "n8n.node.success"
	EventNodeFailed      EventType = "n8n.node.failed"
)

type Event struct {
	ID        string         `json:"eventName"`
	Timestamp time.Time      `json:"ts"`
	Payload   map[string]any `json:"payload"`
}

type Service struct {
	mu       sync.RWMutex
	events   []Event
	next     int
	count    int
	capacity int
}

func New(capacity int) *Service {
	if capacity <= 0 {
		capacity = 1000
	}
	return &Service{events: make([]Event, capacity), capacity: capacity}
}

func (s *Service) Emit(eventType EventType, payload map[string]any) Event {
	event := Event{ID: string(eventType), Timestamp: time.Now().UTC(), Payload: payload}
	s.mu.Lock()
	s.events[s.next] = event
	s.next = (s.next + 1) % s.capacity
	if s.count < s.capacity {
		s.count++
	}
	s.mu.Unlock()
	return event
}

func (s *Service) List(limit int) []Event {
	if limit <= 0 {
		limit = 100
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit > s.count {
		limit = s.count
	}
	result := make([]Event, 0, limit)
	for i := 0; i < limit; i++ {
		index := (s.next - 1 - i + s.capacity) % s.capacity
		result = append(result, s.events[index])
	}
	return result
}

func (s *Service) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}
