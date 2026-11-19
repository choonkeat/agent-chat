package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Step 2 phase 4 (tasks/2026-11-18-mcp-apps-step2-page-tools.md): after a page
// runs a tool, a line shows in the chat and Claude gets it with the user's
// next message. It never wakes Claude by itself.

func TestPageNoteRidesWithTheNextMessage(t *testing.T) {
	resetGuidance(t)
	b := NewEventBus()
	b.AddAgentNote("📱 The `cal` page ran `book_slot` (person: Bob): Pencilled in")
	b.ReceiveUserMessage("what changed?", nil, "")
	got, err := waitForUserReply(context.Background(), b, "send_message", 1)
	if err != nil {
		t.Fatal(err)
	}
	note := strings.Index(got, "Meanwhile, from tool pages:\n- 📱 The `cal` page ran `book_slot` (person: Bob): Pencilled in")
	msg := strings.Index(got, "what changed?")
	if note < 0 || msg < 0 || note > msg {
		t.Errorf("want the note before the message:\n%s", got)
	}
}

func TestPageNotesArriveOnceEach(t *testing.T) {
	b := NewEventBus()
	b.AddAgentNote("note one")
	b.AddAgentNote("note two")
	b.ReceiveUserMessage("first", nil, "")
	first := FormatMessages(b.DrainMessages())
	if strings.Count(first, "note one") != 1 || strings.Count(first, "note two") != 1 {
		t.Errorf("first delivery:\n%s", first)
	}
	b.ReceiveUserMessage("second", nil, "")
	if second := FormatMessages(b.DrainMessages()); strings.Contains(second, "note") || strings.Contains(second, "Meanwhile") {
		t.Errorf("second delivery repeats notes:\n%s", second)
	}
	// A redelivery of the first batch (limbo) still carries its notes.
	b.ReceiveUserMessage("third", nil, "")
	b.DrainMessages()
	b.AddAgentNote("late note")
	b.ReceiveUserMessage("fourth", nil, "")
	b.DrainMessages()
	if l := FormatMessages(b.Limbo()); !strings.Contains(l, "late note") {
		t.Errorf("limbo lost its note:\n%s", l)
	}
}

func TestPageNoteDoesNotWakeClaude(t *testing.T) {
	b := NewEventBus()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		b.AddAgentNote("📱 page ran a tool")
	}()
	if msgs, err := b.WaitForMessages(ctx); err == nil {
		t.Fatalf("a note ended the wait: %v", msgs)
	}
	if b.HasQueuedMessages() {
		t.Errorf("a note counts as a queued message")
	}
	if msgs := b.DrainMessages(); len(msgs) != 0 {
		t.Errorf("an empty drain delivered %v", msgs)
	}
	// Still waiting for the next real message.
	b.ReceiveUserMessage("hi", nil, "")
	if got := FormatMessages(b.DrainMessages()); !strings.Contains(got, "page ran a tool") {
		t.Errorf("note was lost by the empty drain:\n%s", got)
	}
}

func TestPageToolRunPublishesLineAndNote(t *testing.T) {
	useMcpAppBase(t)
	events := useTestBus(t)
	useMcpAppRelay(t)
	srv := newMcpAppMux(t)
	tok := mcpAppToken()
	poll(t, srv, "w", tok)

	long := strings.Repeat("x", 400)
	go func() {
		for {
			_, calls := poll(t, srv, "w", tok)
			for _, c := range calls {
				if c.Tool == "book_slot" {
					answer(t, srv, c.ID, `{"result":{"content":[{"type":"text","text":"Pencilled in Bob. `+long+`"}]}}`)
				} else {
					answer(t, srv, c.ID, `{"error":{"code":-32602,"message":"tool \"secret\" can't be run from a page"}}`)
				}
				if c.Tool == "secret" {
					return
				}
			}
		}
	}()

	browserCall(t, srv, `{"wrapper":"w","server":"cal","tool":"book_slot","arguments":{"person":"Bob","day":"Tue 24"}}`, nil)
	var e Event
	select {
	case e = <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("no mcpAppCall event")
	}
	if e.Type != "mcpAppCall" || e.AppCall == nil {
		t.Fatalf("event = %+v", e)
	}
	c := e.AppCall
	if c.Server != "cal" || c.Tool != "book_slot" || c.IsError {
		t.Errorf("call = %+v", c)
	}
	if !strings.HasPrefix(c.Summary, "Pencilled in Bob.") || len([]rune(c.Summary)) > 301 || !strings.HasSuffix(c.Summary, "…") {
		t.Errorf("summary not cut to ~300 characters: %d %q", len([]rune(c.Summary)), c.Summary)
	}
	if got, want := mcpAppCallLine(c), "📱 `cal` page ran `book_slot` (day: Tue 24, person: Bob): "+c.Summary; got != want {
		t.Errorf("line:\ngot  %q\nwant %q", got, want)
	}

	// A tool the wrapper refused never ran: no line, no note.
	browserCall(t, srv, `{"wrapper":"w","server":"cal","tool":"secret"}`, nil)
	select {
	case e := <-events:
		t.Errorf("refused run published %+v", e)
	case <-time.After(200 * time.Millisecond):
	}

	bus.ReceiveUserMessage("what changed?", nil, "")
	got := FormatMessages(bus.DrainMessages())
	if strings.Count(got, "page ran `book_slot`") != 1 || strings.Contains(got, "secret") {
		t.Errorf("Claude's note:\n%s", got)
	}
}

func TestPageToolRunChatLogLine(t *testing.T) {
	st := &renderState{}
	e := Event{Type: "mcpAppCall", Timestamp: 5000, AppCall: &McpAppCall{
		Server: "cal", Tool: "book_slot", Arguments: []byte(`{"person":"Bob","day":"Tue 24"}`), Summary: "Pencilled in",
	}}
	want := "**USER**\n\n> 📱 `cal` page ran `book_slot` (day: Tue 24, person: Bob): Pencilled in\n\n"
	if got := renderChatBubble(e, st, nil); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if renderChatBubble(Event{Type: "mcpAppCall"}, &renderState{}, nil) != "" {
		t.Errorf("an mcpAppCall event without a call should render nothing")
	}
	errLine := mcpAppCallLine(&McpAppCall{Server: "cal", Tool: "book_slot", Summary: "already pending", IsError: true})
	if errLine != "📱 `cal` page ran `book_slot`: error: already pending" {
		t.Errorf("error line = %q", errLine)
	}
}
