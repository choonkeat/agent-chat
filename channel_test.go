package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHandlePermissionRequest verifies that a permission_request notification
// is intercepted and published as an agentMessage with Allow/Deny quick replies.
func TestHandlePermissionRequest(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	ci := &channelInterceptor{bus: bus}

	sub := bus.Subscribe()
	defer bus.Unsubscribe(sub)

	params, _ := json.Marshal(PermissionRequest{
		RequestID:    "abcde",
		ToolName:     "Bash",
		Description:  "Run a shell command",
		InputPreview: `{"command":"git status"}`,
	})
	ci.handlePermissionRequest(params)

	select {
	case evt := <-sub:
		if evt.Type != "agentMessage" {
			t.Fatalf("expected agentMessage, got %s", evt.Type)
		}
		if !strings.Contains(evt.Text, "Bash") {
			t.Errorf("expected text to contain tool name 'Bash', got %q", evt.Text)
		}
		if !strings.Contains(evt.Text, "git status") {
			t.Errorf("expected text to contain input preview, got %q", evt.Text)
		}
		if len(evt.QuickReplies) != 2 || evt.QuickReplies[0] != "Allow" || evt.QuickReplies[1] != "Deny" {
			t.Errorf("expected quick replies [Allow, Deny], got %v", evt.QuickReplies)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	if !ci.HasPendingPermission() {
		t.Error("expected pending permission after request")
	}
}

// TestHandleUserResponseAllow verifies that "Allow" consumes the message and sends an allow verdict.
func TestHandleUserResponseAllow(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	// Capture stdout to verify verdict is written
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ci := &channelInterceptor{bus: bus}
	ci.pendingPermission = &PermissionRequest{RequestID: "fghkm", ToolName: "Write"}
	ci.savedQuickReplies = []string{"Yes", "No"}

	consumed := ci.HandleUserResponse("Allow")

	// Restore stdout and read what was written
	w.Close()
	os.Stdout = origStdout
	output, _ := io.ReadAll(r)

	if !consumed {
		t.Error("expected Allow to be consumed")
	}
	if ci.HasPendingPermission() {
		t.Error("expected no pending permission after Allow")
	}

	var verdict map[string]any
	if err := json.Unmarshal(output, &verdict); err != nil {
		t.Fatalf("failed to parse verdict JSON: %v (output: %s)", err, output)
	}
	params, ok := verdict["params"].(map[string]any)
	if !ok {
		t.Fatalf("expected params object, got %v", verdict["params"])
	}
	if params["request_id"] != "fghkm" {
		t.Errorf("expected request_id fghkm, got %v", params["request_id"])
	}
	if params["behavior"] != "allow" {
		t.Errorf("expected behavior allow, got %v", params["behavior"])
	}
}

// TestHandleUserResponseDeny verifies that "Deny" consumes the message and sends a deny verdict.
func TestHandleUserResponseDeny(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ci := &channelInterceptor{bus: bus}
	ci.pendingPermission = &PermissionRequest{RequestID: "bcdfg", ToolName: "Edit"}

	consumed := ci.HandleUserResponse("Deny")

	w.Close()
	os.Stdout = origStdout
	output, _ := io.ReadAll(r)

	if !consumed {
		t.Error("expected Deny to be consumed")
	}

	var verdict map[string]any
	json.Unmarshal(output, &verdict)
	params := verdict["params"].(map[string]any)
	if params["behavior"] != "deny" {
		t.Errorf("expected behavior deny, got %v", params["behavior"])
	}
}

// TestHandleUserResponseFreeText verifies that non-Allow/Deny text sends deny
// but does NOT consume the message (returns false so it gets forwarded to agent).
func TestHandleUserResponseFreeText(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ci := &channelInterceptor{bus: bus}
	ci.pendingPermission = &PermissionRequest{RequestID: "hjknp", ToolName: "Bash"}

	consumed := ci.HandleUserResponse("Please do something else")

	w.Close()
	os.Stdout = origStdout
	output, _ := io.ReadAll(r)

	if consumed {
		t.Error("expected free text NOT to be consumed")
	}

	var verdict map[string]any
	json.Unmarshal(output, &verdict)
	params := verdict["params"].(map[string]any)
	if params["behavior"] != "deny" {
		t.Errorf("expected implicit deny, got %v", params["behavior"])
	}
}

// TestHandleUserResponseNoPending verifies that without a pending permission,
// HandleUserResponse returns false (pass-through).
func TestHandleUserResponseNoPending(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	ci := &channelInterceptor{bus: bus}
	consumed := ci.HandleUserResponse("hello")
	if consumed {
		t.Error("expected no consumption when no permission is pending")
	}
}

// TestHandleUserResponseCaseInsensitive verifies that "allow", "ALLOW", etc. work.
func TestHandleUserResponseCaseInsensitive(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	origStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	ci := &channelInterceptor{bus: bus}
	ci.pendingPermission = &PermissionRequest{RequestID: "aaaaa", ToolName: "Bash"}

	consumed := ci.HandleUserResponse("  ALLOW  ")

	w.Close()
	os.Stdout = origStdout

	if !consumed {
		t.Error("expected case-insensitive Allow to be consumed")
	}
}

// TestHandleUserResponseVoicePrefix verifies that a voice-prefixed "🎤 Allow" is consumed.
func TestHandleUserResponseVoicePrefix(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	origStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	ci := &channelInterceptor{bus: bus}
	ci.pendingPermission = &PermissionRequest{RequestID: "ccccc", ToolName: "Bash"}

	consumed := ci.HandleUserResponse("\U0001f3a4 Allow")

	w.Close()
	os.Stdout = origStdout

	if !consumed {
		t.Error("expected voice-prefixed Allow to be consumed")
	}
}

// TestRestoreQuickReplies verifies that agent quick replies are restored after permission resolution.
func TestRestoreQuickReplies(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	origStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	sub := bus.Subscribe()
	defer bus.Unsubscribe(sub)

	ci := &channelInterceptor{bus: bus}
	ci.pendingPermission = &PermissionRequest{RequestID: "bbbbb", ToolName: "Bash"}
	ci.savedQuickReplies = []string{"Option A", "Option B"}

	ci.HandleUserResponse("Allow")

	w.Close()
	os.Stdout = origStdout

	// Drain events — expect a restore event with the saved quick replies
	var found bool
	timeout := time.After(time.Second)
	for !found {
		select {
		case evt := <-sub:
			if evt.Type == "agentMessage" && len(evt.QuickReplies) == 2 &&
				evt.QuickReplies[0] == "Option A" && evt.QuickReplies[1] == "Option B" {
				found = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for restored quick replies")
		}
	}
}

// TestReadLoopInterceptsPermissionRequest verifies the stdin readLoop intercepts
// permission_request notifications and forwards other messages.
func TestReadLoopInterceptsPermissionRequest(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()

	// Create a pipe to simulate stdin
	stdinR, stdinW, _ := os.Pipe()
	origStdin := os.Stdin
	os.Stdin = stdinR

	pr, pw := io.Pipe()
	ci := &channelInterceptor{
		pipeReader: pr,
		pipeWriter: pw,
		bus:        bus,
	}

	sub := bus.Subscribe()
	defer bus.Unsubscribe(sub)

	go ci.readLoop()

	// Write a permission request notification
	permReq := `{"jsonrpc":"2.0","method":"notifications/claude/channel/permission_request","params":{"request_id":"ccccc","tool_name":"Bash","description":"Run command","input_preview":"ls"}}` + "\n"
	stdinW.WriteString(permReq)

	// Write a normal MCP message (should pass through to pipe)
	normalMsg := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	stdinW.WriteString(normalMsg)

	// Verify permission request was intercepted (published as event)
	select {
	case evt := <-sub:
		if evt.Type != "agentMessage" {
			t.Fatalf("expected agentMessage, got %s", evt.Type)
		}
		if !strings.Contains(evt.Text, "Bash") {
			t.Errorf("expected text to mention Bash, got %q", evt.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission event")
	}

	// Verify normal message was forwarded through pipe
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, len(normalMsg))
		n, _ := io.ReadFull(pr, buf)
		done <- string(buf[:n])
	}()
	select {
	case got := <-done:
		if got != normalMsg {
			t.Errorf("expected normal message to pass through, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for normal message on pipe")
	}

	// Cleanup
	stdinW.Close()
	os.Stdin = origStdin
}
