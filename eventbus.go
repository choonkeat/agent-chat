package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// FileRef describes an uploaded file.
type FileRef struct {
	Name string `json:"name"`           // original filename
	Path string `json:"path"`           // absolute server path
	URL  string `json:"url"`            // relative URL for browser to fetch thumbnail
	Size int64  `json:"size"`           // bytes
	Type string `json:"type,omitempty"` // MIME type
}

// UserMessage is a text message with optional file attachments from the browser.
type UserMessage struct {
	Text  string    `json:"text"`
	Files []FileRef `json:"files,omitempty"`
}

// Event represents a chat event sent to browser clients.
type Event struct {
	Type         string    `json:"type"`                   // "agentMessage", "userMessage", "draw"
	Seq          int64     `json:"seq"`                    // monotonic sequence number
	Text         string    `json:"text,omitempty"`
	AckID        string    `json:"ack_id,omitempty"`
	QuickReplies []string  `json:"quick_replies,omitempty"`
	Instructions []any     `json:"instructions,omitempty"` // draw instructions
	Files        []FileRef `json:"files,omitempty"`
	Timestamp    int64     `json:"ts,omitempty"` // Unix milliseconds
}

// AckHandle is returned by CreateAck. Read from Ch to wait for the user's ack.
type AckHandle struct {
	ID string
	Ch chan string // receives "ack" or "ack:<message>"
}

// EventBus fans out events to WebSocket subscribers, tracks pending acks,
// and maintains an in-memory event log for browser reconnect.
type EventBus struct {
	mu              sync.RWMutex
	subscribers     map[chan Event]struct{}
	eventLog        []Event  // session event log for reconnect replay
	nextSeq         int64    // next sequence number (guarded by mu)
	lastQuickReplies []string // last quick_replies sent to browser (nil = agent working)

	ackMu   sync.Mutex
	pending map[string]chan string // ack_id -> channel

	msgQueue  chan UserMessage // queued user messages from browser
	lastVoice bool            // whether the last consumed user message was voice

	logFile *os.File   // optional JSONL event log on disk
	logMu   sync.Mutex // guards logFile writes
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan Event]struct{}),
		pending:     make(map[string]chan string),
		msgQueue:    make(chan UserMessage, 256),
	}
}

// NewEventBusWithLog creates an EventBus that also appends events to a JSONL file.
// If the file already exists, its events are loaded into memory so browsers get
// full history across server restarts.
func NewEventBusWithLog(path string) (*EventBus, error) {
	// Load existing events from the log file.
	events, maxSeq, lastQR := loadEventLog(path)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &EventBus{
		subscribers:      make(map[chan Event]struct{}),
		pending:          make(map[string]chan string),
		msgQueue:         make(chan UserMessage, 256),
		logFile:          f,
		eventLog:         events,
		nextSeq:          maxSeq,
		lastQuickReplies: lastQR,
	}, nil
}

// loadEventLog reads a JSONL event log file and returns the parsed events,
// the highest sequence number found, and the reconstructed lastQuickReplies.
func loadEventLog(path string) ([]Event, int64, []string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, nil
	}
	defer f.Close()

	var events []Event
	var maxSeq int64
	var lastQR []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue // skip malformed lines
		}
		events = append(events, ev)
		if ev.Seq > maxSeq {
			maxSeq = ev.Seq
		}
		// Reconstruct lastQuickReplies state.
		if len(ev.QuickReplies) > 0 {
			lastQR = ev.QuickReplies
		}
		if ev.Type == "userMessage" {
			lastQR = nil
		}
	}
	return events, maxSeq, lastQR
}

// writeToLog marshals an event to JSON and appends it to the log file.
func (eb *EventBus) writeToLog(event Event) {
	eb.logMu.Lock()
	defer eb.logMu.Unlock()
	if eb.logFile == nil {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	data = append(data, '\n')
	eb.logFile.Write(data)
	eb.logFile.Sync()
}

// Close flushes and closes the log file.
func (eb *EventBus) Close() {
	eb.logMu.Lock()
	defer eb.logMu.Unlock()
	if eb.logFile != nil {
		eb.logFile.Sync()
		eb.logFile.Close()
		eb.logFile = nil
	}
}

// PushMessage queues a user message from the browser.
func (eb *EventBus) PushMessage(text string, files []FileRef) {
	msg := UserMessage{Text: text, Files: files}
	select {
	case eb.msgQueue <- msg:
	default:
		// queue full, drop oldest
		select {
		case <-eb.msgQueue:
		default:
		}
		eb.msgQueue <- msg
	}
}

// DrainMessages returns all currently queued messages, or nil if none are queued.
// Non-blocking. Text from multiple messages is joined with "\n\n"; files are aggregated.
func (eb *EventBus) DrainMessages() []UserMessage {
	var msgs []UserMessage
	for {
		select {
		case msg := <-eb.msgQueue:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// WaitForMessages waits for at least one queued message, drains any additional,
// and returns them.
func (eb *EventBus) WaitForMessages(ctx context.Context) ([]UserMessage, error) {
	var msgs []UserMessage
	select {
	case msg := <-eb.msgQueue:
		msgs = append(msgs, msg)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// drain any additional queued messages
	for {
		select {
		case msg := <-eb.msgQueue:
			msgs = append(msgs, msg)
		default:
			return msgs, nil
		}
	}
}

// SetLastVoice records whether the last consumed user messages contained voice input.
func (eb *EventBus) SetLastVoice(voice bool) {
	eb.mu.Lock()
	eb.lastVoice = voice
	eb.mu.Unlock()
}

// LastVoice returns true if the last consumed user messages contained voice input.
func (eb *EventBus) LastVoice() bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return eb.lastVoice
}

// LastQuickReplies returns the last quick_replies sent to the browser, or nil
// if the agent is currently working (no pending quick replies).
func (eb *EventBus) LastQuickReplies() []string {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return eb.lastQuickReplies
}

// HasQueuedMessages returns true if there are user messages waiting in the queue.
func (eb *EventBus) HasQueuedMessages() bool {
	return len(eb.msgQueue) > 0
}

// FormatMessages joins user messages into a single string with file attachment info.
func FormatMessages(msgs []UserMessage) string {
	data := formatMessagesData{}
	for _, m := range msgs {
		isVoice := strings.HasPrefix(m.Text, "\U0001f3a4 ")
		text := m.Text
		if isVoice {
			text = strings.TrimPrefix(text, "\U0001f3a4 ")
		}
		data.Messages = append(data.Messages, messageData{Text: text, IsVoice: isVoice})
		for _, f := range m.Files {
			mime := f.Type
			if mime == "" {
				mime = "application/octet-stream"
			}
			data.Files = append(data.Files, fileData{Path: f.Path, Type: mime, Size: formatSize(f.Size)})
		}
	}
	return execTemplate("format-messages", data)
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
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixMilli()
	}
	eb.mu.Lock()
	eb.nextSeq++
	event.Seq = eb.nextSeq
	eb.eventLog = append(eb.eventLog, event)

	// Track lastQuickReplies for new browser state.
	if len(event.QuickReplies) > 0 {
		eb.lastQuickReplies = event.QuickReplies
	}
	if event.Type == "userMessage" {
		eb.lastQuickReplies = nil
	}

	for ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
	eb.mu.Unlock()
	eb.writeToLog(event)
}

// LogUserMessage appends a user message event to the log for reconnect replay.
func (eb *EventBus) LogUserMessage(text string, files []FileRef) {
	evt := Event{Type: "userMessage", Text: text, Files: files, Timestamp: time.Now().UnixMilli()}
	eb.mu.Lock()
	eb.eventLog = append(eb.eventLog, evt)
	eb.mu.Unlock()
	eb.writeToLog(evt)
}

// EventsSince returns all events with Seq > cursor.
func (eb *EventBus) EventsSince(cursor int64) []Event {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	// Find the first event with Seq > cursor using the fact that seqs are monotonic.
	start := 0
	for i, e := range eb.eventLog {
		if e.Seq > cursor {
			start = i
			break
		}
		// If we reach the end without finding, start = len (returns empty).
		if i == len(eb.eventLog)-1 {
			start = len(eb.eventLog)
		}
	}
	if len(eb.eventLog) == 0 {
		return nil
	}
	result := make([]Event, len(eb.eventLog)-start)
	copy(result, eb.eventLog[start:])
	return result
}

// PendingAckID returns the first pending ack ID, if any.
func (eb *EventBus) PendingAckID() string {
	eb.ackMu.Lock()
	defer eb.ackMu.Unlock()
	for id := range eb.pending {
		return id
	}
	return ""
}

// History returns a copy of the event log and the pending ack ID (if any).
func (eb *EventBus) History() ([]Event, string) {
	eb.mu.RLock()
	log := make([]Event, len(eb.eventLog))
	copy(log, eb.eventLog)
	eb.mu.RUnlock()

	return log, eb.PendingAckID()
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
