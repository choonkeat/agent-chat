package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

// PermissionRequest represents a pending permission prompt from Claude Code.
type PermissionRequest struct {
	RequestID    string `json:"request_id"`
	ToolName     string `json:"tool_name"`
	Description  string `json:"description"`
	InputPreview string `json:"input_preview"`
}

// channelInterceptor intercepts stdin to handle Claude Code channel notifications
// (e.g. permission_request) before they reach the MCP SDK, which doesn't support
// custom notification methods.
type channelInterceptor struct {
	pipeReader *io.PipeReader // the MCP SDK reads from this
	pipeWriter *io.PipeWriter // we write non-channel lines here

	stdoutMu sync.Mutex // guards direct writes to stdout

	permMu            sync.Mutex
	pendingPermission *PermissionRequest // currently displayed permission prompt
	savedQuickReplies []string           // agent's quick replies saved before permission override

	bus *EventBus
}

// newChannelInterceptor creates an interceptor that reads from real stdin,
// handles channel notifications, and forwards everything else through a pipe.
func newChannelInterceptor(bus *EventBus) *channelInterceptor {
	pr, pw := io.Pipe()
	ci := &channelInterceptor{
		pipeReader: pr,
		pipeWriter: pw,
		bus:        bus,
	}
	go ci.readLoop()
	return ci
}

// jsonrpcMessage is the minimal structure for identifying JSON-RPC messages.
type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// readLoop reads real stdin line by line, intercepts channel notifications,
// and forwards everything else to the pipe for the MCP SDK.
func (ci *channelInterceptor) readLoop() {
	scanner := bufio.NewScanner(os.Stdin)
	// Allow large messages (16 MB)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		var msg jsonrpcMessage
		if json.Unmarshal(line, &msg) == nil && msg.ID == nil && msg.Method == "notifications/claude/channel/permission_request" {
			ci.handlePermissionRequest(msg.Params)
			continue
		}

		// Forward to MCP SDK via pipe (include newline delimiter)
		if _, err := ci.pipeWriter.Write(append(line, '\n')); err != nil {
			log.Printf("channel interceptor: pipe write error: %v", err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("channel interceptor: stdin read error: %v", err)
	}
	ci.pipeWriter.Close()
}

// handlePermissionRequest processes an incoming permission_request notification.
func (ci *channelInterceptor) handlePermissionRequest(params json.RawMessage) {
	var req PermissionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		log.Printf("channel: failed to parse permission_request params: %v", err)
		return
	}

	ci.permMu.Lock()
	// Save the agent's current quick replies so we can restore them later
	ci.savedQuickReplies = ci.bus.LastQuickReplies()
	ci.pendingPermission = &req
	ci.permMu.Unlock()

	// Format a user-friendly description
	text := fmt.Sprintf("**Permission request** — %s", req.ToolName)
	if req.Description != "" {
		text += "\n\n" + req.Description
	}
	if req.InputPreview != "" {
		text += "\n\n```\n" + req.InputPreview + "\n```"
	}

	ci.bus.Publish(Event{
		Type:         "agentMessage",
		Text:         text,
		QuickReplies: []string{"Allow", "Deny"},
	})
}

// HandleUserResponse checks if a user message is a response to a pending
// permission request. Returns true if the message was consumed as a permission
// response (and should NOT be forwarded to the agent's message queue).
//
// - "Allow" → sends allow verdict, restores agent quick replies
// - "Deny" → sends deny verdict, restores agent quick replies
// - anything else → sends deny verdict, does NOT consume (caller should forward to agent)
func (ci *channelInterceptor) HandleUserResponse(text string) bool {
	ci.permMu.Lock()
	perm := ci.pendingPermission
	if perm == nil {
		ci.permMu.Unlock()
		return false
	}

	normalized := strings.TrimSpace(strings.ToLower(text))
	switch normalized {
	case "allow":
		ci.pendingPermission = nil
		saved := ci.savedQuickReplies
		ci.savedQuickReplies = nil
		ci.permMu.Unlock()

		ci.sendVerdict(perm.RequestID, "allow")
		ci.restoreQuickReplies(saved)
		return true

	case "deny":
		ci.pendingPermission = nil
		saved := ci.savedQuickReplies
		ci.savedQuickReplies = nil
		ci.permMu.Unlock()

		ci.sendVerdict(perm.RequestID, "deny")
		ci.restoreQuickReplies(saved)
		return true

	default:
		// Free-text response: deny the permission and let the message through to the agent
		ci.pendingPermission = nil
		saved := ci.savedQuickReplies
		ci.savedQuickReplies = nil
		ci.permMu.Unlock()

		ci.sendVerdict(perm.RequestID, "deny")
		ci.restoreQuickReplies(saved)
		return false
	}
}

// sendVerdict writes a permission verdict notification directly to stdout.
func (ci *channelInterceptor) sendVerdict(requestID, behavior string) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/claude/channel/permission",
		"params": map[string]string{
			"request_id": requestID,
			"behavior":   behavior,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("channel: failed to marshal verdict: %v", err)
		return
	}
	data = append(data, '\n')

	ci.stdoutMu.Lock()
	defer ci.stdoutMu.Unlock()
	os.Stdout.Write(data)
}

// restoreQuickReplies re-publishes the agent's saved quick replies so the UI
// shows them again after a permission prompt is resolved.
func (ci *channelInterceptor) restoreQuickReplies(saved []string) {
	if len(saved) > 0 {
		ci.bus.Publish(Event{
			Type:         "agentMessage",
			Text:         "",
			QuickReplies: saved,
		})
	}
}

// HasPendingPermission returns true if there's an unresolved permission prompt.
func (ci *channelInterceptor) HasPendingPermission() bool {
	ci.permMu.Lock()
	defer ci.permMu.Unlock()
	return ci.pendingPermission != nil
}
