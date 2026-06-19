package logstreaming

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker open")

type Destination interface {
	ID() string
	Send(ctx context.Context, event StreamEvent) error
	IsEnabled() bool
	ShouldReceive(eventType EventType) bool
	GetConfig() DestinationConfig
}

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

type CircuitBreaker struct {
	mu                 sync.RWMutex
	state              CircuitState
	failures           int
	maxFailures        int
	resetTimeout       time.Duration
	lastFailure        time.Time
	consecutiveSuccess int
	successThreshold   int
}

func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	if maxFailures <= 0 {
		maxFailures = 3
	}
	if resetTimeout <= 0 {
		resetTimeout = time.Minute
	}
	return &CircuitBreaker{maxFailures: maxFailures, resetTimeout: resetTimeout, successThreshold: 3}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == CircuitOpen {
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			return true
		}
		return false
	}
	return true
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveSuccess++
	if cb.state == CircuitHalfOpen && cb.consecutiveSuccess >= cb.successThreshold {
		cb.state = CircuitClosed
		cb.failures = 0
		cb.consecutiveSuccess = 0
	}
	if cb.state == CircuitClosed {
		cb.failures = 0
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.consecutiveSuccess = 0
	cb.lastFailure = time.Now().UTC()
	if cb.failures >= cb.maxFailures {
		cb.state = CircuitOpen
	}
}

func (cb *CircuitBreaker) State() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	switch cb.state {
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

type baseDestination struct {
	config DestinationConfig
}

func (d baseDestination) ID() string {
	return d.config.ID
}

func (d baseDestination) IsEnabled() bool {
	return d.config.Enabled
}

func (d baseDestination) ShouldReceive(eventType EventType) bool {
	if len(d.config.Events) == 0 {
		return true
	}
	for _, allowed := range d.config.Events {
		if allowed == eventType {
			return true
		}
	}
	return false
}

func (d baseDestination) GetConfig() DestinationConfig {
	return d.config
}

type WebhookDestination struct {
	baseDestination
	client  *http.Client
	breaker *CircuitBreaker
}

func NewWebhookDestination(cfg DestinationConfig) *WebhookDestination {
	return &WebhookDestination{baseDestination: baseDestination{config: cfg}, client: &http.Client{Timeout: 10 * time.Second}, breaker: NewCircuitBreaker(3, time.Minute)}
}

func (d *WebhookDestination) Send(ctx context.Context, event StreamEvent) error {
	if !d.breaker.Allow() {
		return ErrCircuitOpen
	}
	body, err := json.Marshal(event)
	if err != nil {
		d.breaker.RecordFailure()
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, d.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		d.breaker.RecordFailure()
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range d.config.WebhookHeaders {
		request.Header.Set(key, value)
	}
	response, err := d.client.Do(request)
	if err != nil {
		d.breaker.RecordFailure()
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		d.breaker.RecordFailure()
		return fmt.Errorf("webhook status %d", response.StatusCode)
	}
	d.breaker.RecordSuccess()
	return nil
}

type SyslogDestination struct {
	baseDestination
	conn    net.Conn
	breaker *CircuitBreaker
}

func NewSyslogDestination(cfg DestinationConfig) (*SyslogDestination, error) {
	protocol := cfg.SyslogProtocol
	if protocol == "" {
		protocol = "udp"
	}
	port := cfg.SyslogPort
	if port == 0 {
		port = 514
	}
	conn, err := net.Dial(protocol, fmt.Sprintf("%s:%d", cfg.SyslogHost, port))
	if err != nil {
		return nil, err
	}
	return &SyslogDestination{baseDestination: baseDestination{config: cfg}, conn: conn, breaker: NewCircuitBreaker(3, time.Minute)}, nil
}

func (d *SyslogDestination) Send(ctx context.Context, event StreamEvent) error {
	if !d.breaker.Allow() {
		return ErrCircuitOpen
	}
	body, _ := json.Marshal(event)
	message := "n8n " + event.ID + ": " + string(body)
	var err error
	if strings.Contains(event.ID, "failed") || strings.Contains(event.ID, "error") {
		_, err = d.conn.Write([]byte("<11>" + message + "\n"))
	} else if strings.Contains(event.ID, "started") {
		_, err = d.conn.Write([]byte("<14>" + message + "\n"))
	} else {
		_, err = d.conn.Write([]byte("<13>" + message + "\n"))
	}
	if err != nil {
		d.breaker.RecordFailure()
		return err
	}
	d.breaker.RecordSuccess()
	return nil
}

type SentryDestination struct {
	baseDestination
	client  *http.Client
	breaker *CircuitBreaker
}

func NewSentryDestination(cfg DestinationConfig) *SentryDestination {
	return &SentryDestination{baseDestination: baseDestination{config: cfg}, client: &http.Client{Timeout: 10 * time.Second}, breaker: NewCircuitBreaker(3, time.Minute)}
}

func (d *SentryDestination) Send(ctx context.Context, event StreamEvent) error {
	if !d.breaker.Allow() {
		return ErrCircuitOpen
	}
	endpoint, err := sentryStoreEndpoint(d.config.SentryDSN)
	if err != nil {
		d.breaker.RecordFailure()
		return err
	}
	payload := map[string]any{
		"event_id":  strings.ReplaceAll(event.ID+"-"+event.Timestamp.Format("20060102150405.000000000"), ".", "_"),
		"timestamp": event.Timestamp.Format(time.RFC3339Nano),
		"level":     sentryLevel(event.ID),
		"logger":    "n8n",
		"platform":  "go",
		"extra":     event.Payload,
	}
	if d.config.SentryEnvironment != "" {
		payload["environment"] = d.config.SentryEnvironment
	}
	body, _ := json.Marshal(payload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		d.breaker.RecordFailure()
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := d.client.Do(request)
	if err != nil {
		d.breaker.RecordFailure()
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		d.breaker.RecordFailure()
		return fmt.Errorf("sentry status %d", response.StatusCode)
	}
	d.breaker.RecordSuccess()
	return nil
}

func sentryStoreEndpoint(dsn string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	project := strings.Trim(parsed.Path, "/")
	if project == "" {
		return "", fmt.Errorf("missing sentry project id")
	}
	parsed.User = nil
	parsed.Path = "/api/" + project + "/store/"
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func sentryLevel(eventID string) string {
	if strings.Contains(eventID, "failed") || strings.Contains(eventID, "error") {
		return "error"
	}
	return "info"
}
