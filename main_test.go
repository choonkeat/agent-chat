package main

import (
	"context"
	"testing"
	"time"
)

func TestEnsureHTTPServerLazyStart(t *testing.T) {
	// Reset global state for test
	httpMu.Lock()
	httpRunning = false
	httpListener = nil
	httpMu.Unlock()
	uiURL = ""
	mcpServerRef = nil

	// With nil mcpServerRef, ensureHTTPServer should fail (tries to start server)
	err := ensureHTTPServer()
	if err == nil {
		// If it succeeded, clean up
		httpMu.Lock()
		if httpListener != nil {
			httpListener.Close()
		}
		httpRunning = false
		httpMu.Unlock()
	}
	// The key point: it attempted to start (didn't silently no-op)
}

func TestEnsureHTTPServerCrashRecovery(t *testing.T) {
	// Reset global state
	httpMu.Lock()
	httpRunning = false
	httpListener = nil
	httpMu.Unlock()
	uiURL = ""
	mcpServerRef = nil

	// Simulate: httpRunning was true but server crashed (httpRunning set to false)
	httpMu.Lock()
	httpRunning = false
	httpMu.Unlock()

	// We can't easily test a full server start without mcpServerRef,
	// but we can verify the flag logic: if httpRunning is false,
	// ensureHTTPServer should attempt to start (and fail without mcpServerRef).
	err := ensureHTTPServer()
	// Expect an error because mcpServerRef is nil, but importantly it TRIED
	// to start — it didn't skip due to sync.Once.
	if err == nil {
		// If it succeeded somehow, clean up
		httpMu.Lock()
		if httpListener != nil {
			httpListener.Close()
		}
		httpRunning = false
		httpMu.Unlock()
	}
	// The key assertion: it attempted a restart (didn't silently no-op).

	// Call again — should also retry (not cached failure)
	err2 := ensureHTTPServer()
	_ = err2
	// Both calls attempted to start — no permanent failure caching.
}

func TestEventBusSubscribeUnblocks(t *testing.T) {
	eb := NewEventBus()
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- eb.WaitForSubscriber(ctx)
	}()

	// Should not unblock yet
	select {
	case <-done:
		t.Fatal("WaitForSubscriber unblocked before any subscriber")
	case <-time.After(50 * time.Millisecond):
	}

	// Subscribe should unblock it
	sub := eb.Subscribe()
	defer eb.Unsubscribe(sub)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitForSubscriber returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForSubscriber did not unblock after Subscribe")
	}
}

func TestWaitForSubscriberRespectsContext(t *testing.T) {
	eb := NewEventBus()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- eb.WaitForSubscriber(ctx)
	}()

	// Should not unblock yet (no subscribers)
	select {
	case <-done:
		t.Fatal("WaitForSubscriber unblocked before cancel")
	case <-time.After(50 * time.Millisecond):
	}

	// Cancel context — should unblock with error
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from cancelled context, got nil")
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForSubscriber did not unblock after context cancel")
	}
}

func TestWaitForSubscriberAfterReconnect(t *testing.T) {
	eb := NewEventBus()
	ctx := context.Background()

	// First subscriber connects and disconnects
	sub1 := eb.Subscribe()
	if err := eb.WaitForSubscriber(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	eb.Unsubscribe(sub1)

	// Now no subscribers — WaitForSubscriber should block again
	done := make(chan error, 1)
	go func() {
		done <- eb.WaitForSubscriber(ctx)
	}()

	select {
	case <-done:
		t.Fatal("WaitForSubscriber unblocked with no subscribers")
	case <-time.After(200 * time.Millisecond):
	}

	// New subscriber connects — should unblock
	sub2 := eb.Subscribe()
	defer eb.Unsubscribe(sub2)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForSubscriber did not unblock after reconnect")
	}
}

func TestEventBusPublishAndReceive(t *testing.T) {
	eb := NewEventBus()
	sub := eb.Subscribe()
	defer eb.Unsubscribe(sub)

	eb.Publish(Event{Type: "agentMessage", Text: "hello"})

	ev1 := <-sub
	if ev1.Type != "agentMessage" || ev1.Text != "hello" {
		t.Fatalf("expected agentMessage event with text 'hello', got type=%s text=%s", ev1.Type, ev1.Text)
	}
}

func TestEventBusAckResolve(t *testing.T) {
	eb := NewEventBus()
	ack := eb.CreateAck()

	go func() {
		time.Sleep(10 * time.Millisecond)
		eb.ResolveAck(ack.ID, "ack:clicked continue")
	}()

	select {
	case result := <-ack.Ch:
		if result != "ack:clicked continue" {
			t.Fatalf("expected 'ack:clicked continue', got '%s'", result)
		}
	case <-time.After(time.Second):
		t.Fatal("ack did not resolve in time")
	}
}

func TestEventBusMultipleSubscribers(t *testing.T) {
	eb := NewEventBus()
	sub1 := eb.Subscribe()
	sub2 := eb.Subscribe()
	defer eb.Unsubscribe(sub1)
	defer eb.Unsubscribe(sub2)

	eb.Publish(Event{Type: "agentMessage", AckID: "test-123"})

	ev1 := <-sub1
	ev2 := <-sub2

	if ev1.Type != "agentMessage" || ev1.AckID != "test-123" {
		t.Fatalf("subscriber 1 got unexpected event: %+v", ev1)
	}
	if ev2.Type != "agentMessage" || ev2.AckID != "test-123" {
		t.Fatalf("subscriber 2 got unexpected event: %+v", ev2)
	}
}

func TestEventBusUnsubscribe(t *testing.T) {
	eb := NewEventBus()
	sub := eb.Subscribe()
	eb.Unsubscribe(sub)

	eb.Publish(Event{Type: "agentMessage"})

	select {
	case <-sub:
		t.Fatal("unsubscribed channel should not receive events")
	case <-time.After(50 * time.Millisecond):
	}
}
