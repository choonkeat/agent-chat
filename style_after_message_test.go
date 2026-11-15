package main

import (
	"regexp"
	"strings"
	"testing"
)

// Regression for the bug seen live 2026-11-15 (task
// 2026-11-15-ship-tagged-message-wrapping): 0.19.0 pasted the Message-style
// preset ABOVE the user's words with only a blank line between them:
//
//	User said: Reply for a reader with ADHD: ... closers.
//
//	Discuss how do we support this in swe-swe? ...
//
// The style ended in an instruction and the message began "Discuss how do we
// support this", so the agent read "this" as the style and answered the wrong
// question. Now the user's words follow "User said:" directly and the style
// comes once, after them, labelled as not part of the message.

const (
	glueStyle  = "Reply for a reader with ADHD: lead with the next action, number multi-step work, end with one concrete next step, cut all preamble, recap, and closers."
	glueText   = "Discuss how do we support this in swe-swe? Through agent-chat? When calling\nsupporting mcp?"
	styleLabel = "(Reply style your user set in settings, not part of their message:) "
)

// checkStyleAfter asserts out begins with anchor + the texts (blank-line
// separated) and carries the labelled style exactly once, after all of them.
func checkStyleAfter(t *testing.T, out, anchor string, texts []string, style string) {
	t.Helper()
	head := anchor + strings.Join(texts, "\n\n")
	if !strings.HasPrefix(out, head) {
		t.Fatalf("want delivery to start with %q, got:\n%s", head, out)
	}
	if n := strings.Count(out, style); n != 1 {
		t.Errorf("style text appears %d times, want 1:\n%s", n, out)
	}
	at := strings.Index(out, styleLabel+style)
	if at < len(head) {
		t.Errorf("want %q after the message, got:\n%s", styleLabel+style, out)
	}
}

func TestStyleComesAfterMessage(t *testing.T) {
	msgs := []UserMessage{{Text: glueText, Template: glueStyle}}
	t.Run("check_messages", func(t *testing.T) {
		checkStyleAfter(t, composeCheckMessagesResult(nil, msgs), "User said: ", []string{glueText}, glueStyle)
	})
	t.Run("send_message reply", func(t *testing.T) {
		checkStyleAfter(t, userRespondedText(msgs), "User responded: ", []string{glueText}, glueStyle)
	})
}

func TestStyleComesAfterMessageWithAttachment(t *testing.T) {
	msgs := []UserMessage{{Text: glueText, Template: glueStyle, Files: []FileRef{{Path: "/tmp/pasted-207-lines.txt", Type: "text/plain", Size: 2048}}}}
	out := composeCheckMessagesResult(nil, msgs)
	checkStyleAfter(t, out, "User said: ", []string{glueText}, glueStyle)
	if f, s := strings.Index(out, "Attached files:"), strings.Index(out, styleLabel); f < 0 || s < f {
		t.Errorf("want the attachment listed with the message, before the style:\n%s", out)
	}
}

func TestStyleShownOnceAfterBatch(t *testing.T) {
	second := "this one too: what about the tool list?"
	msgs := []UserMessage{{Text: glueText, Template: glueStyle}, {Text: second, Template: glueStyle}}
	checkStyleAfter(t, composeCheckMessagesResult(nil, msgs), "User said: ", []string{glueText, second}, glueStyle)
}

// A "{{message}}" template (saved by older clients) must not wrap the words
// either: the marker is dropped and the rest is the style, as the client
// itself does (MSG_STYLE_LEGACY_MARK in client-dist/app.js).
func TestPlaceholderStyleComesAfterMessage(t *testing.T) {
	for _, tmpl := range []string{glueStyle + "\n\n{{message}}", "{{message}}\n\n" + glueStyle} {
		out := composeCheckMessagesResult(nil, []UserMessage{{Text: glueText, Template: tmpl}})
		if strings.Contains(out, "{{message}}") {
			t.Errorf("placeholder marker leaked:\n%s", out)
		}
		checkStyleAfter(t, out, "User said: ", []string{glueText}, glueStyle)
	}
}

func TestPlaceholderOnlyTemplateHasNoStyle(t *testing.T) {
	out := composeCheckMessagesResult(nil, []UserMessage{{Text: glueText, Template: " {{message}} "}})
	if !strings.HasPrefix(out, "User said: "+glueText+"\n\n") || strings.Contains(out, styleLabel) {
		t.Errorf("want the bare message and no style line:\n%s", out)
	}
}

func TestNoStyleNoLabel(t *testing.T) {
	out := composeCheckMessagesResult(nil, []UserMessage{{Text: glueText}})
	if strings.Contains(out, styleLabel) {
		t.Errorf("no style set, yet the style label appears:\n%s", out)
	}
}

// swe-swe's Stop hook (cmd/swe-swe/hook-scripts/swe-swe-stop-guard.sh in the
// swe-swe repo) reads agent-chat's tool results. swe-swe runs
// `swe-npx -y @choonkeat/agent-chat`, so every installed swe-swe gets the
// newest agent-chat; these strings must keep matching guards already deployed.
//
//   - Turn start: awk /User said:|User responded:/ (releases 75bd769a7..2.43.2).
//   - Empty queue is an allowed silent turn: grep 'queue.\{0,8\}empty'
//     (since 88bbed485).
func TestDeliveriesMatchReleasedSweSweStopGuard(t *testing.T) {
	msgs := []UserMessage{{Text: glueText, Template: glueStyle}}
	turnStart := regexp.MustCompile(`User said:|User responded:`)
	for name, out := range map[string]string{
		"check_messages": composeCheckMessagesResult(nil, msgs),
		"reply":          userRespondedText(msgs),
	} {
		if !turnStart.MatchString(out) {
			t.Errorf("%s: no turn-start anchor for the swe-swe stop guard:\n%s", name, out)
		}
	}
	if out := composeCheckMessagesResult(nil, nil); !regexp.MustCompile(`queue.{0,8}empty`).MatchString(out) {
		t.Errorf("empty queue %q does not match the swe-swe stop guard's queue.{0,8}empty", out)
	}
}
