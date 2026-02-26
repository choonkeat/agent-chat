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

func TestEventBusReloadsLogOnStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	// First session: publish some events.
	bus1, err := NewEventBusWithLog(path)
	if err != nil {
		t.Fatalf("NewEventBusWithLog (session 1): %v", err)
	}
	bus1.Publish(Event{Type: "agentMessage", Text: "hello"})
	bus1.LogUserMessage("world", nil)
	bus1.Close()

	// Second session: open the same log file — events should be loaded.
	bus2, err := NewEventBusWithLog(path)
	if err != nil {
		t.Fatalf("NewEventBusWithLog (session 2): %v", err)
	}
	defer bus2.Close()

	events := bus2.EventsSince(0)
	if len(events) != 2 {
		t.Fatalf("expected 2 reloaded events, got %d", len(events))
	}
	if events[0].Text != "hello" || events[1].Text != "world" {
		t.Errorf("unexpected texts: %q, %q", events[0].Text, events[1].Text)
	}

	// New events should get sequence numbers after the reloaded ones.
	bus2.Publish(Event{Type: "agentMessage", Text: "new"})
	all := bus2.EventsSince(0)
	if len(all) != 3 {
		t.Fatalf("expected 3 total events, got %d", len(all))
	}
	if all[2].Seq <= all[1].Seq {
		t.Errorf("new event seq %d should be > reloaded seq %d", all[2].Seq, all[1].Seq)
	}
}

func TestEventBusWithoutLog(t *testing.T) {
	bus := NewEventBus()
	// Should work without panicking
	bus.Publish(Event{Type: "agentMessage", Text: "test"})
	bus.LogUserMessage("test", nil)
	bus.Close() // no-op when no file
}

func TestEventsSince(t *testing.T) {
	bus := NewEventBus()
	bus.Publish(Event{Type: "agentMessage", Text: "one"})
	bus.Publish(Event{Type: "userMessage", Text: "two"})
	bus.Publish(Event{Type: "agentMessage", Text: "three"})

	// All events (cursor=0)
	all := bus.EventsSince(0)
	if len(all) != 3 {
		t.Fatalf("EventsSince(0): expected 3, got %d", len(all))
	}
	if all[0].Seq != 1 || all[1].Seq != 2 || all[2].Seq != 3 {
		t.Errorf("unexpected seq numbers: %d, %d, %d", all[0].Seq, all[1].Seq, all[2].Seq)
	}

	// Events after seq 1
	after1 := bus.EventsSince(1)
	if len(after1) != 2 {
		t.Fatalf("EventsSince(1): expected 2, got %d", len(after1))
	}
	if after1[0].Text != "two" || after1[1].Text != "three" {
		t.Errorf("unexpected texts: %q, %q", after1[0].Text, after1[1].Text)
	}

	// Events after the latest seq
	none := bus.EventsSince(3)
	if len(none) != 0 {
		t.Fatalf("EventsSince(3): expected 0, got %d", len(none))
	}
}
