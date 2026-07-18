package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestChatLogStreamAppends: fed one event at a time, the stream's .md on disk
// always equals the batch render of the events-so-far (Step 1 equivalence),
// and an attachment's asset file exists on disk the moment its event is
// handled — not at turn end.
func TestChatLogStreamAppends(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)

	imgContent := []byte("fake-png-bytes-for-stream-test")
	upload := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(upload, imgContent, 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(imgContent)
	sha12 := hex.EncodeToString(sum[:])[:12]

	s, err := newChatLogStream(dir, "sess-uuid-1234", "claude", "v1 (abc)", now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}

	if base := filepath.Base(s.MDPath()); base != "2026-07-18-01-untitled.md" {
		t.Errorf("provisional filename = %s, want 2026-07-18-01-untitled.md", base)
	}

	events := []Event{
		{Type: "userMessage", Text: "hello", Timestamp: 1000},
		{Type: "agentMessage", Text: "hi there", Timestamp: 4500, QuickReplies: []string{"more", "stop"}},
		{Type: "userMessage", Text: "see this", Timestamp: 9000, Files: []FileRef{
			{Name: "shot.png", Path: upload, Type: "image/png"},
		}},
		{Type: "agentMessage", Text: "nice shot", Timestamp: 12000},
	}
	expectedMap := map[string]string{
		upload: "./assets/2026-07-18-01-1-" + sha12 + ".png",
	}
	meta := chatExportMeta{
		Title: "Untitled", Date: "2026-07-18", Index: "01", Slug: "untitled",
		Session: "sess-uuid-1234", Agent: "claude", Version: "v1 (abc)",
	}

	for i, e := range events {
		s.HandleEvent(e)
		got, err := os.ReadFile(s.MDPath())
		if err != nil {
			t.Fatalf("event %d: read md: %v", i, err)
		}
		want := renderChatMarkdown(events[:i+1], meta, expectedMap)
		if string(got) != want {
			t.Fatalf("event %d: on-disk md != batch render of events-so-far\n--- got:\n%s\n--- want:\n%s", i, got, want)
		}
		if i >= 2 {
			// Attachment was copied the moment its event was handled.
			asset := filepath.Join(dir, "assets", "2026-07-18-01-1-"+sha12+".png")
			if _, err := os.Stat(asset); err != nil {
				t.Fatalf("event %d: asset not on disk immediately: %v", i, err)
			}
		}
	}
}

// TestChatLogStreamSkipsHiddenEvents: bookkeeping event types produce zero
// writes — the file stays exactly the freshly-created header.
func TestChatLogStreamSkipsHiddenEvents(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-uuid-5678", "claude", "v1 (abc)", now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}
	header, err := os.ReadFile(s.MDPath())
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range []Event{
		{Type: "toolMarker", AgentToolName: "check_messages", AgentToolSeq: 1},
		{Type: "userMessagesConsumed", IDs: []string{"x"}},
		{Type: "draw", Instructions: []any{"x"}},
		{Type: "userMessage", Text: "   "}, // whitespace-only: no bubble
	} {
		s.HandleEvent(e)
	}

	got, err := os.ReadFile(s.MDPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(header) {
		t.Errorf("hidden events wrote to the md:\n--- before:\n%s\n--- after:\n%s", header, got)
	}
}
