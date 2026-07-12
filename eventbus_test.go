package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestHasHistory(t *testing.T) {
	bus := NewEventBus()
	if bus.HasHistory() {
		t.Fatal("fresh bus should have no history")
	}
	// A send_progress-style event (no quick replies) still counts as history,
	// so welcome replies are suppressed after the agent has engaged.
	bus.Publish(Event{Type: "agentMessage", Text: "Working on it..."})
	if !bus.HasHistory() {
		t.Fatal("bus with a logged event should report history")
	}
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

// --- pending-ack (limbo) + single-waiter tests ---
//
// Background: a blocking send_message can be orphaned by the harness (e.g.
// Claude Code's 30-min stdio idle abort sends NO notifications/cancelled), so
// its server-side wait lives on as a zombie that steals the next user reply
// and returns it on a dead request ID. Two defenses:
//  1. single-waiter: any new MCP call cancels the previous blocking wait
//     before it can consume anything (the harness serializes agent-chat
//     calls, so a new call proves the old one is dead client-side).
//  2. limbo: every batch delivered to the agent is retained un-acked; if the
//     delivery was lost in transit, the next check_messages redelivers it.

func TestWaitForMessagesStoresLimbo(t *testing.T) {
	bus := NewEventBus()
	bus.PushMessage("hello", nil)
	msgs, err := bus.WaitForMessages(context.Background())
	if err != nil {
		t.Fatalf("WaitForMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Text != "hello" {
		t.Fatalf("unexpected msgs: %+v", msgs)
	}
	limbo := bus.Limbo()
	if len(limbo) != 1 || limbo[0].Text != "hello" {
		t.Fatalf("delivered batch not retained in limbo: %+v", limbo)
	}
}

func TestDrainMessagesStoresLimboAndOverwrites(t *testing.T) {
	bus := NewEventBus()
	bus.PushMessage("first", nil)
	if msgs := bus.DrainMessages(); len(msgs) != 1 {
		t.Fatalf("drain: %+v", msgs)
	}
	if limbo := bus.Limbo(); len(limbo) != 1 || limbo[0].Text != "first" {
		t.Fatalf("limbo after first drain: %+v", limbo)
	}
	// A later delivery supersedes the previous batch (overwrite, not append).
	bus.PushMessage("second", nil)
	if msgs := bus.DrainMessages(); len(msgs) != 1 {
		t.Fatalf("second drain: %+v", msgs)
	}
	if limbo := bus.Limbo(); len(limbo) != 1 || limbo[0].Text != "second" {
		t.Fatalf("limbo after second drain: %+v", limbo)
	}
}

func TestEmptyDrainLeavesLimboUntouched(t *testing.T) {
	bus := NewEventBus()
	bus.PushMessage("keep me", nil)
	bus.DrainMessages()
	if msgs := bus.DrainMessages(); msgs != nil {
		t.Fatalf("expected empty drain, got %+v", msgs)
	}
	if limbo := bus.Limbo(); len(limbo) != 1 || limbo[0].Text != "keep me" {
		t.Fatalf("empty drain must not clear limbo: %+v", limbo)
	}
}

func TestAckLimboClears(t *testing.T) {
	bus := NewEventBus()
	bus.PushMessage("hello", nil)
	bus.DrainMessages()
	bus.AckLimbo()
	if limbo := bus.Limbo(); limbo != nil {
		t.Fatalf("AckLimbo did not clear: %+v", limbo)
	}
}

func TestSetLimboUnionForRedelivery(t *testing.T) {
	bus := NewEventBus()
	bus.SetLimbo([]UserMessage{{Text: "old"}, {Text: "new"}})
	limbo := bus.Limbo()
	if len(limbo) != 2 || limbo[0].Text != "old" || limbo[1].Text != "new" {
		t.Fatalf("SetLimbo roundtrip: %+v", limbo)
	}
}

func TestCancelActiveWaitAbortsBlockedWaiterWithoutConsuming(t *testing.T) {
	bus := NewEventBus()
	wctx, endWait := bus.BeginBlockingWait(context.Background())
	defer endWait()

	errCh := make(chan error, 1)
	go func() {
		_, err := bus.WaitForMessages(wctx)
		errCh <- err
	}()

	// Give the waiter a moment to block, then a new tool call arrives and
	// cancels it (zombie kill).
	time.Sleep(10 * time.Millisecond)
	bus.CancelActiveWait()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("cancelled wait returned nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("blocked waiter not cancelled by CancelActiveWait")
	}

	// A message arriving after the kill must remain drainable — the zombie
	// must not have consumed it.
	bus.PushMessage("survives", nil)
	msgs := bus.DrainMessages()
	if len(msgs) != 1 || msgs[0].Text != "survives" {
		t.Fatalf("message stolen or lost after zombie kill: %+v", msgs)
	}
}

func TestBeginBlockingWaitSupersedesPrevious(t *testing.T) {
	bus := NewEventBus()
	wctx1, end1 := bus.BeginBlockingWait(context.Background())
	defer end1()
	_, end2 := bus.BeginBlockingWait(context.Background())
	defer end2()

	select {
	case <-wctx1.Done():
		// first wait cancelled by the second — correct
	case <-time.After(2 * time.Second):
		t.Fatalf("second BeginBlockingWait did not cancel the first")
	}
}

func TestEndBlockingWaitClearsOnlyItself(t *testing.T) {
	bus := NewEventBus()
	_, end1 := bus.BeginBlockingWait(context.Background())
	end1() // wait #1 finishes normally

	wctx2, end2 := bus.BeginBlockingWait(context.Background())
	defer end2()
	end1() // stale cleanup from #1 must not cancel #2

	select {
	case <-wctx2.Done():
		t.Fatalf("stale end func cancelled the active wait")
	default:
	}
}
