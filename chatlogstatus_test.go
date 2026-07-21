package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An external orchestrator (the swe-swe server) needs to answer "does this
// session have a chat log, and where is it?" before it can offer to discard or
// commit it at end-of-session. It cannot work that out from the filesystem:
// an untitled export carries the host SESSION_UUID in its filename, but
// set_chat_title renames it, and the `session:` header is a hash of the event
// log path -- not the swe-swe session UUID. Only the stream itself knows.
func TestStatusReportsPathAndTitledState(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-status", "host-uuid-1", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}

	st := s.Status()
	if !st.Enabled {
		t.Error("a live stream must report enabled")
	}
	if st.Titled {
		t.Error("a fresh export is untitled")
	}
	if st.Stopped || st.OptedOut {
		t.Error("a fresh export is neither stopped nor opted out")
	}
	if st.Path == "" {
		t.Fatal("status must report the .md path")
	}
	if _, err := os.Stat(st.Path); err != nil {
		t.Errorf("reported path must exist on disk: %v", err)
	}
	if got := filepath.Base(st.Path); got != "2026-07-21-01-untitled-host-uuid-1.md" {
		t.Errorf("path = %q, want the provisional untitled name carrying the host session uuid", got)
	}

	// Titling renames the file; status must follow it, which is the whole
	// reason an orchestrator cannot cache the path.
	if err := s.SetTitle("Auth Bug Fix", nil); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}
	st = s.Status()
	if !st.Titled {
		t.Error("after SetTitle the export is titled")
	}
	if st.Slug != "auth-bug-fix" {
		t.Errorf("slug = %q, want auth-bug-fix", st.Slug)
	}
	if got := filepath.Base(st.Path); got != "2026-07-21-01-auth-bug-fix.md" {
		t.Errorf("path = %q, want the renamed file", got)
	}
	if _, err := os.Stat(st.Path); err != nil {
		t.Errorf("renamed path must exist: %v", err)
	}
}

// After opt-out the .md is gone. Status must say so rather than hand back a
// path that no longer exists -- an orchestrator offering to "discard" a log
// that is already discarded is a confusing dead end.
func TestStatusAfterOptout(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-optout", "host-uuid-2", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}
	if err := s.Optout(); err != nil {
		t.Fatalf("Optout: %v", err)
	}

	st := s.Status()
	if !st.OptedOut {
		t.Error("status must report optedOut after Optout")
	}
	if !st.Stopped {
		t.Error("opt-out also stops the stream")
	}
	if st.Exists {
		t.Error("the .md was deleted -- status must not claim it exists")
	}
}

// chatlog_close freezes the file but keeps it on disk: an orchestrator must be
// able to tell "frozen, ready to commit" apart from "deleted".
func TestStatusAfterClose(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	s, err := newChatLogStream(dir, "sess-close", "host-uuid-3", "claude", "v1 (abc)", nil, now)
	if err != nil {
		t.Fatalf("newChatLogStream: %v", err)
	}
	if _, err := s.CloseOut("Some Title", nil); err != nil {
		t.Fatalf("CloseOut: %v", err)
	}

	st := s.Status()
	if !st.Stopped {
		t.Error("a closed stream is stopped")
	}
	if st.OptedOut {
		t.Error("closing is not opting out")
	}
	if !st.Exists {
		t.Error("the frozen .md is still on disk and still committable")
	}
	if !st.Titled {
		t.Error("CloseOut titled it")
	}
}

// The feature-off case: no AGENT_CHAT_EXPORT_DIR means no stream at all, and
// the orchestrator must get a clean "nothing here" rather than a nil deref.
func TestStatusWhenExportDisabled(t *testing.T) {
	var s *chatLogStream
	st := s.Status()
	if st.Enabled || st.Exists || st.Path != "" {
		t.Errorf("a nil stream must report a zero status, got %+v", st)
	}
}
