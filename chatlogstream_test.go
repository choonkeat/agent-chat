package main

import (
	"strings"
	"testing"
)

// TestRenderChatBubbleMatchesBatch proves the per-bubble renderer extraction
// is a pure refactor: folding renderChatBubble over a fixture event list must
// reproduce renderChatMarkdown's output byte-for-byte (header included via a
// zero-event batch render).
func TestRenderChatBubbleMatchesBatch(t *testing.T) {
	events := []Event{
		{Type: "userMessage", Text: "hello", Timestamp: 1000},
		{Type: "toolMarker", AgentToolName: "check_messages", AgentToolSeq: 1},
		{Type: "agentMessage", Text: "hi there\nsecond line", Timestamp: 3500, QuickReplies: []string{"Yes", "No"}},
		{Type: "userMessage", Text: "look at this", Timestamp: 60000, Files: []FileRef{
			{Name: "shot.png", Path: "/up/shot.png", Type: "image/png"},
		}},
		{Type: "agentMessage", Text: "   ", Timestamp: 61000}, // whitespace-only: skipped, must not emit elapsed line
		{Type: "verbalReply", Text: "spoken reply", Timestamp: 65000},
		{Type: "agentMessage", Text: "", Timestamp: 70000, Files: []FileRef{
			{Name: "doc.pdf", Path: "/up/doc.pdf", Type: "application/pdf"},
		}},
		{Type: "userMessage", Text: "no timestamp"},
		{Type: "agentMessage", Text: "after zero-ts user", Timestamp: 80000},
	}
	imageMap := map[string]string{
		"/up/shot.png": "./assets/2026-07-18-01-1-abcdefabcdef.png",
		"/up/doc.pdf":  "./assets/2026-07-18-01-2-fedcbafedcba.pdf",
	}
	meta := chatExportMeta{
		Title: "Test Chat", Date: "2026-07-18", Index: "01",
		Slug: "test-chat", Agent: "claude", Version: "v1 (abc123)",
	}

	batch := renderChatMarkdown(events, meta, imageMap)
	header := renderChatMarkdown(nil, meta, imageMap)
	if !strings.HasPrefix(batch, header) {
		t.Fatalf("batch render does not start with the zero-event header:\nheader:\n%s\nbatch:\n%s", header, batch)
	}

	var st renderState
	var b strings.Builder
	b.WriteString(header)
	for _, e := range events {
		b.WriteString(renderChatBubble(e, &st, imageMap))
	}
	if got := b.String(); got != batch {
		t.Errorf("folded renderChatBubble output differs from batch render\n--- fold:\n%s\n--- batch:\n%s", got, batch)
	}
}
