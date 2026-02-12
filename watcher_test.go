package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestWatcher_DetectsPermissionPrompt(t *testing.T) {
	// Create a temporary JSONL file
	f, err := os.CreateTemp(t.TempDir(), "session-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	// Write initial content (watcher seeks to end, so this is skipped)
	f.WriteString(`{"type":"user","message":{"role":"user","content":"hello"}}` + "\n")
	f.Close()

	bus := NewEventBus()
	sub := bus.Subscribe()
	defer bus.Unsubscribe(sub)

	w := NewWatcher(f.Name(), bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)

	// Give watcher time to start and seek to end
	time.Sleep(100 * time.Millisecond)

	// Append a tool_use line (assistant entry with Bash command)
	appendLine := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_test123","name":"Bash","input":{"command":"ls /tmp","description":"List temp files"}}]}}`
	appendToFile(t, f.Name(), appendLine+"\n")

	// Wait for the timeout to fire (1.5s + buffer)
	select {
	case event := <-sub:
		if event.Type != "permissionPrompt" {
			t.Errorf("expected permissionPrompt event, got %q", event.Type)
		}
		if event.ToolUseID != "toolu_test123" {
			t.Errorf("wrong ToolUseID: %s", event.ToolUseID)
		}
		if event.ToolName != "Bash" {
			t.Errorf("wrong ToolName: %s", event.ToolName)
		}
		if event.Detail != "ls /tmp" {
			t.Errorf("wrong Detail: %s", event.Detail)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for permissionPrompt event")
	}
}

func TestWatcher_ResolvedBeforeTimeout(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "session-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"user","message":{"role":"user","content":"hello"}}` + "\n")
	f.Close()

	bus := NewEventBus()
	sub := bus.Subscribe()
	defer bus.Unsubscribe(sub)

	w := NewWatcher(f.Name(), bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	// Append tool_use
	toolUseLine := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_fast456","name":"Read","input":{"file_path":"/tmp/foo"}}]}}`
	appendToFile(t, f.Name(), toolUseLine+"\n")

	// Quickly append tool_result (before 1.5s timeout)
	time.Sleep(100 * time.Millisecond)
	toolResultLine := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_fast456","content":"file contents"}]}}`
	appendToFile(t, f.Name(), toolResultLine+"\n")

	// We should get a permissionResolved event (not a permissionPrompt)
	select {
	case event := <-sub:
		if event.Type != "permissionResolved" {
			t.Errorf("expected permissionResolved, got %q", event.Type)
		}
		if event.ToolUseID != "toolu_fast456" {
			t.Errorf("wrong ToolUseID: %s", event.ToolUseID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	// Make sure no permissionPrompt fires after
	select {
	case event := <-sub:
		if event.Type == "permissionPrompt" && event.ToolUseID == "toolu_fast456" {
			t.Error("got unexpected permissionPrompt after resolution")
		}
	case <-time.After(2 * time.Second):
		// Good — no prompt fired
	}
}

func TestWatcher_SkipsHistoricalLines(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "session-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	// Pre-existing tool_use should be ignored (watcher seeks to end)
	f.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_old","name":"Bash","input":{"command":"echo old"}}]}}` + "\n")
	f.Close()

	bus := NewEventBus()
	sub := bus.Subscribe()
	defer bus.Unsubscribe(sub)

	w := NewWatcher(f.Name(), bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)

	// Wait longer than the timeout
	select {
	case event := <-sub:
		t.Errorf("unexpected event for historical line: %+v", event)
	case <-time.After(2500 * time.Millisecond):
		// Good — no events from historical data
	}
}

func appendToFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
}
