package main

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// Step 2 phase 1 (tasks/2026-11-18-mcp-apps-step2-page-tools.md): the wrapper
// runs a tool for a page, under its own id, and the reply never reaches the
// client.

// callHarness is a wrapApps whose "server" is respond: every line the wrapper
// writes to the server is parsed and, if respond returns a line, that line is
// fed back through serverHook. Lines the hook doesn't drop are recorded as
// reaching the client.
type callHarness struct {
	a        *wrapApps
	mu       sync.Mutex
	sent     []wrapMsg // what the wrapper sent to the server
	toClient []string  // server lines the hook let through
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }

func newCallHarness(t *testing.T, tools string, respond func(m wrapMsg) string) *callHarness {
	t.Helper()
	h := &callHarness{}
	h.a = newWrapApps("cal", "http://unused", &wrapLineWriter{w: writerFunc(func(b []byte) (int, error) {
		m, _ := parseWrapMsg(b)
		h.mu.Lock()
		h.sent = append(h.sent, m)
		h.mu.Unlock()
		if respond != nil {
			if reply := respond(m); reply != "" {
				go h.fromServer(reply)
			}
		}
		return len(b), nil
	})}, &syncBuf{})
	h.a.mu.Lock()
	h.a.learnTools(json.RawMessage(tools), true)
	h.a.mu.Unlock()
	return h
}

func (h *callHarness) fromServer(line string) {
	if v := h.a.serverHook([]byte(line)); !v.drop {
		h.mu.Lock()
		h.toClient = append(h.toClient, line)
		h.mu.Unlock()
	}
}

func (h *callHarness) sentCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sent)
}

func (h *callHarness) leaked() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.toClient...)
}

const calTools = `{"tools":[
	{"name":"show","_meta":{"ui":{"resourceUri":"ui://cal/page.html"}}},
	{"name":"book","_meta":{"ui":{"visibility":["model","app"]}}},
	{"name":"plain"},
	{"name":"secret","_meta":{"ui":{"visibility":["model"]}}},
	{"name":"apponly","_meta":{"ui":{"visibility":["app"]}}}
]}`

func echoResult(m wrapMsg) string {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	json.Unmarshal(m.Params, &p)
	res, _ := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": "ran " + p.Name + " " + string(p.Arguments)}}})
	return `{"jsonrpc":"2.0","id":` + string(m.ID) + `,"result":` + string(res) + `}`
}

func TestWrapCallToolRunsAppVisibleTools(t *testing.T) {
	h := newCallHarness(t, calTools, func(m wrapMsg) string {
		if m.Method != "tools/call" {
			t.Errorf("wrapper sent %q", m.Method)
		}
		return echoResult(m)
	})
	for _, name := range []string{"book", "plain", "show", "apponly"} {
		res, err := h.a.callTool(name, json.RawMessage(`{"person":"Bob"}`))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(string(res), `ran `+name+` {\"person\":\"Bob\"}`) {
			t.Errorf("%s: result = %s", name, res)
		}
	}
	if l := h.leaked(); len(l) != 0 {
		t.Errorf("replies reached the client: %v", l)
	}
	h.mu.Lock()
	id := string(h.sent[0].ID)
	h.mu.Unlock()
	if !strings.HasPrefix(id, `"`+wrapIDPrefix) {
		t.Errorf("tools/call id = %s, want the wrapper's own %q prefix", id, wrapIDPrefix)
	}
}

func TestWrapCallToolRefusesModelOnlyAndUnknownTools(t *testing.T) {
	h := newCallHarness(t, calTools, echoResult)
	for _, name := range []string{"secret", "nope", ""} {
		_, err := h.a.callTool(name, nil)
		var ce *wrapCallError
		if !errors.As(err, &ce) || ce.Code != -32602 {
			t.Errorf("%q: err = %v, want a -32602 refusal", name, err)
		}
	}
	if n := h.sentCount(); n != 0 {
		t.Errorf("the server saw %d requests for refused tools", n)
	}
}

func TestWrapCallToolTimesOutAndSwallowsTheLateReply(t *testing.T) {
	orig := wrapCallTimeout
	wrapCallTimeout = 100 * time.Millisecond
	t.Cleanup(func() { wrapCallTimeout = orig })

	var late string
	h := newCallHarness(t, calTools, func(m wrapMsg) string { late = echoResult(m); return "" })
	_, err := h.a.callTool("book", nil)
	var ce *wrapCallError
	if !errors.As(err, &ce) || !strings.Contains(ce.Message, "too long") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	h.fromServer(late)
	if l := h.leaked(); len(l) != 0 {
		t.Errorf("late reply reached the client: %v", l)
	}
}

func TestWrapCallToolPassesErrorsBack(t *testing.T) {
	h := newCallHarness(t, calTools, func(m wrapMsg) string {
		return `{"jsonrpc":"2.0","id":` + string(m.ID) + `,"error":{"code":-32603,"message":"db down"}}`
	})
	_, err := h.a.callTool("book", nil)
	var ce *wrapCallError
	if !errors.As(err, &ce) || ce.Code != -32603 || ce.Message != "db down" {
		t.Errorf("err = %v, want the server's own error", err)
	}

	// isError results are ordinary results: the page shows them.
	h2 := newCallHarness(t, calTools, func(m wrapMsg) string {
		return `{"jsonrpc":"2.0","id":` + string(m.ID) + `,"result":{"isError":true,"content":[{"type":"text","text":"taken"}]}}`
	})
	res, err := h2.a.callTool("book", nil)
	if err != nil || !strings.Contains(string(res), `"isError":true`) {
		t.Errorf("isError result = %s, %v", res, err)
	}
}

func TestWrapCallToolFollowsToolListChanges(t *testing.T) {
	h := newCallHarness(t, calTools, echoResult)
	h.fromServer(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`)
	if _, err := h.a.callTool("book", nil); err == nil {
		t.Errorf("after list_changed, tools are unknown until the next tools/list")
	}
	h.a.mu.Lock()
	h.a.learnTools(json.RawMessage(`{"tools":[{"name":"book","_meta":{"ui":{"visibility":["model"]}}},{"name":"newone"}]}`), true)
	h.a.mu.Unlock()
	if _, err := h.a.callTool("book", nil); err == nil {
		t.Errorf("book is now model-only and must be refused")
	}
	if _, err := h.a.callTool("newone", nil); err != nil {
		t.Errorf("newone: %v", err)
	}
}

func TestWrapCallToolAfterShutdown(t *testing.T) {
	h := newCallHarness(t, calTools, func(wrapMsg) string { return "" })
	done := make(chan error, 1)
	go func() { _, err := h.a.callTool("book", nil); done <- err }()
	time.Sleep(50 * time.Millisecond)
	h.a.shutdown(time.Second)
	select {
	case err := <-done:
		var ce *wrapCallError
		if !errors.As(err, &ce) || !strings.Contains(ce.Message, "not running") {
			t.Errorf("err = %v, want 'not running'", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callTool still waiting after shutdown")
	}
	if _, err := h.a.callTool("book", nil); err == nil {
		t.Errorf("a call after shutdown should fail at once")
	}
}
