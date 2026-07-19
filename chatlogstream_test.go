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

	s, err := newChatLogStream(dir, "sess-uuid-1234", "", "claude", "v1 (abc)", nil, now)
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
	s, err := newChatLogStream(dir, "sess-uuid-5678", "", "claude", "v1 (abc)", nil, now)
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
	s, err := newChatLogStream(dir, "sess-rename", "", "claude", "v1 (abc)", nil, now)
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

	s1, err := newChatLogStream(dir, "sess-A", "", "claude", "v1 (abc)", nil, now)
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

	s2, err := newChatLogStream(dir, "sess-A", "", "claude", "v1 (abc)", history, now.Add(time.Hour))
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
	s3, err := newChatLogStream(dir, "sess-B", "", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("third stream: %v", err)
	}
	if base := filepath.Base(s3.MDPath()); base != "2026-07-18-02-untitled.md" {
		t.Errorf("different session got %s, want 2026-07-18-02-untitled.md", base)
	}
}

// TestChatLogStreamEnvDisabled: no AGENT_CHAT_EXPORT_DIR value → feature off,
// no directory or files created.
func TestChatLogStreamEnvDisabled(t *testing.T) {
	cwd := t.TempDir()
	s, err := initChatLogStream("", cwd, "sid", "", "claude", "v1", nil, time.Now())
	if err != nil {
		t.Fatalf("disabled feature must not error: %v", err)
	}
	if s != nil {
		t.Fatalf("disabled feature must return nil stream, got %+v", s)
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("disabled feature created files: %v", entries)
	}
}

// TestChatLogStreamEscapesCwd: an export dir resolving outside cwd is a
// misconfiguration — warning + feature off, never a crash, and nothing is
// created in the escaping location.
func TestChatLogStreamEscapesCwd(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()
	for _, dir := range []string{outside, "../escape"} {
		s, err := initChatLogStream(dir, cwd, "sid", "", "claude", "v1", nil, time.Now())
		if err != nil {
			t.Errorf("escaping dir %q must disable, not error: %v", dir, err)
		}
		if s != nil {
			t.Errorf("escaping dir %q must return nil stream", dir)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("escaping export dir was populated: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cwd), "escape")); !os.IsNotExist(err) {
		t.Errorf("relative escaping dir was created")
	}
}

// TestChatLogStreamIndexDebounce: index.html is regenerated after the
// turn-end debounce, not synchronously per event.
func TestChatLogStreamIndexDebounce(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-debounce", "", "claude", "v1", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	s.indexDebounce = 50 * time.Millisecond

	s.HandleEvent(Event{Type: "userMessage", Text: "hi", Timestamp: 1000})
	if _, err := os.Stat(filepath.Join(dir, "index.html")); !os.IsNotExist(err) {
		t.Errorf("index.html written synchronously per event; want debounced (err=%v)", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(dir, "index.html"))
		if err == nil && strings.Contains(string(data), "2026-07-18-01-untitled.md") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("index.html not regenerated after debounce (err=%v)", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestChatLogOptout: chatlog_optout stops appends, deletes the .md (assets
// stay — content-sha names may be shared), regenerates index.html; a later
// set_chat_title re-arms the stream with a full rewrite from history.
func TestChatLogOptout(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)

	img := []byte("optout-image-bytes")
	up := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(up, img, 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(img)
	sha12 := hex.EncodeToString(sum[:])[:12]

	s, err := newChatLogStream(dir, "sess-optout", "", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	s.indexDebounce = 10 * time.Millisecond

	history := []Event{
		{Type: "userMessage", Text: "hello", Timestamp: 1000, Files: []FileRef{
			{Name: "pic.png", Path: up, Type: "image/png"},
		}},
		{Type: "agentMessage", Text: "hi", Timestamp: 4000},
	}
	for _, e := range history {
		s.HandleEvent(e)
	}
	mdPath := s.MDPath()
	asset := filepath.Join(dir, "assets", "2026-07-18-01-1-"+sha12+".png")
	if _, err := os.Stat(asset); err != nil {
		t.Fatalf("asset missing before optout: %v", err)
	}

	if err := s.Optout(); err != nil {
		t.Fatalf("Optout: %v", err)
	}
	if _, err := os.Stat(mdPath); !os.IsNotExist(err) {
		t.Errorf(".md still exists after optout (err=%v)", err)
	}
	if _, err := os.Stat(asset); err != nil {
		t.Errorf("optout deleted shared asset: %v", err)
	}
	idx, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("index.html missing after optout: %v", err)
	}
	if strings.Contains(string(idx), "untitled") {
		t.Errorf("index.html still lists the deleted export:\n%s", idx)
	}

	// Appends are stopped.
	e3 := Event{Type: "agentMessage", Text: "ignored", Timestamp: 9000}
	s.HandleEvent(e3)
	if _, err := os.Stat(mdPath); !os.IsNotExist(err) {
		t.Errorf("HandleEvent after optout recreated the file")
	}

	// set_chat_title re-arms: full rewrite from history, then appends resume.
	history = append(history, e3)
	if err := s.SetTitle("Back Again", history); err != nil {
		t.Fatalf("SetTitle re-arm: %v", err)
	}
	rearmed := filepath.Join(dir, "2026-07-18-01-back-again.md")
	if got := s.MDPath(); got != rearmed {
		t.Errorf("re-armed path = %s, want %s", got, rearmed)
	}
	meta := chatExportMeta{
		Title: "Back Again", Date: "2026-07-18", Index: "01", Slug: "back-again",
		Session: "sess-optout", Agent: "claude", Version: "v1 (abc)",
	}
	expectedMap := map[string]string{up: "./assets/2026-07-18-01-1-" + sha12 + ".png"}
	got, err := os.ReadFile(rearmed)
	if err != nil {
		t.Fatalf("read re-armed file: %v", err)
	}
	if want := renderChatMarkdown(history, meta, expectedMap); string(got) != want {
		t.Errorf("re-armed file != batch render of history\n--- got:\n%s\n--- want:\n%s", got, want)
	}
	e4 := Event{Type: "userMessage", Text: "welcome back", Timestamp: 12000}
	s.HandleEvent(e4)
	got, err = os.ReadFile(rearmed)
	if err != nil {
		t.Fatal(err)
	}
	if want := renderChatMarkdown(append(history, e4), meta, expectedMap); string(got) != want {
		t.Errorf("post-re-arm append != batch render\n--- got:\n%s\n--- want:\n%s", got, want)
	}
}

// TestChatLogStreamSessionUUIDSuffix: a non-blank sessionUUID (SESSION_UUID
// env in production) tags the provisional filename so a dangling untitled
// export is attributable to its session; the display title stays "Untitled"
// and set_chat_title drops the suffix.
func TestChatLogStreamSessionUUIDSuffix(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-tag", "B227-UUID", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}
	if base := filepath.Base(s.MDPath()); base != "2026-07-18-01-untitled-b227-uuid.md" {
		t.Errorf("provisional filename = %s, want 2026-07-18-01-untitled-b227-uuid.md", base)
	}
	if s.meta.Title != "Untitled" {
		t.Errorf("provisional title = %q, want Untitled", s.meta.Title)
	}
	if err := s.SetTitle("Real Title", nil); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}
	if base := filepath.Base(s.MDPath()); base != "2026-07-18-01-real-title.md" {
		t.Errorf("titled filename = %s, want 2026-07-18-01-real-title.md", base)
	}
}

// TestChatLogStreamSetTitleFailureKeepsStream: when the rename cannot build
// the new file (read-only dir), SetTitle errors but the stream keeps
// appending to the OLD filename — it must not go dark mid-rename.
func TestChatLogStreamSetTitleFailureKeepsStream(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-robust", "", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}
	oldPath := s.MDPath()

	events := []Event{{Type: "userMessage", Text: "hello", Timestamp: 1000}}
	s.HandleEvent(events[0])

	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0755)

	if err := s.SetTitle("Doomed Title", events); err == nil {
		t.Fatal("SetTitle in read-only dir succeeded, want error")
	}
	if got := s.MDPath(); got != oldPath {
		t.Errorf("MDPath changed after failed rename: %s, want %s", got, oldPath)
	}
	if s.meta.Slug != "untitled" {
		t.Errorf("meta.Slug mutated by failed rename: %q, want untitled", s.meta.Slug)
	}

	// The stream survives: subsequent events still land in the old file.
	e2 := Event{Type: "agentMessage", Text: "still here", Timestamp: 4000}
	s.HandleEvent(e2)
	got, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	meta := chatExportMeta{
		Title: "Untitled", Date: "2026-07-18", Index: "01", Slug: "untitled",
		Session: "sess-robust", Agent: "claude", Version: "v1 (abc)",
	}
	if want := renderChatMarkdown(append(events, e2), meta, nil); string(got) != want {
		t.Errorf("post-failure append != batch render\n--- got:\n%s\n--- want:\n%s", got, want)
	}

	// Once the dir is writable again the same rename succeeds and the stream
	// switches to the new filename.
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	history := append(events, e2)
	if err := s.SetTitle("Doomed Title", history); err != nil {
		t.Fatalf("SetTitle retry after chmod: %v", err)
	}
	wantPath := filepath.Join(dir, "2026-07-18-01-doomed-title.md")
	if got := s.MDPath(); got != wantPath {
		t.Errorf("MDPath after retry = %s, want %s", got, wantPath)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file still exists after successful retry: %v", err)
	}
}

// TestChatLogStreamCloseOut: chatlog_close freezes the .md (append no-ops)
// without deleting it, demands a title while untitled, refuses to rename an
// already-titled export, is idempotent, returns the commit paths, and
// set_chat_title re-opens with a full-history backfill of frozen messages.
func TestChatLogStreamCloseOut(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-close", "", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}

	history := []Event{{Type: "userMessage", Text: "hello", Timestamp: 1000}}
	s.HandleEvent(history[0])

	// Untitled + no title → refused.
	if _, err := s.CloseOut("", history); err == nil {
		t.Fatal("CloseOut on untitled export without title succeeded, want error")
	}

	// Untitled + title → renames and freezes in one call.
	paths, err := s.CloseOut("Close Flow", history)
	if err != nil {
		t.Fatalf("CloseOut: %v", err)
	}
	wantMD := filepath.Join(dir, "2026-07-18-01-close-flow.md")
	if s.MDPath() != wantMD {
		t.Errorf("MDPath = %s, want %s", s.MDPath(), wantMD)
	}
	wantPaths := map[string]bool{
		wantMD: false,
		filepath.Join(dir, "index.html"):           false,
		filepath.Join(dir, "assets", "viewer.css"): false,
		filepath.Join(dir, "assets", "viewer.js"):  false,
	}
	for _, p := range paths {
		if _, ok := wantPaths[p]; !ok {
			t.Errorf("unexpected path in CloseOut result: %s", p)
		}
		wantPaths[p] = true
	}
	for p, seen := range wantPaths {
		if !seen {
			t.Errorf("CloseOut result missing %s", p)
		}
	}

	// Frozen: events are dropped from the .md.
	frozen, err := os.ReadFile(wantMD)
	if err != nil {
		t.Fatal(err)
	}
	missed := Event{Type: "agentMessage", Text: "arrived while frozen", Timestamp: 5000}
	s.HandleEvent(missed)
	after, err := os.ReadFile(wantMD)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(frozen) {
		t.Error("frozen .md changed after HandleEvent")
	}

	// Idempotent: same paths, no error.
	again, err := s.CloseOut("", nil)
	if err != nil {
		t.Fatalf("second CloseOut: %v", err)
	}
	if len(again) != len(paths) {
		t.Errorf("second CloseOut returned %d paths, want %d", len(again), len(paths))
	}

	// Same title is a no-op; a different title is refused.
	if _, err := s.CloseOut("Close Flow", nil); err != nil {
		t.Errorf("CloseOut with matching title: %v", err)
	}
	if _, err := s.CloseOut("Other Name", nil); err == nil {
		t.Error("CloseOut with different title succeeded, want refusal")
	}

	// Re-open via set_chat_title: full rewrite backfills the frozen message.
	history = append(history, missed)
	if err := s.SetTitle("Close Flow", history); err != nil {
		t.Fatalf("SetTitle re-open: %v", err)
	}
	got, err := os.ReadFile(wantMD)
	if err != nil {
		t.Fatal(err)
	}
	meta := chatExportMeta{
		Title: "Close Flow", Date: "2026-07-18", Index: "01", Slug: "close-flow",
		Session: "sess-close", Agent: "claude", Version: "v1 (abc)",
	}
	if want := renderChatMarkdown(history, meta, nil); string(got) != want {
		t.Errorf("re-opened file != batch render incl. frozen-period message\n--- got:\n%s\n--- want:\n%s", got, want)
	}

	// And appends flow again.
	e3 := Event{Type: "userMessage", Text: "one more thing", Timestamp: 9000}
	s.HandleEvent(e3)
	got, err = os.ReadFile(wantMD)
	if err != nil {
		t.Fatal(err)
	}
	if want := renderChatMarkdown(append(history, e3), meta, nil); string(got) != want {
		t.Errorf("post-re-open append != batch render\n--- got:\n%s\n--- want:\n%s", got, want)
	}
}

// TestChatLogStreamCloseOutAfterOptout: optout deletes the .md, so a
// subsequent chatlog_close has nothing to freeze and must error.
func TestChatLogStreamCloseOutAfterOptout(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-close-optout", "", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}
	if err := s.Optout(); err != nil {
		t.Fatalf("Optout: %v", err)
	}
	if _, err := s.CloseOut("Some Title", nil); err == nil {
		t.Fatal("CloseOut after Optout succeeded, want error")
	}
}
