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
// ID is assigned when the message enters the system (via ReceiveUserMessage) and
// is echoed back on the matching userMessagesConsumed event so the browser can
// flip the bubble's "pending" state once the agent has actually drained it.
type UserMessage struct {
	ID    string    `json:"id,omitempty"`
	Text  string    `json:"text"`
	Files []FileRef `json:"files,omitempty"`
}

// Event represents a chat event sent to browser clients.
//
// For userMessage events, ID is the message's unique ID (so the browser can
// tag the bubble). For userMessagesConsumed events, IDs lists the message IDs
// the agent has just drained from the queue (or that the server consumed
// inline via the permission/ack paths).
type Event struct {
	Type         string    `json:"type"`                   // "agentMessage", "userMessage", "userMessagesConsumed", "draw"
	Seq          int64     `json:"seq"`                    // monotonic sequence number
	ID           string    `json:"id,omitempty"`           // userMessage: the message's unique ID
	IDs          []string  `json:"ids,omitempty"`          // userMessagesConsumed: which IDs were consumed
	Text         string    `json:"text,omitempty"`
	AckID        string    `json:"ack_id,omitempty"`
	QuickReplies []string  `json:"quick_replies,omitempty"`
	Instructions []any     `json:"instructions,omitempty"` // draw instructions
	Files        []FileRef `json:"files,omitempty"`
	Timestamp    int64     `json:"ts,omitempty"` // Unix milliseconds

	// AgentToolSeq + AgentToolName stamp events with the per-tool ordinal of
	// the MCP call that produced them, so consumers (e.g. swe-swe-server's
	// /api/fork resolver) can locate the matching tool_use/function_call in
	// the agent's own .jsonl without resorting to text correlation.
	//
	// For "agentMessage" events: the Nth call to AgentToolName that emitted
	// this bubble (send_message, send_progress, send_verbal_reply, etc.).
	// For "userMessagesConsumed" events: the Nth check_messages call that
	// drained the listed IDs.
	// Zero / empty means "unstamped" -- legacy events or server-side ack
	// paths that didn't originate from an MCP tool call.
	AgentToolSeq  int64  `json:"agent_tool_seq,omitempty"`
	AgentToolName string `json:"agent_tool_name,omitempty"`
}

// AckHandle is returned by CreateAck. Read from Ch to wait for the user's ack.
type AckHandle struct {
	ID string
	Ch chan string // receives "ack" or "ack:<message>"
}

// ExportResult carries the bytes a browser POSTed back for an export request,
// or an error string if the browser reported failure.
type ExportResult struct {
	HTML  []byte
	Error string
}

// ExportHandle is returned by CreateExport. Read from Ch to wait for the
// browser to POST the rendered HTML back to /api/export.
type ExportHandle struct {
	Token string
	Ch    chan ExportResult
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

	exportMu        sync.Mutex
	pendingExports  map[string]chan ExportResult // export token -> channel

	transientMu   sync.RWMutex
	transientSubs map[chan any]struct{} // per-connection writeCh sinks for non-logged broadcasts

	msgQueue  chan UserMessage // queued user messages from browser
	lastVoice bool            // whether the last consumed user message was voice

	logFile *os.File   // optional JSONL event log on disk
	logMu   sync.Mutex // guards logFile writes
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers:    make(map[chan Event]struct{}),
		pending:        make(map[string]chan string),
		pendingExports: make(map[string]chan ExportResult),
		transientSubs:  make(map[chan any]struct{}),
		msgQueue:       make(chan UserMessage, 256),
	}
}

// NewEventBusWithLog creates an EventBus that also appends events to a JSONL file.
// If the file already exists, its events are loaded into memory so browsers get
// full history across server restarts.
func NewEventBusWithLog(path string) (*EventBus, error) {
	// Load existing events from the log file.
	events, maxSeq, lastQR := loadEventLog(path)

	// Resume MCP tool-call counters from whatever the on-disk events already
	// stamped so post-restart events keep counting from where they left off.
	// Without this, the first post-restart send_message would re-stamp 1 and
	// collide with the existing #1 in the agent's .jsonl.
	SeedToolCounters(events)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &EventBus{
		subscribers:      make(map[chan Event]struct{}),
		pending:          make(map[string]chan string),
		pendingExports:   make(map[string]chan ExportResult),
		transientSubs:    make(map[chan any]struct{}),
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

// PushMessage queues a user message from the browser. The ID will be assigned
// automatically; callers that need to broadcast the userMessage event with the
// matching ID should use ReceiveUserMessage instead.
func (eb *EventBus) PushMessage(text string, files []FileRef) {
	eb.pushUserMessage(UserMessage{ID: uuid.New().String(), Text: text, Files: files})
}

// pushUserMessage enqueues a pre-built UserMessage (used by ReceiveUserMessage,
// which generates the ID up front so the broadcast and the queue carry the
// same ID).
func (eb *EventBus) pushUserMessage(msg UserMessage) {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
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

// ReceiveUserMessage is the canonical entry point for a user-originated
// message: it publishes the userMessage event first (so every browser sees the
// bubble before any consumption signal) and then queues the message for the
// agent. The returned ID is the same one carried by the userMessage event and
// the eventual userMessagesConsumed event.
func (eb *EventBus) ReceiveUserMessage(text string, files []FileRef) string {
	id := uuid.New().String()
	eb.Publish(Event{Type: "userMessage", ID: id, Text: text, Files: files})
	eb.pushUserMessage(UserMessage{ID: id, Text: text, Files: files})
	return id
}

// PublishConsumedUserMessage is for paths where the server itself consumes a
// message without ever putting it in the agent queue (the permission-prompt
// interceptor and the ack-reply path). It broadcasts the userMessage event,
// then immediately broadcasts userMessagesConsumed for the same ID so the
// browser never shows a stuck "pending" bubble.
func (eb *EventBus) PublishConsumedUserMessage(text string, files []FileRef) string {
	id := uuid.New().String()
	eb.Publish(Event{Type: "userMessage", ID: id, Text: text, Files: files})
	eb.Publish(Event{Type: "userMessagesConsumed", IDs: []string{id}})
	return id
}

// publishConsumed fans out a userMessagesConsumed event for the given message
// IDs. No-op when the slice is empty. toolName + toolSeq stamp the event with
// the MCP call that caused the drain (so /api/fork can map a userMessage
// bubble back to the agent-side tool_use). Pass empty/zero for unstamped
// (server-acked) drains.
func (eb *EventBus) publishConsumed(msgs []UserMessage, toolName string, toolSeq int64) {
	if len(msgs) == 0 {
		return
	}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	eb.Publish(Event{
		Type:          "userMessagesConsumed",
		IDs:           ids,
		AgentToolName: toolName,
		AgentToolSeq:  toolSeq,
	})
}

// DrainMessages returns all currently queued messages, or nil if none are queued.
// Unstamped variant for callers that don't have an MCP tool ordinal to attach.
func (eb *EventBus) DrainMessages() []UserMessage {
	return eb.DrainMessagesStamped("", 0)
}

// DrainMessagesStamped is DrainMessages plus a tool-name/ordinal stamp on the
// resulting userMessagesConsumed event.
func (eb *EventBus) DrainMessagesStamped(toolName string, toolSeq int64) []UserMessage {
	var msgs []UserMessage
	for {
		select {
		case msg := <-eb.msgQueue:
			msgs = append(msgs, msg)
		default:
			eb.publishConsumed(msgs, toolName, toolSeq)
			return msgs
		}
	}
}

// WaitForMessages waits for at least one queued message, drains any additional,
// and returns them.
func (eb *EventBus) WaitForMessages(ctx context.Context) ([]UserMessage, error) {
	return eb.WaitForMessagesStamped(ctx, "", 0)
}

// WaitForMessagesStamped is WaitForMessages plus a tool-name/ordinal stamp on
// the resulting userMessagesConsumed event.
func (eb *EventBus) WaitForMessagesStamped(ctx context.Context, toolName string, toolSeq int64) ([]UserMessage, error) {
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
			eb.publishConsumed(msgs, toolName, toolSeq)
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

// RemoveFromQueue atomically pulls every queued message, drops the one with
// the matching ID, and re-enqueues the rest in their original order. Returns
// true if the target ID was found and removed. Used by the "unsend" flow so
// the agent never sees a withdrawn message. Distinct from drain: nothing is
// "consumed" here — withdrawn messages should fire userMessageDeleted, not
// userMessagesConsumed.
func (eb *EventBus) RemoveFromQueue(targetID string) bool {
	if targetID == "" {
		return false
	}
	var keep []UserMessage
	found := false
	for {
		select {
		case msg := <-eb.msgQueue:
			if msg.ID == targetID {
				found = true
				continue
			}
			keep = append(keep, msg)
		default:
			for _, m := range keep {
				eb.msgQueue <- m
			}
			return found
		}
	}
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

// SubscribeTransient registers a per-connection sink for transient (non-logged,
// non-replayed) broadcasts. The caller is responsible for unsubscribing on
// disconnect.
func (eb *EventBus) SubscribeTransient(ch chan any) {
	eb.transientMu.Lock()
	eb.transientSubs[ch] = struct{}{}
	eb.transientMu.Unlock()
}

// UnsubscribeTransient removes a transient sink.
func (eb *EventBus) UnsubscribeTransient(ch chan any) {
	eb.transientMu.Lock()
	delete(eb.transientSubs, ch)
	eb.transientMu.Unlock()
}

// PublishTransient fans out a payload to every transient subscriber. Skipped
// silently if a subscriber's buffer is full — transient messages are a "best
// effort" channel by design.
func (eb *EventBus) PublishTransient(payload any) int {
	eb.transientMu.RLock()
	defer eb.transientMu.RUnlock()
	delivered := 0
	for ch := range eb.transientSubs {
		select {
		case ch <- payload:
			delivered++
		default:
		}
	}
	return delivered
}

// TransientSubscriberCount returns the number of currently connected transient
// sinks (≈ number of browser tabs).
func (eb *EventBus) TransientSubscriberCount() int {
	eb.transientMu.RLock()
	defer eb.transientMu.RUnlock()
	return len(eb.transientSubs)
}

// CreateExport registers a pending export request and returns a handle whose
// Ch will receive the rendered HTML once a browser POSTs to /api/export.
func (eb *EventBus) CreateExport() ExportHandle {
	token := uuid.New().String()
	ch := make(chan ExportResult, 1)

	eb.exportMu.Lock()
	eb.pendingExports[token] = ch
	eb.exportMu.Unlock()

	return ExportHandle{Token: token, Ch: ch}
}

// ResolveExport completes a pending export with the given result. Returns true
// if the token matched a pending export.
func (eb *EventBus) ResolveExport(token string, result ExportResult) bool {
	eb.exportMu.Lock()
	ch, ok := eb.pendingExports[token]
	if ok {
		delete(eb.pendingExports, token)
	}
	eb.exportMu.Unlock()

	if !ok {
		return false
	}
	select {
	case ch <- result:
	default:
	}
	return true
}

// CancelExport removes a pending export without delivering a result. Use this
// in defer to clean up after timeout/error paths in the caller.
func (eb *EventBus) CancelExport(token string) {
	eb.exportMu.Lock()
	delete(eb.pendingExports, token)
	eb.exportMu.Unlock()
}
