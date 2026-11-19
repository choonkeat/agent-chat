package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Wrap safety (tasks/2026-11-17-mcp-apps-step1.md, "Wrap safety"): swe-swe
// checks for `wrap` without side effects, and a crash in the wrapper's extras
// never stops the relay.

func TestWrapVersion(t *testing.T) {
	var out, errb bytes.Buffer
	code := runWrap([]string{"--version"}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
	if want := fmt.Sprintf("agent-chat wrap %s (%s)\n", version, commit); out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
}

func TestMainUsageMentionsWrap(t *testing.T) {
	var b bytes.Buffer
	mainUsage(&b)
	if !strings.Contains(b.String(), "agent-chat wrap") {
		t.Errorf("top-level -h must mention `agent-chat wrap` (swe-swe's safe check):\n%s", b.String())
	}
}

// usePanicAt makes the wrapper's extras panic at one spot.
func usePanicAt(t *testing.T, where string) {
	t.Helper()
	orig := wrapPanicAt
	wrapPanicAt = where
	t.Cleanup(func() { wrapPanicAt = orig })
}

func waitStderr(t *testing.T, s *wrapSession, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(s.stderr.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("stderr never said %q:\n%s", want, s.stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWrapSurvivesCrashInExtras(t *testing.T) {
	for _, where := range []string{"client-hook", "server-hook", "deliver"} {
		t.Run(where, func(t *testing.T) {
			usePanicAt(t, where)
			ac := newFakeAgentChat(t, nil)
			s := startWrapSession(t, "--agent-chat-url", ac.url())
			s.handshake()
			reply := s.request("tools/call", `{"name":"show"}`)
			if !strings.Contains(fmt.Sprint(reply["result"]), "ok show") {
				t.Fatalf("tool answer = %v", reply)
			}
			waitStderr(t, s, "relaying only")
			// Traffic keeps flowing after the extras are off.
			s.request("ping", "")
			s.request("tools/call", `{"name":"plain"}`)
			if strings.Count(s.stderr.String(), "relaying only") != 1 {
				t.Errorf("crash logged more than once:\n%s", s.stderr.String())
			}
		})
	}
}

func TestWrapGuardDropsOwnRepliesAfterTurningOff(t *testing.T) {
	var log bytes.Buffer
	g := &wrapGuard{log: &log}
	calls := 0
	hook := g.hook(func(line []byte) wrapVerdict {
		calls++
		panic("boom")
	}, true)
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":"acw-7","result":{"late":true}}`,
		`{"jsonrpc":"2.0","id":2,"result":{}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := wrapRelay(strings.NewReader(in), &wrapLineWriter{w: &out}, hook); err != nil {
		t.Fatal(err)
	}
	want := `{"jsonrpc":"2.0","id":1,"result":{}}` + "\n" + `{"jsonrpc":"2.0","id":2,"result":{}}` + "\n"
	if out.String() != want {
		t.Errorf("relayed %q, want %q", out.String(), want)
	}
	if calls != 1 {
		t.Errorf("hook ran %d times after crashing, want 1", calls)
	}
	if !strings.Contains(log.String(), "relaying only") {
		t.Errorf("log = %q", log.String())
	}
	// A crash inside an after func is caught too.
	g2 := &wrapGuard{log: &log}
	v := g2.hook(func([]byte) wrapVerdict { return wrapVerdict{after: func() { panic("late boom") }} }, false)([]byte("{}"))
	v.after()
	if !g2.off.Load() {
		t.Errorf("a crash in after should turn the extras off")
	}
}
