package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEventBusWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	bus, err := NewEventBusWithLog(path)
	if err != nil {
		t.Fatalf("NewEventBusWithLog: %v", err)
	}

	// Publish an agentMessage
	bus.Publish(Event{Type: "agentMessage", Text: "hello from agent"})

	// Log a userMessage
	bus.LogUserMessage("hello from user", nil)

	// Publish a draw event
	bus.Publish(Event{
		Type:         "draw",
		Instructions: []any{map[string]any{"type": "drawRect", "x": 0, "y": 0}},
	})

	bus.Close()

	// Read and verify JSONL
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var events []Event
	for scanner.Scan() {
		var evt Event
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	if events[0].Type != "agentMessage" || events[0].Text != "hello from agent" {
		t.Errorf("event 0: got type=%q text=%q", events[0].Type, events[0].Text)
	}
	if events[1].Type != "userMessage" || events[1].Text != "hello from user" {
		t.Errorf("event 1: got type=%q text=%q", events[1].Type, events[1].Text)
	}
	if events[2].Type != "draw" || len(events[2].Instructions) == 0 {
		t.Errorf("event 2: got type=%q instructions=%v", events[2].Type, events[2].Instructions)
	}
}

func TestEventBusWithoutLog(t *testing.T) {
	bus := NewEventBus()
	// Should work without panicking
	bus.Publish(Event{Type: "agentMessage", Text: "test"})
	bus.LogUserMessage("test", nil)
	bus.Close() // no-op when no file
}
