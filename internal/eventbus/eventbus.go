package eventbus

import (
	"context"
	"sync"
)

type Event struct {
	Type string
	Data any
}

type Handler func(context.Context, Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

func New() *Bus {
	return &Bus{handlers: map[string][]Handler{}}
}

func (b *Bus) Subscribe(eventType string, handler Handler) {
	if b == nil || handler == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *Bus) Publish(ctx context.Context, event Event) {
	if b == nil {
		return
	}
	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers[event.Type]...)
	allHandlers := append([]Handler(nil), b.handlers["*"]...)
	b.mu.RUnlock()
	handlers = append(handlers, allHandlers...)
	for _, handler := range handlers {
		handler(ctx, event)
	}
}

func (b *Bus) Count(eventType string) int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.handlers[eventType])
}
