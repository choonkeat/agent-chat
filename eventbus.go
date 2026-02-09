package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Event represents a chat event sent to browser clients.
type Event struct {
	Type         string   `json:"type"`                    // "agentMessage", "userMessage"
	Text         string   `json:"text,omitempty"`
	AckID        string   `json:"ack_id,omitempty"`
	QuickReplies []string `json:"quick_replies,omitempty"`
}

// AckHandle is returned by CreateAck. Read from Ch to wait for the user's ack.
type AckHandle struct {
	ID string
	Ch chan string // receives "ack" or "ack:<message>"
}

// EventBus fans out events to WebSocket subscribers, tracks pending acks,
// and maintains an in-memory event log for browser reconnect.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
	eventLog    []Event // session event log for reconnect replay

	ackMu   sync.Mutex
	pending map[string]chan string // ack_id -> channel
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan Event]struct{}),
		pending:     make(map[string]chan string),
	}
}

// Subscribe returns a buffered channel that receives all published events.
// Call Unsubscribe when done.
func (eb *EventBus) Subscribe() chan Event {
	ch := make(chan Event, 64)
	eb.mu.Lock()
	eb.subscribers[ch] = struct{}{}
	eb.mu.Unlock()
	return ch
}

// WaitForSubscriber polls until at least one subscriber is connected,
// or the context is cancelled, or 30 seconds elapse.
func (eb *EventBus) WaitForSubscriber(ctx context.Context) error {
	for {
		eb.mu.RLock()
		n := len(eb.subscribers)
		eb.mu.RUnlock()
		if n > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timed out waiting for browser to connect")
		case <-time.After(100 * time.Millisecond):
			// poll again
		}
	}
}

// Unsubscribe removes a subscriber channel.
func (eb *EventBus) Unsubscribe(ch chan Event) {
	eb.mu.Lock()
	delete(eb.subscribers, ch)
	eb.mu.Unlock()
}

// ResetLog clears the event log.
func (eb *EventBus) ResetLog() {
	eb.mu.Lock()
	eb.eventLog = nil
	eb.mu.Unlock()
}

// Publish sends an event to all subscribers and appends to the event log.
func (eb *EventBus) Publish(event Event) {
	eb.mu.Lock()
	eb.eventLog = append(eb.eventLog, event)
	for ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
	eb.mu.Unlock()
}

// LogUserMessage appends a user message event to the log for reconnect replay.
func (eb *EventBus) LogUserMessage(text string) {
	eb.mu.Lock()
	eb.eventLog = append(eb.eventLog, Event{Type: "userMessage", Text: text})
	eb.mu.Unlock()
}

// History returns a copy of the event log and the pending ack ID (if any).
func (eb *EventBus) History() ([]Event, string) {
	eb.mu.RLock()
	log := make([]Event, len(eb.eventLog))
	copy(log, eb.eventLog)
	eb.mu.RUnlock()

	eb.ackMu.Lock()
	var pendingID string
	for id := range eb.pending {
		pendingID = id
		break
	}
	eb.ackMu.Unlock()

	return log, pendingID
}

// CreateAck creates a pending acknowledgment. The caller waits on Ch until
// the user responds or the context is cancelled.
func (eb *EventBus) CreateAck() AckHandle {
	id := uuid.New().String()
	ch := make(chan string, 1)

	eb.ackMu.Lock()
	eb.pending[id] = ch
	eb.ackMu.Unlock()

	return AckHandle{ID: id, Ch: ch}
}

// ResolveAck resolves a pending ack. The result string is sent through the
// channel (e.g. "ack" or "ack:message"). Returns true if the ack existed.
func (eb *EventBus) ResolveAck(id, result string) bool {
	eb.ackMu.Lock()
	ch, ok := eb.pending[id]
	if ok {
		delete(eb.pending, id)
	}
	eb.ackMu.Unlock()

	if !ok {
		return false
	}
	select {
	case ch <- result:
	default:
	}
	return true
}
