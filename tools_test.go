package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tt
}

// Common expected substrings for reply instructions (shared across tests).
const (
	replyInstructionsBody = "The TUI is invisible to the user (so don't ever call the built-in AskUserQuestion tool). EVERY user-visible message — questions, status, final answers, errors — must go through `send_message` or `send_progress`. Plain text in your response is never seen by the user.\n\n" +
		"- If the request is ambiguous, risky, or destructive, confirm with `send_message` BEFORE acting. Otherwise just proceed.\n" +
		"- Use `send_progress` for non-blocking status updates during long work. If the user sends a barge-in message while you are working, it will be appended to the next `send_progress` return value after a `---BARGE-IN---` sentinel — treat that as a new instruction. You do NOT need to poll for it.\n" +
		"- When the task is done, deliver the result with `send_message` and wait. NEVER end your turn without calling `send_message` — going silent looks like a crash to the user."

	replyInstructionsVoiceBody = "User can only hear you now; keep it conversational, no markdown.\n" +
		"IMPORTANT: Never put more than one question in a single message. Wait for the answer before asking the next question.\n\n" +
		"The TUI is invisible to the user (so don't ever call the built-in AskUserQuestion tool). EVERY user-visible message — questions, status, final answers, errors — must go through `send_verbal_reply` or `send_verbal_progress`. Plain text in your response is never seen by the user.\n\n" +
		"- If the request is ambiguous, risky, or destructive, confirm with `send_verbal_reply` BEFORE acting. Otherwise just proceed.\n" +
		"- Use `send_verbal_progress` for non-blocking status updates during long work. If the user sends a barge-in message while you are working, it will be appended to the next `send_verbal_progress` return value after a `---BARGE-IN---` sentinel — treat that as a new instruction. You do NOT need to poll for it.\n" +
		"- When the task is done, deliver the result with `send_verbal_reply` and wait. NEVER end your turn without calling `send_verbal_reply` — going silent looks like a crash to the user."
)

func TestFormatMessagesPlainText(t *testing.T) {
	msgs := []UserMessage{{Text: "hello world"}}
	got := FormatMessages(msgs)
	want := "hello world"
	if got != want {
		t.Errorf("FormatMessages plain text:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatMessagesVoice(t *testing.T) {
	msgs := []UserMessage{{Text: "\U0001f3a4 turn the box red"}}
	got := FormatMessages(msgs)
	want := "Decoded user's speech to text (may be inaccurate): turn the box red"
	if got != want {
		t.Errorf("FormatMessages voice:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatMessagesWithFileAttachment(t *testing.T) {
	msgs := []UserMessage{{
		Text: "check this file",
		Files: []FileRef{
			{Name: "photo.png", Path: "/tmp/photo.png", Type: "image/png", Size: 2048},
		},
	}}
	got := FormatMessages(msgs)
	want := "check this file\n\nAttached files:\n  /tmp/photo.png (image/png, 2KB)"
	if got != want {
		t.Errorf("FormatMessages with file:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatMessagesFileAttachmentSizeFormatting(t *testing.T) {
	tests := []struct {
		name string
		size int64
		want string // just the size part
	}{
		{"bytes", 500, "500B"},
		{"kilobytes", 10240, "10KB"},
		{"megabytes", 2 * 1024 * 1024, "2.0MB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := []UserMessage{{
				Text:  "f",
				Files: []FileRef{{Path: "/tmp/f", Type: "text/plain", Size: tt.size}},
			}}
			got := FormatMessages(msgs)
			wantSuffix := "/tmp/f (text/plain, " + tt.want + ")"
			if !strings.Contains(got, wantSuffix) {
				t.Errorf("size formatting %q: got %q, want to contain %q", tt.name, got, wantSuffix)
			}
		})
	}
}

func TestFormatMessagesFileAttachmentNoMIME(t *testing.T) {
	msgs := []UserMessage{{
		Text:  "here",
		Files: []FileRef{{Path: "/tmp/data.bin", Size: 100}},
	}}
	got := FormatMessages(msgs)
	want := "here\n\nAttached files:\n  /tmp/data.bin (application/octet-stream, 100B)"
	if got != want {
		t.Errorf("FormatMessages no MIME:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFormatMessagesMultiple(t *testing.T) {
	msgs := []UserMessage{
		{Text: "first message"},
		{Text: "second message"},
	}
	got := FormatMessages(msgs)
	want := "first message\n\nsecond message"
	if got != want {
		t.Errorf("FormatMessages multiple:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestVoiceSuffixTextMessage(t *testing.T) {
	msgs := []UserMessage{{Text: "hello"}}
	got := voiceSuffix(msgs)
	if got != replyInstructionsBody {
		t.Errorf("voiceSuffix text:\ngot:  %q\nwant: %q", got, replyInstructionsBody)
	}
}

func TestVoiceSuffixVoiceMessage(t *testing.T) {
	msgs := []UserMessage{{Text: "\U0001f3a4 do something"}}
	got := voiceSuffix(msgs)
	if got != replyInstructionsVoiceBody {
		t.Errorf("voiceSuffix voice:\ngot:  %q\nwant: %q", got, replyInstructionsVoiceBody)
	}
}

func TestIsVoiceMessage(t *testing.T) {
	tests := []struct {
		name string
		msgs []UserMessage
		want bool
	}{
		{"plain text", []UserMessage{{Text: "hello"}}, false},
		{"voice prefix", []UserMessage{{Text: "\U0001f3a4 hello"}}, true},
		{"mixed with voice", []UserMessage{{Text: "plain"}, {Text: "\U0001f3a4 voice"}}, true},
		{"empty", nil, false},
		{"emoji without space", []UserMessage{{Text: "\U0001f3a4hello"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isVoiceMessage(tt.msgs)
			if got != tt.want {
				t.Errorf("isVoiceMessage(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestComposedResultSendMessage(t *testing.T) {
	msgs := []UserMessage{{Text: "looks good"}}
	got := "User responded: " + FormatMessages(msgs) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(msgs)
	want := "User responded: looks good\n\n" + executeNotEchoGuidance + "\n\n" + replyInstructionsBody
	if got != want {
		t.Errorf("composed result (text):\ngot:  %q\nwant: %q", got, want)
	}
}

func TestComposedResultVoiceMessage(t *testing.T) {
	msgs := []UserMessage{{Text: "\U0001f3a4 make it blue"}}
	got := "User responded: " + FormatMessages(msgs) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(msgs)
	want := "User responded: Decoded user's speech to text (may be inaccurate): make it blue\n\n" +
		executeNotEchoGuidance + "\n\n" + replyInstructionsVoiceBody
	if got != want {
		t.Errorf("composed result (voice):\ngot:  %q\nwant: %q", got, want)
	}
}

func TestComposedResultCheckMessages(t *testing.T) {
	msgs := []UserMessage{{Text: "update please"}}
	got := "User said: " + FormatMessages(msgs) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(msgs)
	want := "User said: update please\n\n" + executeNotEchoGuidance + "\n\n" + replyInstructionsBody
	if got != want {
		t.Errorf("composed result (check_messages):\ngot:  %q\nwant: %q", got, want)
	}
}

func TestEmptyQueueGuidance(t *testing.T) {
	// Preserve the machine-parseable {"queue":"empty"} prefix so any existing
	// programmatic check still works, AND include guidance against echoing the
	// empty state back as a send_message reply.
	if !strings.HasPrefix(emptyQueueGuidance, `{"queue":"empty"}`) {
		t.Errorf("emptyQueueGuidance must start with {\"queue\":\"empty\"} for backward-compat; got: %q", emptyQueueGuidance)
	}
	if !strings.Contains(emptyQueueGuidance, "Do NOT call send_message") {
		t.Errorf("emptyQueueGuidance must warn against sending a user-visible reply; got: %q", emptyQueueGuidance)
	}
}

func TestAppendBargeInEmptyQueueNoOp(t *testing.T) {
	bus := NewEventBus()
	got := appendBargeIn(bus, "Progress sent.")
	want := "Progress sent."
	if got != want {
		t.Errorf("appendBargeIn empty queue:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestAppendBargeInPicksUpQueuedMessage(t *testing.T) {
	bus := NewEventBus()
	bus.PushMessage("skip e2e, just unit tests", nil)
	got := appendBargeIn(bus, "Progress sent.")
	if !strings.Contains(got, "---BARGE-IN---") {
		t.Errorf("appendBargeIn missing sentinel:\n%s", got)
	}
	if !strings.Contains(got, "skip e2e, just unit tests") {
		t.Errorf("appendBargeIn missing message body:\n%s", got)
	}
	if !strings.HasPrefix(got, "Progress sent.") {
		t.Errorf("appendBargeIn must preserve original text prefix:\n%s", got)
	}
	if !strings.Contains(got, executeNotEchoGuidance) {
		t.Errorf("appendBargeIn missing execute-not-echo guidance:\n%s", got)
	}
}

func TestAppendBargeInDrainsQueue(t *testing.T) {
	bus := NewEventBus()
	bus.PushMessage("first", nil)
	_ = appendBargeIn(bus, "Progress sent.")
	// Second call should now be a no-op because the first drained the queue.
	got := appendBargeIn(bus, "Progress sent.")
	if got != "Progress sent." {
		t.Errorf("appendBargeIn did not drain queue; second call returned:\n%s", got)
	}
}

func TestComposedResultWithFiles(t *testing.T) {
	msgs := []UserMessage{{
		Text: "review this",
		Files: []FileRef{
			{Name: "main.go", Path: "/tmp/main.go", Type: "text/x-go", Size: 4096},
		},
	}}
	got := "User responded: " + FormatMessages(msgs) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(msgs)
	want := "User responded: review this\n\nAttached files:\n  /tmp/main.go (text/x-go, 4KB)\n\n" +
		executeNotEchoGuidance + "\n\n" + replyInstructionsBody
	if got != want {
		t.Errorf("composed result (files):\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSlugifyTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"auth bug fix", "auth-bug-fix"},
		{"Fix Auth Bug!", "fix-auth-bug"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"already-kebab-case", "already-kebab-case"},
		{"under_score/slash", "under-score-slash"},
		{"unicode—dash", "unicode-dash"},
		{"multiple   spaces", "multiple-spaces"},
		{"v1.2.3 release", "v1-2-3-release"},
		{"!!!", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := slugifyTitle(c.in)
		if got != c.want {
			t.Errorf("slugifyTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNextDailyIndexGapTolerantAndSlugDigits(t *testing.T) {
	dir := t.TempDir()

	// Pre-populate with files that have gaps and a slug starting with digits.
	for _, name := range []string{
		"2026-04-25-01-foo.html",
		"2026-04-25-03-bar.html",                  // gap at 02
		"2026-04-25-1234567890-numeric-slug.html", // not an index — slug starts with digits
		"2026-04-25-99-other.md",                  // markdown counts toward the daily index
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	if got := nextDailyIndex(dir, "2026-04-25"); got != 100 {
		t.Errorf("nextDailyIndex with gaps + .md = %d, want 100", got)
	}
}

func TestRunChatMarkdownExportFreshDir(t *testing.T) {
	dir := t.TempDir()
	now := mustParseTime(t, "2026-04-30T10:00:00Z")
	events := []Event{
		{Type: "userMessage", Text: "hello", Timestamp: 1000},
		{Type: "agentMessage", Text: "hi there", Timestamp: 4500, QuickReplies: []string{"more", "stop"}},
		{Type: "userMessage", Text: "thanks", Timestamp: 5000},
	}
	mdPath, _, err := runChatMarkdownExport(dir, "test-chat", events, "claude", "v0.5.0 (abc123)", now)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	wantBase := "2026-04-30-01-test-chat.md"
	if filepath.Base(mdPath) != wantBase {
		t.Errorf("md path = %s, want base %s", mdPath, wantBase)
	}

	// .md file must exist with expected structure.
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read md: %v", err)
	}
	mdStr := string(md)
	wantContains := []string{
		"<!-- agent-chat export",
		"title: Test Chat",
		"date: 2026-04-30",
		"index: 01",
		"slug: test-chat",
		"agent: claude",
		"version: v0.5.0 (abc123)",
		"-->",
		"# Test Chat",
		"_2026-04-30 · 01 · claude · agent-chat v0.5.0 (abc123)_",
		"**USER**\n\n> hello",
		"**AGENT**\n\n> hi there",
		"**USER**\n\n> thanks",
		"[Quick replies]\n- more\n- stop",
	}
	for _, w := range wantContains {
		if !strings.Contains(mdStr, w) {
			t.Errorf("md missing %q\n--- md ---\n%s", w, mdStr)
		}
	}
	// User at ts=1000, agent at ts=4500 → 3.5s elapsed before the first agent
	// turn. Mirrors the JS viewer logic which emits elapsed before any
	// non-user bubble whose preceding bubble has a timestamp.
	if !strings.Contains(mdStr, "<small>took 3.5s</small><br>\n**AGENT**") {
		t.Errorf("expected elapsed-time prefix before first agent turn; md:\n%s", mdStr)
	}

	// Viewer assets must exist.
	for _, name := range []string{"viewer.css", "viewer.js"} {
		if _, err := os.Stat(filepath.Join(dir, "assets", name)); err != nil {
			t.Errorf("expected %s in assets: %v", name, err)
		}
	}

	// index.html must exist with manifest entry.
	idx, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	idxStr := string(idx)
	wantIdx := []string{
		`{ md: './2026-04-30-01-test-chat.md', date: '2026-04-30', idx: '01', title: 'Test Chat' },`,
		"agent-chat:manifest-insert",
	}
	for _, w := range wantIdx {
		if !strings.Contains(idxStr, w) {
			t.Errorf("index.html missing %q", w)
		}
	}
}

func TestRunChatMarkdownExportPrependsToExistingIndex(t *testing.T) {
	dir := t.TempDir()
	now1 := mustParseTime(t, "2026-04-30T10:00:00Z")
	now2 := mustParseTime(t, "2026-04-30T11:00:00Z") // same day → idx 02

	if _, _, err := runChatMarkdownExport(dir, "first", []Event{{Type: "userMessage", Text: "a"}}, "claude", "v1", now1); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, _, err := runChatMarkdownExport(dir, "second", []Event{{Type: "userMessage", Text: "b"}}, "claude", "v1", now2); err != nil {
		t.Fatalf("second: %v", err)
	}

	idx, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	idxStr := string(idx)
	posSecond := strings.Index(idxStr, "2026-04-30-02-second.md")
	posFirst := strings.Index(idxStr, "2026-04-30-01-first.md")
	if posSecond < 0 || posFirst < 0 {
		t.Fatalf("missing entries; second=%d first=%d", posSecond, posFirst)
	}
	if posSecond > posFirst {
		t.Errorf("second entry should appear before first (newest first); got second=%d first=%d", posSecond, posFirst)
	}
}

func TestRenderChatMarkdownAlternates(t *testing.T) {
	events := []Event{
		{Type: "userMessage", Text: "U1", Timestamp: 1000},
		{Type: "agentMessage", Text: "A1", Timestamp: 5000},
		{Type: "userMessage", Text: "U2", Timestamp: 6000},
		{Type: "agentMessage", Text: "A2", Timestamp: 38000},
	}
	md := renderChatMarkdown(events, chatExportMeta{
		Title: "T", Date: "2026-04-30", Index: "01", Slug: "t",
	}, nil)

	posU1 := strings.Index(md, "> U1")
	posA1 := strings.Index(md, "> A1")
	posU2 := strings.Index(md, "> U2")
	posA2 := strings.Index(md, "> A2")
	if !(posU1 < posA1 && posA1 < posU2 && posU2 < posA2) {
		t.Errorf("turns out of order: U1=%d A1=%d U2=%d A2=%d", posU1, posA1, posU2, posA2)
	}
	if !strings.Contains(md, "**USER**\n\n> U1") {
		t.Errorf("expected user marker + blockquoted body; got:\n%s", md)
	}
	if !strings.Contains(md, "**AGENT**\n\n> A1") {
		t.Errorf("expected agent marker + blockquoted body; got:\n%s", md)
	}

	// Second agent turn (A2) follows U2 with a 32s gap → elapsed prefix.
	if !strings.Contains(md, "<small>took 32.0s</small><br>\n**AGENT**\n\n> A2") {
		t.Errorf("expected `took 32.0s` elapsed-time prefix before A2; got:\n%s", md)
	}

	// Whitespace-only user message produces no turn.
	mdEmpty := renderChatMarkdown([]Event{{Type: "userMessage", Text: "   "}}, chatExportMeta{Title: "x", Date: "d", Index: "01"}, nil)
	if strings.Contains(mdEmpty, "**USER**") {
		t.Errorf("empty user message should not emit a turn marker")
	}
}

// TestRunChatMarkdownExportEmbedsAgentImages locks in the contract that
// screenshots attached to an agent turn (via send_message/send_progress) are
// copied into assets/ and rendered inline in the AGENT blockquote — symmetric
// with user uploads. This restores the all-parties behavior the original
// browser-rendered export_chat_html had; the server-side markdown rewrite
// (dff1d6d) silently regressed it to user-only. See ADR
// 2026-05-30-export-embeds-agent-images.md.
func TestRunChatMarkdownExportEmbedsAgentImages(t *testing.T) {
	dir := t.TempDir()
	now := mustParseTime(t, "2026-05-30T10:00:00Z")

	// A real source image the exporter can copy into assets/.
	src := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(src, []byte("\x89PNG\r\n\x1a\nfake"), 0644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	events := []Event{
		{Type: "userMessage", Text: "take a screenshot", Timestamp: 1000},
		{Type: "agentMessage", Text: "here it is", Timestamp: 2000,
			Files: []FileRef{{Name: "shot.png", Path: src, Type: "image/png"}}},
	}
	mdPath, _, err := runChatMarkdownExport(dir, "agent-shot", events, "claude", "v1", now)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read md: %v", err)
	}
	mdStr := string(md)

	// The agent body and its image must both appear, with the image inside the
	// AGENT turn (after the body, not in some user turn).
	if !strings.Contains(mdStr, "**AGENT**\n\n> here it is") {
		t.Errorf("agent body missing/misformatted; got:\n%s", mdStr)
	}
	// Asset filenames carry a content sha (first 12 hex of sha256) before the
	// extension so distinct content can never collide on numbering alone.
	sum := sha256.Sum256([]byte("\x89PNG\r\n\x1a\nfake"))
	sha := hex.EncodeToString(sum[:])[:12]
	asset := "2026-05-30-01-1-" + sha + ".png"
	rel := "./assets/" + asset
	if !strings.Contains(mdStr, `<img src="`+rel+`"`) {
		t.Errorf("agent image not rendered inline; want img src %q; got:\n%s", rel, mdStr)
	}
	// The image markup must sit within the agent turn (after **AGENT**), and
	// there is no later turn to bleed into.
	if ai := strings.Index(mdStr, "**AGENT**"); ai < 0 || strings.Index(mdStr, rel) < ai {
		t.Errorf("agent image rendered outside the agent turn; got:\n%s", mdStr)
	}

	// The bytes must have been copied into assets/.
	if _, err := os.Stat(filepath.Join(dir, "assets", asset)); err != nil {
		t.Errorf("agent screenshot not copied to assets: %v", err)
	}
}

// TestRunChatMarkdownExportSkipsMissingAttachment locks in that an attachment
// whose source file has gone missing (uploads are transient scratch files) does
// not fail the whole export: the turn's other content is still archived, the
// present sibling attachment is still copied, and the loss is reported as a
// warning rather than an error.
func TestRunChatMarkdownExportSkipsMissingAttachment(t *testing.T) {
	dir := t.TempDir()
	now := mustParseTime(t, "2026-07-03T10:00:00Z")

	// One real image, one referencing a path that no longer exists.
	present := filepath.Join(dir, "here.png")
	if err := os.WriteFile(present, []byte("\x89PNG\r\n\x1a\nreal"), 0644); err != nil {
		t.Fatalf("write present: %v", err)
	}
	missing := filepath.Join(dir, "gone.png") // never created

	events := []Event{
		{Type: "userMessage", Text: "see attached", Timestamp: 1000,
			Files: []FileRef{
				{Name: "gone.png", Path: missing, Type: "image/png"},
				{Name: "here.png", Path: present, Type: "image/png"},
			}},
	}

	mdPath, warnings, err := runChatMarkdownExport(dir, "missing-attach", events, "claude", "v1", now)
	if err != nil {
		t.Fatalf("export must not fail on a missing attachment: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("want exactly 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "gone.png") || !strings.Contains(warnings[0], missing) {
		t.Errorf("warning should name the missing file and its path; got %q", warnings[0])
	}

	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read md: %v", err)
	}
	mdStr := string(md)

	// The user body is archived regardless of the missing attachment.
	if !strings.Contains(mdStr, "**USER**\n\n> see attached") {
		t.Errorf("user body missing; got:\n%s", mdStr)
	}
	// The present sibling is still copied and referenced.
	sum := sha256.Sum256([]byte("\x89PNG\r\n\x1a\nreal"))
	sha := hex.EncodeToString(sum[:])[:12]
	// The missing file consumes its sequence number before the copy fails, so
	// the present sibling lands on index 2 (a harmless gap in numbering).
	rel := "./assets/2026-07-03-01-2-" + sha + ".png"
	if !strings.Contains(mdStr, `<img src="`+rel+`"`) {
		t.Errorf("present image not rendered inline; want img src %q; got:\n%s", rel, mdStr)
	}
	// The missing file must not leave a broken <img> reference behind.
	if strings.Contains(mdStr, "gone.png") {
		t.Errorf("missing attachment should not be referenced in the .md; got:\n%s", mdStr)
	}
}

// TestRenderChatMarkdownIgnoresToolMarker locks in the contract that hidden
// toolMarker events (emitted on routine early-returns to keep restart-seed
// counts aligned) never surface in the export and never perturb the elapsed-time
// deltas between real bubbles.
func TestRenderChatMarkdownIgnoresToolMarker(t *testing.T) {
	events := []Event{
		{Type: "userMessage", Text: "U1", Timestamp: 1000},
		// A phantom check_messages drain sits between the two real turns.
		{Type: "toolMarker", AgentToolName: "check_messages", AgentToolSeq: 2, Timestamp: 4000},
		{Type: "agentMessage", Text: "A1", Timestamp: 33000},
	}
	md := renderChatMarkdown(events, chatExportMeta{Title: "T", Date: "d", Index: "01"}, nil)

	if strings.Contains(md, "toolMarker") || strings.Contains(md, "check_messages") {
		t.Errorf("toolMarker leaked into export:\n%s", md)
	}
	// Timing must measure U1->A1 (32s), unaffected by the marker's 4000 ts.
	if !strings.Contains(md, "<small>took 32.0s</small><br>\n**AGENT**\n\n> A1") {
		t.Errorf("marker perturbed elapsed-time delta; got:\n%s", md)
	}

	// A marker-only log produces no turns at all.
	mdOnly := renderChatMarkdown([]Event{
		{Type: "toolMarker", AgentToolName: "send_message", AgentToolSeq: 1},
	}, chatExportMeta{Title: "x", Date: "d", Index: "01"}, nil)
	if strings.Contains(mdOnly, "**USER**") || strings.Contains(mdOnly, "**AGENT**") {
		t.Errorf("marker-only log should emit no turns; got:\n%s", mdOnly)
	}
}

func TestRenderChatMarkdownBlockquoteEscape(t *testing.T) {
	// Body with leading `> ` should nest one level deeper, not overwrite the
	// turn's blockquote prefix.
	events := []Event{
		{Type: "userMessage", Text: "regular line\n> already quoted\nback to normal"},
	}
	md := renderChatMarkdown(events, chatExportMeta{Title: "T", Date: "d", Index: "01"}, nil)
	want := "**USER**\n\n> regular line\n> > already quoted\n> back to normal"
	if !strings.Contains(md, want) {
		t.Errorf("blockquote nesting wrong:\nwant substring: %q\ngot:\n%s", want, md)
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := []struct{ ms int64; want string }{
		{500, "500ms"},
		{1500, "1.5s"},
		{37900, "37.9s"},
		{75000, "1m 15s"},
		{134000, "2m 14s"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.ms); got != c.want {
			t.Errorf("formatElapsed(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

// --- check_messages redelivery (pending-ack) composition ---

func TestComposeCheckMessagesResultEmpty(t *testing.T) {
	got := composeCheckMessagesResult(nil, nil)
	if got != emptyQueueGuidance {
		t.Errorf("empty/empty must return emptyQueueGuidance, got: %q", got)
	}
}

func TestComposeCheckMessagesResultFreshOnly(t *testing.T) {
	fresh := []UserMessage{{Text: "update please"}}
	got := composeCheckMessagesResult(nil, fresh)
	want := "User said: update please\n\n" + executeNotEchoGuidance + "\n\n" + replyInstructionsBody
	if got != want {
		t.Errorf("fresh-only:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestComposeCheckMessagesResultLimboOnly(t *testing.T) {
	limbo := []UserMessage{{Text: "did you get my reply"}}
	got := composeCheckMessagesResult(limbo, nil)
	if !strings.Contains(got, "---REDELIVERY---") {
		t.Errorf("limbo-only missing redelivery sentinel:\n%s", got)
	}
	if !strings.Contains(got, "did you get my reply") {
		t.Errorf("limbo-only missing redelivered body:\n%s", got)
	}
	if !strings.Contains(got, "already") {
		t.Errorf("limbo-only must tell the agent to ignore already-handled messages:\n%s", got)
	}
	if strings.Contains(got, `{"queue":"empty"}`) {
		t.Errorf("limbo-only must not claim the queue is empty:\n%s", got)
	}
}

func TestComposeCheckMessagesResultFreshAndLimbo(t *testing.T) {
	limbo := []UserMessage{{Text: "possibly lost"}}
	fresh := []UserMessage{{Text: "new instruction"}}
	got := composeCheckMessagesResult(limbo, fresh)
	// Fresh messages are the authoritative instruction and must lead.
	if !strings.HasPrefix(got, "User said: new instruction") {
		t.Errorf("fresh must lead the result:\n%s", got)
	}
	idxFresh := strings.Index(got, "new instruction")
	idxLimbo := strings.Index(got, "possibly lost")
	if idxLimbo < idxFresh {
		t.Errorf("redelivered messages must come after fresh ones:\n%s", got)
	}
	if !strings.Contains(got, "---REDELIVERY---") {
		t.Errorf("missing redelivery sentinel:\n%s", got)
	}
}

// --- progress keepalive ---

type fakeProgressNotifier struct {
	mu    sync.Mutex
	calls []mcp.ProgressNotificationParams
}

func (f *fakeProgressNotifier) NotifyProgress(ctx context.Context, p *mcp.ProgressNotificationParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, *p)
	return nil
}

func (f *fakeProgressNotifier) snapshot() []mcp.ProgressNotificationParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mcp.ProgressNotificationParams(nil), f.calls...)
}

func TestProgressKeepaliveSendsNotifications(t *testing.T) {
	fake := &fakeProgressNotifier{}
	stop := startProgressKeepalive(context.Background(), fake, "tok-1", 5*time.Millisecond, "waiting for user reply")

	deadline := time.After(2 * time.Second)
	for {
		if len(fake.snapshot()) >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected >=2 progress notifications, got %d", len(fake.snapshot()))
		case <-time.After(2 * time.Millisecond):
		}
	}
	stop()
	n := len(fake.snapshot())
	time.Sleep(30 * time.Millisecond)
	if after := len(fake.snapshot()); after != n {
		t.Errorf("keepalive kept firing after stop: %d -> %d", n, after)
	}

	calls := fake.snapshot()
	if calls[0].ProgressToken != "tok-1" {
		t.Errorf("wrong progress token: %v", calls[0].ProgressToken)
	}
	if calls[0].Message != "waiting for user reply" {
		t.Errorf("wrong message: %q", calls[0].Message)
	}
	if !(calls[1].Progress > calls[0].Progress) {
		t.Errorf("progress must increase monotonically: %v then %v", calls[0].Progress, calls[1].Progress)
	}
}

func TestProgressKeepaliveStopsOnContextCancel(t *testing.T) {
	fake := &fakeProgressNotifier{}
	ctx, cancel := context.WithCancel(context.Background())
	stop := startProgressKeepalive(ctx, fake, "tok-2", 5*time.Millisecond, "waiting")
	defer stop()
	cancel()
	time.Sleep(15 * time.Millisecond)
	n := len(fake.snapshot())
	time.Sleep(30 * time.Millisecond)
	if after := len(fake.snapshot()); after != n {
		t.Errorf("keepalive kept firing after ctx cancel: %d -> %d", n, after)
	}
}

func TestKeepaliveForRequestNoTokenNoOp(t *testing.T) {
	// A request without a progress token must yield a no-op stopper and
	// never panic.
	stop := keepaliveForRequest(context.Background(), &mcp.CallToolRequest{}, "waiting")
	stop()
}
