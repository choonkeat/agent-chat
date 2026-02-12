package main

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// promptTimeout is how long a tool_use must remain unresolved before
// we surface it as a permission prompt. This filters out tools that
// get approved automatically (fast tool_result follows).
const promptTimeout = 1500 * time.Millisecond

// pollInterval is how often the watcher checks for new lines.
const pollInterval = 200 * time.Millisecond

// Watcher tails a Claude Code session JSONL file and publishes
// permission prompt events when tool_use entries remain unresolved.
type Watcher struct {
	filePath string
	bus      *EventBus

	mu      sync.Mutex
	pending map[string]*pendingPrompt // tool_use_id -> pending prompt
	cancel  context.CancelFunc
}

type pendingPrompt struct {
	prompt PermissionPrompt
	timer  *time.Timer
}

// NewWatcher creates a watcher for the given JSONL file.
func NewWatcher(filePath string, bus *EventBus) *Watcher {
	return &Watcher{
		filePath: filePath,
		bus:      bus,
		pending:  make(map[string]*pendingPrompt),
	}
}

// Run starts tailing the JSONL file from the current end.
// It blocks until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	ctx, w.cancel = context.WithCancel(ctx)

	f, err := os.Open(w.filePath)
	if err != nil {
		log.Printf("watcher: failed to open %s: %v", w.filePath, err)
		return
	}
	defer f.Close()

	// Seek to end — we only care about new entries
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		log.Printf("watcher: failed to seek: %v", err)
		return
	}

	reader := bufio.NewReader(f)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.clearPending()
			return
		case <-ticker.C:
			w.readNewLines(reader)
		}
	}
}

// Stop cancels the watcher's context.
func (w *Watcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

// readNewLines reads all available complete lines from the reader.
func (w *Watcher) readNewLines(reader *bufio.Reader) {
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// No more complete lines available
			return
		}
		if len(line) <= 1 {
			continue
		}
		w.processLine(line)
	}
}

// processLine parses a JSONL line and updates pending state.
func (w *Watcher) processLine(line []byte) {
	prompts, resolvedIDs := ParseJSONLLine(line)

	w.mu.Lock()
	defer w.mu.Unlock()

	// Resolve any completed tool_use IDs
	for _, id := range resolvedIDs {
		if pp, ok := w.pending[id]; ok {
			pp.timer.Stop()
			delete(w.pending, id)
			// Publish resolution event
			w.bus.Publish(Event{
				Type:      "permissionResolved",
				ToolUseID: id,
			})
		}
	}

	// Register new pending prompts with timeout
	for _, p := range prompts {
		prompt := p // capture for closure
		timer := time.AfterFunc(promptTimeout, func() {
			w.mu.Lock()
			_, stillPending := w.pending[prompt.ToolUseID]
			w.mu.Unlock()

			if stillPending {
				w.bus.Publish(Event{
					Type:      "permissionPrompt",
					ToolUseID: prompt.ToolUseID,
					ToolName:  prompt.ToolName,
					Text:      prompt.Title,
					Detail:    prompt.Detail,
				})
			}
		})
		w.pending[prompt.ToolUseID] = &pendingPrompt{
			prompt: prompt,
			timer:  timer,
		}
	}
}

// clearPending stops all pending timers.
func (w *Watcher) clearPending() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, pp := range w.pending {
		pp.timer.Stop()
		delete(w.pending, id)
	}
}
