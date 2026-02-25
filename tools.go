package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// isVoiceMessage returns true if any message is a voice message (prefixed with 🎤).
func isVoiceMessage(msgs []UserMessage) bool {
	for _, m := range msgs {
		if strings.HasPrefix(m.Text, "\U0001f3a4 ") {
			return true
		}
	}
	return false
}

// voiceSuffix returns the appropriate reply instruction suffix.
func voiceSuffix(msgs []UserMessage) string {
	const checkMsg = "\nIMPORTANT: Call check_messages between tasks to see if the user has sent you anything. The user does not see your text replies in the TUI — always reply via send_verbal_reply/send_message so they can see it in the chat UI."
	if isVoiceMessage(msgs) {
		return "(Ask any question or Reply to user by voice using send_verbal_reply — keep it conversational, plain text only, no markdown. For non-blocking updates use send_verbal_progress.)" + checkMsg
	}
	return "(Ask any question or Reply to user in chat when done)" + checkMsg
}

// MessageParams are the parameters for the send_message tool.
type MessageParams struct {
	Text             string   `json:"text"`
	QuickReply       string   `json:"quick_reply"`
	MoreQuickReplies []string `json:"more_quick_replies,omitempty"`
	ImageURLs        []string `json:"image_urls,omitempty"`
}

// VerbalReplyParams are the parameters for the send_verbal_reply tool.
type VerbalReplyParams struct {
	Text             string   `json:"text"`
	QuickReply       string   `json:"quick_reply"`
	MoreQuickReplies []string `json:"more_quick_replies,omitempty"`
	ImageURLs        []string `json:"image_urls,omitempty"`
}

// resolveImageFiles copies local image files into the upload directory and returns FileRefs.
func resolveImageFiles(paths []string) []FileRef {
	var refs []FileRef
	for _, p := range paths {
		if p == "" {
			continue
		}
		src, err := os.Open(p)
		if err != nil {
			continue
		}

		info, err := src.Stat()
		if err != nil {
			src.Close()
			continue
		}

		base := filepath.Base(p)
		prefix := uuid.New().String()[:8]
		savedName := prefix + "-" + base
		destPath := filepath.Join(uploadDir, savedName)

		dst, err := os.Create(destPath)
		if err != nil {
			src.Close()
			continue
		}

		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			src.Close()
			continue
		}
		dst.Close()
		src.Close()

		mimeType := mime.TypeByExtension(filepath.Ext(base))
		if mimeType == "" {
			mimeType = "image/png"
		}

		refs = append(refs, FileRef{
			Name: base,
			Path: destPath,
			URL:  "/uploads/" + savedName,
			Size: info.Size(),
			Type: mimeType,
		})
	}
	return refs
}

func registerTools(server *mcp.Server, bus *EventBus) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_message",
		Description: "Send a text message to the whiteboard chat and wait for viewer response. Use this to respond conversationally to viewer feedback (e.g., acknowledging 'Slower pace' or answering a question). Blocks until the viewer responds, like draw. IMPORTANT: The user can send messages at any time. Call check_messages periodically between tasks to see if the user has sent you anything. The user does not see your text replies in the TUI — always reply via send_message so they can see it in the chat UI.\n\nThe `quick_reply` field is the primary reply option shown to the user. Use `more_quick_replies` for additional options.\n\nOptionally pass `image_urls` with an array of absolute paths to local image files (e.g., screenshots) to include them inline in the message.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *MessageParams) (*mcp.CallToolResult, any, error) {
		// Reject send_message when user is in voice mode — agent must use send_verbal_reply
		if bus.LastVoice() {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "ERROR: The user is in voice mode. Use send_verbal_reply instead of send_message to respond."},
				},
				IsError: true,
			}, nil, nil
		}

		// Lazily start HTTP server + open browser
		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		// Open browser if not already opened this session
		httpMu.Lock()
		shouldOpen := uiURL != "" && !browserOpened
		if shouldOpen {
			openBrowser(uiURL)
			browserOpened = true
		}
		httpMu.Unlock()

		// Wait for at least one viewer (browser) to be connected
		if err := bus.WaitForSubscriber(ctx); err != nil {
			return nil, nil, fmt.Errorf("waiting for browser: %w", err)
		}

		replies := append([]string{params.QuickReply}, params.MoreQuickReplies...)
		files := resolveImageFiles(params.ImageURLs)
		bus.Publish(Event{Type: "agentMessage", Text: params.Text, QuickReplies: replies, Files: files})

		msgs, err := bus.WaitForMessages(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("waiting for user message: %w", err)
		}

		bus.SetLastVoice(isVoiceMessage(msgs))
		text := "User responded: " + FormatMessages(msgs) + "\n\n" + voiceSuffix(msgs)
		if uiURL != "" {
			text += "\nChat UI: " + uiURL
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_verbal_reply",
		Description: "Send a spoken reply to the user in voice mode. Use this tool when the user's message starts with 🎙 (microphone emoji), indicating they are using voice input. Keep replies conversational, concise, and plain text only — no markdown, no code blocks, no links. The text will be spoken aloud via browser text-to-speech. After speaking, the browser automatically listens for the user's next voice input.\n\nThe `quick_reply` field is the primary reply option shown to the user. Use `more_quick_replies` for additional options.\n\nOptionally pass `image_urls` with an array of absolute paths to local image files (e.g., screenshots) to include them inline in the message.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *VerbalReplyParams) (*mcp.CallToolResult, any, error) {
		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		httpMu.Lock()
		shouldOpen := uiURL != "" && !browserOpened
		if shouldOpen {
			openBrowser(uiURL)
			browserOpened = true
		}
		httpMu.Unlock()

		if err := bus.WaitForSubscriber(ctx); err != nil {
			return nil, nil, fmt.Errorf("waiting for browser: %w", err)
		}

		replies := append([]string{params.QuickReply}, params.MoreQuickReplies...)
		files := resolveImageFiles(params.ImageURLs)
		bus.Publish(Event{Type: "verbalReply", Text: params.Text, QuickReplies: replies, Files: files})

		msgs, err := bus.WaitForMessages(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("waiting for user message: %w", err)
		}

		bus.SetLastVoice(isVoiceMessage(msgs))
		text := "User responded: " + FormatMessages(msgs) + "\n\n" + voiceSuffix(msgs)
		if uiURL != "" {
			text += "\nChat UI: " + uiURL
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})

	// DrawParams are the parameters for the draw tool.
	type DrawParams struct {
		Text             string   `json:"text"`
		Instructions     []any    `json:"instructions"`
		QuickReply       string   `json:"quick_reply"`
		MoreQuickReplies []string `json:"more_quick_replies,omitempty"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "draw",
		Description: `Draw a diagram as an inline canvas bubble in the chat and wait for viewer response.

Each draw call creates a new canvas bubble in the chat history, rendered with a hand-drawn aesthetic.
Use send_message for explanatory text before or after drawing.

HOW IT WORKS:
• Each draw call = one slide. Build complex diagrams across multiple slides (gradual reveal).
• Viewer clicks Continue (or gives feedback like "Slower pace") before this tool returns.
• The result tells you what the viewer said—adjust your next slide accordingly.

INSTRUCTIONS FORMAT — JSON objects with "type" field:
  [{"type":"drawRect","x":100,"y":100,"width":150,"height":60,"fill":"#E3F2FD"},
   {"type":"writeText","text":"Client","x":130,"y":140,"fontSize":16},
   {"type":"moveTo","x":250,"y":130},{"type":"lineTo","x":350,"y":130}]

COMMON TYPES: moveTo, lineTo, drawRect, drawCircle, writeText, setColor

Read whiteboard://instructions for all instruction types with parameters.
Read whiteboard://diagramming-guide for layout rules and cognitive principles.

The ` + "`quick_reply`" + ` field is the primary reply option shown to the viewer. Use ` + "`more_quick_replies`" + ` for additional options.`,
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *DrawParams) (*mcp.CallToolResult, any, error) {
		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		httpMu.Lock()
		shouldOpen := uiURL != "" && !browserOpened
		if shouldOpen {
			openBrowser(uiURL)
			browserOpened = true
		}
		httpMu.Unlock()

		if err := bus.WaitForSubscriber(ctx); err != nil {
			return nil, nil, fmt.Errorf("waiting for browser: %w", err)
		}

		// Publish text as a chat bubble before the canvas
		bus.Publish(Event{Type: "agentMessage", Text: params.Text})

		replies := append([]string{params.QuickReply}, params.MoreQuickReplies...)
		ack := bus.CreateAck()
		bus.Publish(Event{
			Type:         "draw",
			Instructions: params.Instructions,
			QuickReplies: replies,
			AckID:        ack.ID,
		})

		var result string
		select {
		case result = <-ack.Ch:
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("draw cancelled: %w", ctx.Err())
		}

		text := "Viewer acknowledged."
		if result != "ack" && len(result) > 4 {
			msg := result[4:] // strip "ack:" prefix
			text = "Viewer responded: " + msg + "\n\n(Reply to user in chat when done)"
		}

		if uiURL != "" {
			text += "\nChat UI: " + uiURL
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})

	// ProgressParams are the parameters for the send_progress tool.
	type ProgressParams struct {
		Text      string   `json:"text"`
		ImageURLs []string `json:"image_urls,omitempty"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_progress",
		Description: "Send a progress update to the chat UI without blocking. Use this for status updates (e.g., 'Working on it...', 'Found 3 matching files') when you want to keep the user informed but don't need a response. Unlike send_message, this returns immediately.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *ProgressParams) (*mcp.CallToolResult, any, error) {
		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		files := resolveImageFiles(params.ImageURLs)
		bus.Publish(Event{Type: "agentMessage", Text: params.Text, Files: files})

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Progress sent."},
			},
		}, nil, nil
	})

	// VerbalProgressParams are the parameters for the send_verbal_progress tool.
	type VerbalProgressParams struct {
		Text      string   `json:"text"`
		ImageURLs []string `json:"image_urls,omitempty"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_verbal_progress",
		Description: "Send a spoken progress update to the user in voice mode without blocking. Use this for non-blocking status updates that should be spoken aloud (e.g., 'Looking into that now', 'Found the issue'). Unlike send_verbal_reply, this returns immediately without waiting for a response. The text will be spoken via browser text-to-speech. Keep it conversational, concise, and plain text only — no markdown, no code blocks, no links.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *VerbalProgressParams) (*mcp.CallToolResult, any, error) {
		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		files := resolveImageFiles(params.ImageURLs)
		bus.Publish(Event{Type: "verbalReply", Text: params.Text, Files: files})

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Verbal progress sent."},
			},
		}, nil, nil
	})

	type EmptyParams struct{}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_messages",
		Description: "Non-blocking check for user messages. Returns any queued messages from the chat UI, or 'No new messages.' if the queue is empty. Call this periodically between tasks to stay responsive to user input.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *EmptyParams) (*mcp.CallToolResult, any, error) {
		msgs := bus.DrainMessages()
		var result string
		if len(msgs) == 0 {
			result = "No new messages."
		} else {
			bus.SetLastVoice(isVoiceMessage(msgs))
			result = "User said: " + FormatMessages(msgs) + "\n\n" + voiceSuffix(msgs)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: result},
			},
		}, nil, nil
	})
}
