package main

import (
	"strings"
	"testing"
)

// Common expected substrings for reply instructions (shared across tests).
const (
	replyInstructionsBody = "Before taking any action\n" +
		"- present a brief summary of your understanding of the intent and task\n" +
		"- present a brief summary of your intended actions and why\n" +
		"- ask user some questions to verify user is lucid and understands the impact of the actions you're about to take (include a tricky question)\n" +
		"- once you are confident user understands, ask user to explicitly confirm yes to proceed\n" +
		"- send_progress before proceeding\n" +
		"\nWhen doing tasks\n" +
		"- call check_messages between steps.\n" +
		"\nUser cannot see TUI — never ask questions there. send_message to wait for further instructions. send_progress for non-blocking updates."

	replyInstructionsVoiceBody = "User can only hear you now; keep it conversational, no markdown.\n\n" +
		"Before taking any action\n" +
		"- present a brief summary of your understanding of the intent and task\n" +
		"- present a brief summary of your intended actions and why\n" +
		"- ask only ONE question at a time; wait for the user's response before asking the next question\n" +
		"- ask user some questions to verify user is lucid and understands the impact of the actions you're about to take (include a tricky question)\n" +
		"- once you are confident user understands, ask user to explicitly confirm yes to proceed\n" +
		"- send_verbal_progress before proceeding\n" +
		"\nWhen doing tasks\n" +
		"- call check_messages between steps.\n" +
		"\nUser cannot see TUI — never ask questions there. send_verbal_reply to wait for further instructions. send_verbal_progress for non-blocking updates."
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
	want := "Decoded user's speech to text (may be inaccurate): turn the box red\n\n" +
		"IMPORTANT: Confirm your understanding with the user before taking action. Present a brief summary of what you understood and ask the user to confirm yes or no before proceeding."
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
		"IMPORTANT: Confirm your understanding with the user before taking action. Present a brief summary of what you understood and ask the user to confirm yes or no before proceeding.\n\n" +
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
