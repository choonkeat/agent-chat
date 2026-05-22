package main

import (
	"context"
	"testing"
	"time"
)

// resetToolCounters returns the per-test cleanup for the shared atomic
// counters so each test starts from zero regardless of order.
func resetToolCounters(t *testing.T) {
	t.Helper()
	prevSM := sendMessageCount.Load()
	prevSP := sendProgressCount.Load()
	prevSVR := sendVerbalReplyCount.Load()
	prevSVP := sendVerbalProgressCount.Load()
	prevCM := checkMessagesCount.Load()
	sendMessageCount.Store(0)
	sendProgressCount.Store(0)
	sendVerbalReplyCount.Store(0)
	sendVerbalProgressCount.Store(0)
	checkMessagesCount.Store(0)
	t.Cleanup(func() {
		sendMessageCount.Store(prevSM)
		sendProgressCount.Store(prevSP)
		sendVerbalReplyCount.Store(prevSVR)
		sendVerbalProgressCount.Store(prevSVP)
		checkMessagesCount.Store(prevCM)
	})
}

func TestSeedToolCounters_RecoversMaxPerTool(t *testing.T) {
	resetToolCounters(t)

	events := []Event{
		{Type: "agentMessage", Text: "hi", AgentToolName: "send_message", AgentToolSeq: 1},
		{Type: "agentMessage", Text: "progress", AgentToolName: "send_progress", AgentToolSeq: 1},
		{Type: "agentMessage", Text: "bye", AgentToolName: "send_message", AgentToolSeq: 2},
		{Type: "userMessagesConsumed", IDs: []string{"u1"}, AgentToolName: "check_messages", AgentToolSeq: 1},
		// Out-of-order entry shouldn't fool the max calc.
		{Type: "agentMessage", Text: "later", AgentToolName: "send_progress", AgentToolSeq: 5},
		{Type: "agentMessage", Text: "earlier", AgentToolName: "send_progress", AgentToolSeq: 3},
		// Unstamped event (legacy) must not affect counters.
		{Type: "agentMessage", Text: "legacy"},
	}

	SeedToolCounters(events)

	if got, want := sendMessageCount.Load(), int64(2); got != want {
		t.Errorf("sendMessageCount: got %d, want %d", got, want)
	}
	if got, want := sendProgressCount.Load(), int64(5); got != want {
		t.Errorf("sendProgressCount: got %d, want %d", got, want)
	}
	if got, want := checkMessagesCount.Load(), int64(1); got != want {
		t.Errorf("checkMessagesCount: got %d, want %d", got, want)
	}
	if got, want := sendVerbalReplyCount.Load(), int64(0); got != want {
		t.Errorf("sendVerbalReplyCount: got %d, want %d", got, want)
	}
	if got, want := sendVerbalProgressCount.Load(), int64(0); got != want {
		t.Errorf("sendVerbalProgressCount: got %d, want %d", got, want)
	}
}

func TestSeedToolCounters_EmptyEvents_LeavesCountersAtZero(t *testing.T) {
	resetToolCounters(t)
	SeedToolCounters(nil)
	if got := sendMessageCount.Load(); got != 0 {
		t.Errorf("sendMessageCount: got %d, want 0", got)
	}
}

// TestPublishConsumed_StampsEvent confirms publishConsumed stamps the userMessagesConsumed
// event with the supplied tool ordinal so a fork resolver can locate the MCP call.
func TestPublishConsumed_StampsEvent(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	bus.publishConsumed([]UserMessage{
		{ID: "u-1", Text: "hello"},
		{ID: "u-2", Text: "world"},
	}, "check_messages", 42)

	select {
	case ev := <-ch:
		if ev.Type != "userMessagesConsumed" {
			t.Fatalf("unexpected event type %q", ev.Type)
		}
		if ev.AgentToolName != "check_messages" {
			t.Errorf("AgentToolName: got %q, want %q", ev.AgentToolName, "check_messages")
		}
		if ev.AgentToolSeq != 42 {
			t.Errorf("AgentToolSeq: got %d, want %d", ev.AgentToolSeq, 42)
		}
		if len(ev.IDs) != 2 || ev.IDs[0] != "u-1" || ev.IDs[1] != "u-2" {
			t.Errorf("IDs: got %v, want [u-1 u-2]", ev.IDs)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("no userMessagesConsumed event observed")
	}
}

// TestDrainMessagesStamped_PropagatesToolStamp confirms the convenience wrapper
// forwards toolName/toolSeq through to the published consumed event.
func TestDrainMessagesStamped_PropagatesToolStamp(t *testing.T) {
	bus := NewEventBus()
	bus.ReceiveUserMessage("hi", nil)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	msgs := bus.DrainMessagesStamped("check_messages", 7)
	if len(msgs) == 0 {
		t.Fatal("no messages drained")
	}

	deadline := time.After(100 * time.Millisecond)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "userMessagesConsumed" {
				continue
			}
			if ev.AgentToolName != "check_messages" || ev.AgentToolSeq != 7 {
				t.Errorf("stamp: got (%q, %d), want (check_messages, 7)", ev.AgentToolName, ev.AgentToolSeq)
			}
			return
		case <-deadline:
			t.Fatal("did not observe stamped userMessagesConsumed")
		}
	}
}

// TestWaitForMessagesStamped_PropagatesToolStamp confirms WaitForMessagesStamped
// also threads the stamp.
func TestWaitForMessagesStamped_PropagatesToolStamp(t *testing.T) {
	bus := NewEventBus()
	bus.ReceiveUserMessage("hi", nil)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	msgs, err := bus.WaitForMessagesStamped(ctx, "send_message", 3)
	if err != nil {
		t.Fatalf("WaitForMessagesStamped: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}

	deadline := time.After(100 * time.Millisecond)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "userMessagesConsumed" {
				continue
			}
			if ev.AgentToolName != "send_message" || ev.AgentToolSeq != 3 {
				t.Errorf("stamp: got (%q, %d), want (send_message, 3)", ev.AgentToolName, ev.AgentToolSeq)
			}
			return
		case <-deadline:
			t.Fatal("did not observe stamped userMessagesConsumed")
		}
	}
}

// TestDrainMessages_LegacyUnstamped confirms the no-arg variant keeps emitting
// unstamped events (zero/empty fields) for callers that don't track ordinals.
func TestDrainMessages_LegacyUnstamped(t *testing.T) {
	bus := NewEventBus()
	bus.ReceiveUserMessage("hi", nil)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	if msgs := bus.DrainMessages(); len(msgs) == 0 {
		t.Fatal("no messages")
	}

	deadline := time.After(100 * time.Millisecond)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "userMessagesConsumed" {
				continue
			}
			if ev.AgentToolName != "" || ev.AgentToolSeq != 0 {
				t.Errorf("expected unstamped event, got (%q, %d)", ev.AgentToolName, ev.AgentToolSeq)
			}
			return
		case <-deadline:
			t.Fatal("did not observe userMessagesConsumed")
		}
	}
}
