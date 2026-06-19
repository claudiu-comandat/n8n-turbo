package logstreaming

import (
	"context"
	"sync"
	"time"
)

type BufferedEvent struct {
	Event    StreamEvent
	Received time.Time
}

type ServiceConfig struct {
	BufferSize  int
	Workers     int
	SendTimeout time.Duration
}

type Service struct {
	mu           sync.RWMutex
	destinations []Destination
	buffer       chan BufferedEvent
	workers      int
	sendTimeout  time.Duration
	once         sync.Once
}

func NewService(cfg ServiceConfig) *Service {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 1000
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 5
	}
	if cfg.SendTimeout <= 0 {
		cfg.SendTimeout = 10 * time.Second
	}
	return &Service{buffer: make(chan BufferedEvent, cfg.BufferSize), workers: cfg.Workers, sendTimeout: cfg.SendTimeout}
}

func (s *Service) AddDestination(destination Destination) {
	if destination == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, existing := range s.destinations {
		if existing.ID() == destination.ID() {
			s.destinations[index] = destination
			return
		}
	}
	s.destinations = append(s.destinations, destination)
}

func (s *Service) RemoveDestination(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.destinations[:0]
	for _, destination := range s.destinations {
		if destination.ID() != id {
			filtered = append(filtered, destination)
		}
	}
	s.destinations = filtered
}

func (s *Service) Destinations() []DestinationConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]DestinationConfig, 0, len(s.destinations))
	for _, destination := range s.destinations {
		result = append(result, destination.GetConfig())
	}
	return result
}

func (s *Service) Emit(eventType EventType, payload map[string]any) bool {
	event := StreamEvent{ID: string(eventType), Timestamp: time.Now().UTC(), Payload: payload}
	select {
	case s.buffer <- BufferedEvent{Event: event, Received: time.Now().UTC()}:
		return true
	default:
		return false
	}
}

func (s *Service) Start(ctx context.Context) {
	s.once.Do(func() {
		for i := 0; i < s.workers; i++ {
			go s.worker(ctx)
		}
	})
}

func (s *Service) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case buffered := <-s.buffer:
			s.deliver(ctx, buffered.Event)
		}
	}
}

func (s *Service) deliver(ctx context.Context, event StreamEvent) {
	eventType := EventType(event.ID)
	destinations := s.matchingDestinations(eventType)
	var wg sync.WaitGroup
	for _, destination := range destinations {
		destination := destination
		wg.Add(1)
		go func() {
			defer wg.Done()
			sendCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
			defer cancel()
			_ = destination.Send(sendCtx, event)
		}()
	}
	wg.Wait()
}

func (s *Service) matchingDestinations(eventType EventType) []Destination {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Destination, 0, len(s.destinations))
	for _, destination := range s.destinations {
		if destination.IsEnabled() && destination.ShouldReceive(eventType) {
			result = append(result, destination)
		}
	}
	return result
}

func (s *Service) BufferLen() int {
	return len(s.buffer)
}
