package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Common expected substrings for reply instructions (shared across tests).
const (
	replyInstructionsBody = "The TUI is invisible to the user. EVERY user-visible message — questions, status, final answers, errors — must go through `send_message` or `send_progress`. Plain text in your response is never seen by the user.\n\n" +
		"- If the request is ambiguous, risky, or destructive, confirm with `send_message` BEFORE acting. Otherwise just proceed.\n" +
		"- Use `send_progress` for non-blocking status updates during long work.\n" +
		"- When the task is done, deliver the result with `send_message` and wait. NEVER end your turn without calling `send_message` — going silent looks like a crash to the user.\n" +
		"- For long-running multi-step work, call `check_messages` between steps to stay responsive."

	replyInstructionsVoiceBody = "User can only hear you now; keep it conversational, no markdown.\n" +
		"IMPORTANT: Never put more than one question in a single message. Wait for the answer before asking the next question.\n\n" +
		"The TUI is invisible to the user. EVERY user-visible message — questions, status, final answers, errors — must go through `send_verbal_reply` or `send_verbal_progress`. Plain text in your response is never seen by the user.\n\n" +
		"- If the request is ambiguous, risky, or destructive, confirm with `send_verbal_reply` BEFORE acting. Otherwise just proceed.\n" +
		"- Use `send_verbal_progress` for non-blocking status updates during long work.\n" +
		"- When the task is done, deliver the result with `send_verbal_reply` and wait. NEVER end your turn without calling `send_verbal_reply` — going silent looks like a crash to the user.\n" +
		"- For long-running multi-step work, call `check_messages` between steps to stay responsive."
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
	got := "User responded: " + FormatMessages(msgs) + "\n\n" + voiceSuffix(msgs)
	want := "User responded: looks good\n\n" + replyInstructionsBody
	if got != want {
		t.Errorf("composed result (text):\ngot:  %q\nwant: %q", got, want)
	}
}

func TestComposedResultVoiceMessage(t *testing.T) {
	msgs := []UserMessage{{Text: "\U0001f3a4 make it blue"}}
	got := "User responded: " + FormatMessages(msgs) + "\n\n" + voiceSuffix(msgs)
	want := "User responded: Decoded user's speech to text (may be inaccurate): make it blue\n\n" +
		replyInstructionsVoiceBody
	if got != want {
		t.Errorf("composed result (voice):\ngot:  %q\nwant: %q", got, want)
	}
}

func TestComposedResultCheckMessages(t *testing.T) {
	msgs := []UserMessage{{Text: "update please"}}
	got := "User said: " + FormatMessages(msgs) + "\n\n" + voiceSuffix(msgs)
	want := "User said: update please\n\n" + replyInstructionsBody
	if got != want {
		t.Errorf("composed result (check_messages):\ngot:  %q\nwant: %q", got, want)
	}
}

func TestComposedResultWithFiles(t *testing.T) {
	msgs := []UserMessage{{
		Text: "review this",
		Files: []FileRef{
			{Name: "main.go", Path: "/tmp/main.go", Type: "text/x-go", Size: 4096},
		},
	}}
	got := "User responded: " + FormatMessages(msgs) + "\n\n" + voiceSuffix(msgs)
	want := "User responded: review this\n\nAttached files:\n  /tmp/main.go (text/x-go, 4KB)\n\n" +
		replyInstructionsBody
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

func TestNextAvailableExportPath(t *testing.T) {
	dir := t.TempDir()

	first := nextAvailableExportPath(dir, "2026-04-25-test")
	if filepath.Base(first) != "2026-04-25-test.html" {
		t.Errorf("first call: got %q, want base 2026-04-25-test.html", first)
	}
	if err := os.WriteFile(first, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	second := nextAvailableExportPath(dir, "2026-04-25-test")
	if filepath.Base(second) != "2026-04-25-test-2.html" {
		t.Errorf("second call: got %q, want suffix -2", second)
	}
	if err := os.WriteFile(second, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	third := nextAvailableExportPath(dir, "2026-04-25-test")
	if filepath.Base(third) != "2026-04-25-test-3.html" {
		t.Errorf("third call: got %q, want suffix -3", third)
	}
}
