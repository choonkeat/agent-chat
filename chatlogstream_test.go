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

	s, err := newChatLogStream(dir, "sess-uuid-1234", "claude", "v1 (abc)", nil, now)
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
	s, err := newChatLogStream(dir, "sess-uuid-5678", "claude", "v1 (abc)", nil, now)
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

// TestChatLogStreamRename: set_chat_title after N events renames the file and
// rewrites the header (comment + H1 + byline) from in-memory history while
// leaving the body bubbles unchanged; subsequent events append to the renamed
// file.
func TestChatLogStreamRename(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-rename", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}
	oldPath := s.MDPath()

	events := []Event{
		{Type: "userMessage", Text: "fix the login bug", Timestamp: 1000},
		{Type: "agentMessage", Text: "on it", Timestamp: 4000},
	}
	for _, e := range events {
		s.HandleEvent(e)
	}

	if err := s.SetTitle("Auth Bug Fix!", events); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}

	wantPath := filepath.Join(dir, "2026-07-18-01-auth-bug-fix.md")
	if got := s.MDPath(); got != wantPath {
		t.Errorf("MDPath after rename = %s, want %s", got, wantPath)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("provisional file still exists after rename: %v", err)
	}

	titledMeta := chatExportMeta{
		Title: "Auth Bug Fix", Date: "2026-07-18", Index: "01", Slug: "auth-bug-fix",
		Session: "sess-rename", Agent: "claude", Version: "v1 (abc)",
	}
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read renamed md: %v", err)
	}
	if want := renderChatMarkdown(events, titledMeta, nil); string(got) != want {
		t.Errorf("rewritten file != batch render with new title\n--- got:\n%s\n--- want:\n%s", got, want)
	}

	// Appends continue on the renamed file with correct fold state (elapsed line).
	e3 := Event{Type: "agentMessage", Text: "done", Timestamp: 9000}
	s.HandleEvent(e3)
	got, err = os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := renderChatMarkdown(append(events, e3), titledMeta, nil); string(got) != want {
		t.Errorf("post-rename append != batch render\n--- got:\n%s\n--- want:\n%s", got, want)
	}
}

// TestChatLogStreamResume: a new stream pointed at a dir containing a file
// with a matching `session:` header continues that file — recovering
// lastTs/assetN by re-folding the in-memory history — instead of minting a
// new NN. A different session DOES mint a new NN.
func TestChatLogStreamResume(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)

	img1 := []byte("first-image-bytes")
	up1 := filepath.Join(t.TempDir(), "one.png")
	if err := os.WriteFile(up1, img1, 0644); err != nil {
		t.Fatal(err)
	}
	sum1 := sha256.Sum256(img1)
	sha1hex := hex.EncodeToString(sum1[:])[:12]

	s1, err := newChatLogStream(dir, "sess-A", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("first stream: %v", err)
	}
	history := []Event{
		{Type: "userMessage", Text: "look", Timestamp: 1000, Files: []FileRef{
			{Name: "one.png", Path: up1, Type: "image/png"},
		}},
		{Type: "agentMessage", Text: "seen", Timestamp: 5000},
	}
	for _, e := range history {
		s1.HandleEvent(e)
	}
	mdPath := s1.MDPath()
	s1.Close()

	// The upload file vanishes between restart — assets were already copied.
	if err := os.Remove(up1); err != nil {
		t.Fatal(err)
	}

	s2, err := newChatLogStream(dir, "sess-A", "claude", "v1 (abc)", history, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("resume stream: %v", err)
	}
	if got := s2.MDPath(); got != mdPath {
		t.Errorf("resume minted a new file: got %s, want %s", got, mdPath)
	}

	// New attachment after resume: assetN continues at 2.
	img2 := []byte("second-image-bytes")
	up2 := filepath.Join(t.TempDir(), "two.png")
	if err := os.WriteFile(up2, img2, 0644); err != nil {
		t.Fatal(err)
	}
	sum2 := sha256.Sum256(img2)
	sha2hex := hex.EncodeToString(sum2[:])[:12]

	e3 := Event{Type: "agentMessage", Text: "back", Timestamp: 65000, Files: []FileRef{
		{Name: "two.png", Path: up2, Type: "image/png"},
	}}
	s2.HandleEvent(e3)

	expectedMap := map[string]string{
		up1: "./assets/2026-07-18-01-1-" + sha1hex + ".png",
		up2: "./assets/2026-07-18-01-2-" + sha2hex + ".png",
	}
	meta := chatExportMeta{
		Title: "Untitled", Date: "2026-07-18", Index: "01", Slug: "untitled",
		Session: "sess-A", Agent: "claude", Version: "v1 (abc)",
	}
	got, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := renderChatMarkdown(append(history, e3), meta, expectedMap); string(got) != want {
		t.Errorf("resumed file != batch render (lastTs/assetN recovery broken)\n--- got:\n%s\n--- want:\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "assets", "2026-07-18-01-2-"+sha2hex+".png")); err != nil {
		t.Errorf("post-resume asset missing: %v", err)
	}

	// A different session must NOT steal the file — it mints the next NN.
	s3, err := newChatLogStream(dir, "sess-B", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("third stream: %v", err)
	}
	if base := filepath.Base(s3.MDPath()); base != "2026-07-18-02-untitled.md" {
		t.Errorf("different session got %s, want 2026-07-18-02-untitled.md", base)
	}
}
