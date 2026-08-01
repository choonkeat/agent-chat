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

func TestPendingUserMessagesFiltersConsumedAndDeleted(t *testing.T) {
	events := []Event{
		{Type: "userMessage", ID: "a", Text: "consumed"},
		{Type: "userMessagesConsumed", IDs: []string{"a"}},
		{Type: "userMessage", ID: "b", Text: "deleted"},
		{Type: "userMessageDeleted", ID: "b"},
		{Type: "userMessage", ID: "c", Text: "still-pending-1"},
		{Type: "userMessage", ID: "d", Text: "still-pending-2"},
		{Type: "userMessage", Text: "no-id-legacy"}, // LogUserMessage path — never pending
		{Type: "agentMessage", Text: "unrelated"},
	}
	got := pendingUserMessages(events)
	if len(got) != 2 {
		t.Fatalf("expected 2 pending messages, got %d: %+v", len(got), got)
	}
	// Order must match the log so the rehydrated queue lines up with the
	// browser's replayed pending bubbles.
	if got[0].ID != "c" || got[1].ID != "d" {
		t.Errorf("unexpected pending order: %q, %q", got[0].ID, got[1].ID)
	}
}

// A message left pending when the server stops must be re-queued on restart, so
// the agent can still drain it and an unsend (Delete) finds it in the queue.
// Without rehydration it becomes an un-deletable "ghost" bubble.
func TestEventBusRehydratesPendingQueueOnRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	bus1, err := NewEventBusWithLog(path)
	if err != nil {
		t.Fatalf("NewEventBusWithLog (session 1): %v", err)
	}
	// One message consumed, one withdrawn, one left pending in the queue.
	bus1.ReceiveUserMessage("consumed-me", nil, "")
	bus1.DrainMessages() // publishes userMessagesConsumed for the above
	delID := bus1.ReceiveUserMessage("delete-me", nil, "")
	if !bus1.RemoveFromQueue(delID) {
		t.Fatalf("expected delete-me to be in the queue")
	}
	bus1.Publish(Event{Type: "userMessageDeleted", ID: delID})
	pendingID := bus1.ReceiveUserMessage("still-pending", nil, "")
	bus1.Close() // queue is in-memory — the pending message is only in the log now

	// Restart on the same log file.
	bus2, err := NewEventBusWithLog(path)
	if err != nil {
		t.Fatalf("NewEventBusWithLog (session 2): %v", err)
	}
	defer bus2.Close()

	if !bus2.HasQueuedMessages() {
		t.Fatal("expected the pending message to be rehydrated into the queue")
	}
	// Delete must now succeed against the rehydrated queue (the fix: unsend finds
	// it and can publish userMessageDeleted instead of silently failing).
	if !bus2.RemoveFromQueue(pendingID) {
		t.Fatalf("expected rehydrated pending message %q to be removable", pendingID)
	}
	if bus2.HasQueuedMessages() {
		t.Error("queue should be empty after removing the only rehydrated message")
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

// --- three-state read receipts (queued / handed over / read) ---
//
// Background: userMessagesConsumed fires the instant a waiter takes messages
// off the queue, but a waiter can be a zombie (the user broke out of the tool
// call client-side and the harness sent no notifications/cancelled). Rendering
// that hand-over as "read" is a lie. ProveDelivery is the promotion: the
// agent's NEXT chat-tool call is the proof that the previous hand-over really
// reached it, and only then does userMessagesRead fire.

// eventTypesOf returns the types of every logged event, in order.
func eventTypesOf(bus *EventBus) []string {
	var types []string
	for _, e := range bus.EventsSince(0) {
		types = append(types, e.Type)
	}
	return types
}

// readEventIDs returns the IDs carried by every userMessagesRead event logged.
func readEventIDs(bus *EventBus) [][]string {
	var out [][]string
	for _, e := range bus.EventsSince(0) {
		if e.Type == "userMessagesRead" {
			out = append(out, e.IDs)
		}
	}
	return out
}

func hasType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// A drain hands the message over but proves nothing: consumed fires, read does not.
func TestDrainPublishesConsumedWithoutRead(t *testing.T) {
	bus := NewEventBus()
	bus.ReceiveUserMessage("hello", nil, "")
	bus.DrainMessages()

	types := eventTypesOf(bus)
	if !hasType(types, "userMessagesConsumed") {
		t.Fatalf("expected userMessagesConsumed, got %v", types)
	}
	if hasType(types, "userMessagesRead") {
		t.Fatalf("a bare drain must not publish userMessagesRead: %v", types)
	}
}

// The agent's next chat-tool call promotes the handed-over batch to "read",
// carrying exactly the IDs that were handed over. A second call is a no-op —
// there is nothing left unproven.
func TestProveDeliveryPromotesHandedOverIDsOnce(t *testing.T) {
	bus := NewEventBus()
	id := bus.ReceiveUserMessage("hello", nil, "")
	bus.DrainMessages()

	bus.ProveDelivery()
	reads := readEventIDs(bus)
	if len(reads) != 1 {
		t.Fatalf("expected exactly 1 userMessagesRead event, got %d: %v", len(reads), reads)
	}
	if len(reads[0]) != 1 || reads[0][0] != id {
		t.Fatalf("userMessagesRead IDs: got %v, want [%s]", reads[0], id)
	}

	bus.ProveDelivery()
	if reads := readEventIDs(bus); len(reads) != 1 {
		t.Fatalf("second ProveDelivery must be a no-op, got %d read events", len(reads))
	}
}

// Nothing handed over — nothing to prove.
func TestProveDeliveryWithoutHandoverIsNoOp(t *testing.T) {
	bus := NewEventBus()
	bus.ProveDelivery()
	if types := eventTypesOf(bus); len(types) != 0 {
		t.Fatalf("ProveDelivery on an idle bus published %v", types)
	}
}

// The blocking-wait path (send_message) registers its batch as unproven too.
func TestWaitForMessagesRegistersUnproven(t *testing.T) {
	bus := NewEventBus()
	id := bus.ReceiveUserMessage("hello", nil, "")
	if _, err := bus.WaitForMessages(context.Background()); err != nil {
		t.Fatalf("WaitForMessages: %v", err)
	}
	if hasType(eventTypesOf(bus), "userMessagesRead") {
		t.Fatal("the wait itself must not publish userMessagesRead")
	}
	bus.ProveDelivery()
	reads := readEventIDs(bus)
	if len(reads) != 1 || len(reads[0]) != 1 || reads[0][0] != id {
		t.Fatalf("ProveDelivery after a wait: got %v, want [[%s]]", reads, id)
	}
}

// A later hand-over supersedes an unproven earlier one only by accumulating:
// both batches are still awaiting proof, so one ProveDelivery covers both.
func TestProveDeliveryCoversEveryUnprovenBatch(t *testing.T) {
	bus := NewEventBus()
	first := bus.ReceiveUserMessage("first", nil, "")
	bus.DrainMessages()
	second := bus.ReceiveUserMessage("second", nil, "")
	bus.DrainMessages()

	bus.ProveDelivery()
	reads := readEventIDs(bus)
	if len(reads) != 1 {
		t.Fatalf("expected 1 read event, got %v", reads)
	}
	if len(reads[0]) != 2 || reads[0][0] != first || reads[0][1] != second {
		t.Fatalf("read IDs: got %v, want [%s %s]", reads[0], first, second)
	}
}

// The server-side consume paths (permission prompt, ack reply) are genuine
// receipts — the server itself read the message — so they must emit both
// events and never strand a bubble at "handed over".
func TestPublishConsumedUserMessageEmitsConsumedThenRead(t *testing.T) {
	bus := NewEventBus()
	id := bus.PublishConsumedUserMessage("acked inline", nil)

	types := eventTypesOf(bus)
	want := []string{"userMessage", "userMessagesConsumed", "userMessagesRead"}
	if len(types) != len(want) {
		t.Fatalf("event types: got %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("event types: got %v, want %v", types, want)
		}
	}
	reads := readEventIDs(bus)
	if len(reads) != 1 || len(reads[0]) != 1 || reads[0][0] != id {
		t.Fatalf("read IDs: got %v, want [[%s]]", reads, id)
	}
	// Nothing is left unproven, so the agent's next tool call adds nothing.
	bus.ProveDelivery()
	if reads := readEventIDs(bus); len(reads) != 1 {
		t.Fatalf("ProveDelivery re-published a receipt: %v", reads)
	}
}

// A restart is not evidence of a live disconnect. Old hand-overs restored from
// the log must not sit unproven, or a later tool call would emit receipts for
// historic bubbles the browser has long since rendered as read.
func TestRestartStartsWithNothingUnproven(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	bus1, err := NewEventBusWithLog(path)
	if err != nil {
		t.Fatalf("NewEventBusWithLog (session 1): %v", err)
	}
	bus1.ReceiveUserMessage("handed over, never proven", nil, "")
	bus1.DrainMessages()
	bus1.Close()

	bus2, err := NewEventBusWithLog(path)
	if err != nil {
		t.Fatalf("NewEventBusWithLog (session 2): %v", err)
	}
	defer bus2.Close()

	before := len(bus2.EventsSince(0))
	bus2.ProveDelivery()
	if got := len(bus2.EventsSince(0)); got != before {
		t.Fatalf("ProveDelivery after restart published %d event(s); the unproven set must start empty", got-before)
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
