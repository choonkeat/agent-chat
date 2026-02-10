package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MessageParams are the parameters for the send_message tool.
type MessageParams struct {
	Text         string   `json:"text"`
	QuickReplies []string `json:"quick_replies,omitempty"`
}

func registerTools(server *mcp.Server, bus *EventBus) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_message",
		Description: "Send a text message to the whiteboard chat and wait for viewer response. Use this to respond conversationally to viewer feedback (e.g., acknowledging 'Slower pace' or answering a question). Blocks until the viewer responds, like draw. Returns the current canvas dimensions — call this before your first draw to discover the canvas size. IMPORTANT: The user can send messages at any time. Call check_messages periodically between tasks to see if the user has sent you anything. The user does not see your text replies in the TUI — always reply via send_message so they can see it in the chat UI.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *MessageParams) (*mcp.CallToolResult, any, error) {
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

		bus.Publish(Event{Type: "agentMessage", Text: params.Text, QuickReplies: params.QuickReplies})

		result, err := bus.WaitForMessages(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("waiting for user message: %w", err)
		}

		text := "User responded: " + result

		if uiURL != "" {
			text += " Chat UI: " + uiURL
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})

	// DrawParams are the parameters for the draw tool.
	type DrawParams struct {
		Instructions []any    `json:"instructions"`
		QuickReplies []string `json:"quick_replies,omitempty"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "draw",
		Description: `Draw a diagram as an inline canvas bubble in the chat and wait for viewer response.

Each draw call creates a new canvas bubble in the chat history, rendered with a hand-drawn aesthetic.
Use send_message for explanatory text before or after drawing.

INSTRUCTIONS FORMAT — JSON objects with "type" field:
  [{"type":"drawRect","x":100,"y":100,"width":150,"height":60,"fill":"#E3F2FD"},
   {"type":"writeText","text":"Client","x":130,"y":140,"fontSize":16},
   {"type":"moveTo","x":250,"y":130},{"type":"lineTo","x":350,"y":130}]

COMMON TYPES: moveTo, lineTo, drawRect, drawCircle, writeText, setColor

Read whiteboard://instructions for all instruction types with parameters.
Read whiteboard://diagramming-guide for layout rules and cognitive principles.`,
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

		ack := bus.CreateAck()
		bus.Publish(Event{
			Type:         "draw",
			Instructions: params.Instructions,
			QuickReplies: params.QuickReplies,
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
			text = "Viewer responded: " + msg
		}

		if uiURL != "" {
			text += " Chat UI: " + uiURL
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})

	type EmptyParams struct{}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_messages",
		Description: "Non-blocking check for user messages. Returns any queued messages from the chat UI, or 'No new messages.' if the queue is empty. Call this periodically between tasks to stay responsive to user input.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *EmptyParams) (*mcp.CallToolResult, any, error) {
		result := bus.DrainMessages()
		if result == "" {
			result = "No new messages."
		} else {
			bus.LogUserMessage(result)
			result = "User said: " + result + "\n\n(Reply to user in chat when done)"
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: result},
			},
		}, nil, nil
	})
}
