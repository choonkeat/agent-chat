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
		Description: "Send a text message to the whiteboard chat and wait for viewer response. Use this to respond conversationally to viewer feedback (e.g., acknowledging 'Slower pace' or answering a question). Blocks until the viewer responds, like draw. Returns the current canvas dimensions — call this before your first draw to discover the canvas size.",
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
}
