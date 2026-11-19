package main

// Wrap safety (tasks/2026-11-17-mcp-apps-step1.md, "Wrap safety"): if swe-swe
// starts every tool server through `agent-chat wrap`, one crash in the
// wrapper's extras (capability edit, page tracking, delivery) must not take
// the tools down. wrapGuard catches such a crash, logs it once, and turns the
// extras off for the rest of the session: the wrapper is a plain relay from
// then on. Only the tool server failing to start stops the wrapper.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
)

// wrapPanicAt makes the extras panic at one spot ("client-hook",
// "server-hook", "deliver"). Tests only; always "" in real use.
var wrapPanicAt string

func wrapMaybePanic(where string) {
	if wrapPanicAt == where {
		panic("test panic at " + where)
	}
}

type wrapGuard struct {
	log  io.Writer
	off  atomic.Bool
	once sync.Once
}

// trip turns the extras off. Safe on a nil guard (it then only logs).
func (g *wrapGuard) trip(r any) {
	if g == nil {
		return
	}
	g.off.Store(true)
	g.once.Do(func() {
		fmt.Fprintf(g.log, "agent-chat wrap: extras crashed (%v); relaying only from now on\n", r)
	})
}

// catch is deferred (directly) by any extras goroutine.
func (g *wrapGuard) catch() {
	if r := recover(); r != nil {
		g.trip(r)
	}
}

// isOff reports whether the extras were turned off.
func (g *wrapGuard) isOff() bool { return g != nil && g.off.Load() }

// hook wraps h so that a crash forwards the line unchanged and turns the
// extras off. With ownReplies (server→client), replies to the wrapper's own
// requests are still dropped once off, so a late one never reaches the client.
func (g *wrapGuard) hook(h wrapHook, ownReplies bool) wrapHook {
	if h == nil {
		return nil
	}
	off := func(line []byte) wrapVerdict {
		if ownReplies && wrapIsOwnReply(line) {
			return wrapVerdict{drop: true}
		}
		return wrapVerdict{}
	}
	return func(line []byte) (v wrapVerdict) {
		if g.off.Load() {
			return off(line)
		}
		defer func() {
			if r := recover(); r != nil {
				g.trip(r)
				v = off(line)
			}
		}()
		v = h(line)
		if after := v.after; after != nil {
			v.after = func() {
				defer g.catch()
				after()
			}
		}
		return v
	}
}

// wrapIsOwnReply reports whether line is a reply to one of the wrapper's own
// requests ("acw-N" ids). Never panics.
func wrapIsOwnReply(line []byte) (own bool) {
	defer func() {
		if recover() != nil {
			own = false
		}
	}()
	if !bytes.Contains(line, []byte(`"`+wrapIDPrefix)) {
		return false
	}
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &m) != nil || m.Method != "" {
		return false
	}
	var id string
	return json.Unmarshal(m.ID, &id) == nil && strings.HasPrefix(id, wrapIDPrefix)
}
