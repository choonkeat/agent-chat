package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MessageParams are the parameters for the send_message tool.
type MessageParams struct {
	Text string `json:"text"`
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

		ack := bus.CreateAck()

		bus.Publish(Event{Type: "agentMessage", Text: params.Text, AckID: ack.ID})

		var result string
		select {
		case result = <-ack.Ch:
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("message cancelled: %w", ctx.Err())
		}

		var text string
		switch {
		case result == "ack":
			text = "User acknowledged."
		default:
			msg := result[len("ack:"):]
			text = "User responded: " + msg
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
}
