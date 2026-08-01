package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withWorkspacePathRoot points the annotator at a throwaway workspace for the
// duration of one test. Empty root is the feature-off default, so every test
// that does not call this sees no annotation at all.
func withWorkspacePathRoot(t *testing.T, root string) {
	t.Helper()
	prev := workspacePathRoot
	workspacePathRoot = root
	t.Cleanup(func() { workspacePathRoot = prev })
}

func TestPublishAnnotatesFilePaths(t *testing.T) {
	root := newPathsWorkspace(t)
	withWorkspacePathRoot(t, root)

	eb := NewEventBus()
	eb.Publish(Event{Type: "agentMessage", Text: "the rule is in client-dist/app.js next to docs/adr"})

	events := eb.EventsSince(0)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	got := paths(events[0].FilePaths)
	want := []string{"client-dist/app.js", "docs/adr(dir)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("FilePaths = %v, want %v", got, want)
	}
}

func TestPublishFilePathsOffWithoutRoot(t *testing.T) {
	withWorkspacePathRoot(t, "")

	eb := NewEventBus()
	eb.Publish(Event{Type: "agentMessage", Text: "the rule is in client-dist/app.js"})

	events := eb.EventsSince(0)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if len(events[0].FilePaths) != 0 {
		t.Errorf("FilePaths = %v, want none when the root is unset", paths(events[0].FilePaths))
	}
}

// The annotation is a rendering aid, not a fact about the conversation. It must
// never reach the JSONL: the archive records what was said, and a stored answer
// would be wrong the moment the file moved.
func TestFilePathsNeverReachTheEventLog(t *testing.T) {
	root := newPathsWorkspace(t)
	withWorkspacePathRoot(t, root)

	logPath := filepath.Join(t.TempDir(), "events.jsonl")
	eb, err := NewEventBusWithLog(logPath)
	if err != nil {
		t.Fatalf("NewEventBusWithLog: %v", err)
	}
	eb.Publish(Event{Type: "agentMessage", Text: "see client-dist/app.js"})
	eb.LogUserMessage("also @Makefile", nil)
	eb.Close()

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(raw), "file_paths") {
		t.Errorf("event log contains file_paths:\n%s", raw)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		if len(ev.FilePaths) != 0 {
			t.Errorf("logged event carries FilePaths: %v", paths(ev.FilePaths))
		}
	}
}

func TestLogUserMessageAnnotatesFilePaths(t *testing.T) {
	root := newPathsWorkspace(t)
	withWorkspacePathRoot(t, root)

	eb := NewEventBus()
	eb.LogUserMessage("look at @Makefile please", nil)

	// LogUserMessage appends without a Seq, so EventsSince(0) skips it —
	// History is the accessor that sees the whole log.
	events, _ := eb.History()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	want := []string{"Makefile"}
	if got := paths(events[0].FilePaths); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("FilePaths = %v, want %v", got, want)
	}
}

// A restart rebuilds the in-memory log from the JSONL. Annotating there is what
// lets bubbles written before this feature existed become clickable, with no
// backfill of the archive.
func TestRestoredHistoryIsAnnotated(t *testing.T) {
	root := newPathsWorkspace(t)
	logPath := filepath.Join(t.TempDir(), "events.jsonl")

	withWorkspacePathRoot(t, "")
	first, err := NewEventBusWithLog(logPath)
	if err != nil {
		t.Fatalf("NewEventBusWithLog: %v", err)
	}
	first.Publish(Event{Type: "agentMessage", Text: "written before the feature: client-dist/style.css"})
	first.Close()

	withWorkspacePathRoot(t, root)
	second, err := NewEventBusWithLog(logPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	events := second.EventsSince(0)
	if len(events) != 1 {
		t.Fatalf("got %d restored events, want 1", len(events))
	}
	want := []string{"client-dist/style.css"}
	if got := paths(events[0].FilePaths); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("restored FilePaths = %v, want %v", got, want)
	}
}

// Proves the work happens once, at write time, rather than on every read: a
// file created after the bubble was published is not picked up by a later read
// of the same bubble.
func TestFilePathsComputedAtWriteNotAtRead(t *testing.T) {
	root := newPathsWorkspace(t)
	withWorkspacePathRoot(t, root)

	eb := NewEventBus()
	eb.Publish(Event{Type: "agentMessage", Text: "see docs/later.md"})

	if got := eb.EventsSince(0)[0].FilePaths; len(got) != 0 {
		t.Fatalf("FilePaths = %v, want none before the file exists", paths(got))
	}

	if err := os.WriteFile(filepath.Join(root, "docs/later.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := eb.EventsSince(0)[0].FilePaths; len(got) != 0 {
		t.Errorf("FilePaths = %v — a read recomputed the answer", paths(got))
	}
}
