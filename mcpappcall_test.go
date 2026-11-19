package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Step 2 phase 2 (tasks/2026-11-18-mcp-apps-step2-page-tools.md): agent-chat
// hands a page's tool request to the wrapper that showed the page, which
// waits for work on GET /api/mcp-app/calls.

// useMcpAppRelay gives the test a fresh relay with short timings.
func useMcpAppRelay(t *testing.T) {
	t.Helper()
	orig, origWait, origStale := mcpAppCalls, mcpAppPollWait, mcpAppPollStale
	mcpAppCalls = newMcpAppRelay()
	mcpAppPollWait = 300 * time.Millisecond
	mcpAppPollStale = 2 * time.Second
	t.Cleanup(func() { mcpAppCalls, mcpAppPollWait, mcpAppPollStale = orig, origWait, origStale })
}

func newMcpAppMux(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp-app", handleMcpApp)
	mux.HandleFunc(mcpAppPagePrefix, handleMcpAppPage)
	mux.HandleFunc(mcpAppCallsPath, handleMcpAppCalls)
	mux.HandleFunc(mcpAppCallsPath+"/", handleMcpAppCallResult)
	mux.HandleFunc(mcpAppCallPath, handleMcpAppCall)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// poll is one wrapper wait, as `agent-chat wrap` does it.
func poll(t *testing.T, srv *httptest.Server, wrapper, token string) (int, []mcpAppCallReq) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+mcpAppCallsPath+"?wrapper="+wrapper, nil)
	if token != "" {
		req.Header.Set(mcpAppTokenHeader, token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var calls []mcpAppCallReq
	if resp.StatusCode == 200 {
		json.NewDecoder(resp.Body).Decode(&calls)
	}
	return resp.StatusCode, calls
}

func answer(t *testing.T, srv *httptest.Server, callID, body string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+mcpAppCallsPath+"/"+callID, strings.NewReader(body))
	req.Header.Set(mcpAppTokenHeader, mcpAppToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// browserCall is the chat window's request to run a tool for a page.
func browserCall(t *testing.T, srv *httptest.Server, body string, hdr map[string]string) (int, map[string]json.RawMessage) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+mcpAppCallPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]json.RawMessage
	b, _ := io.ReadAll(resp.Body)
	json.Unmarshal(b, &out)
	return resp.StatusCode, out
}

func TestMcpAppCallRoutesToTheOwningWrapper(t *testing.T) {
	useMcpAppBase(t)
	useTestBus(t)
	useMcpAppRelay(t)
	srv := newMcpAppMux(t)
	tok := mcpAppToken()

	// Both wrappers have checked in once (so both count as running).
	poll(t, srv, "wA", tok)
	poll(t, srv, "wB", tok)

	type got struct {
		code int
		body map[string]json.RawMessage
	}
	done := make(chan got, 1)
	go func() {
		code, body := browserCall(t, srv, `{"wrapper":"wB","server":"cal","tool":"book","arguments":{"person":"Bob"}}`, nil)
		done <- got{code, body}
	}()

	// wA hears nothing; wB gets the call.
	var callsB []mcpAppCallReq
	bDone := make(chan struct{})
	go func() { _, callsB = poll(t, srv, "wB", tok); close(bDone) }()
	if _, callsA := poll(t, srv, "wA", tok); len(callsA) != 0 {
		t.Errorf("wA got %v", callsA)
	}
	<-bDone
	if len(callsB) != 1 || callsB[0].Tool != "book" || string(callsB[0].Arguments) != `{"person":"Bob"}` || callsB[0].ID == "" {
		t.Fatalf("wB got %+v", callsB)
	}
	if code := answer(t, srv, callsB[0].ID, `{"result":{"content":[{"type":"text","text":"booked"}]}}`); code != 200 {
		t.Fatalf("answer: HTTP %d", code)
	}
	select {
	case g := <-done:
		if g.code != 200 || !strings.Contains(string(g.body["result"]), "booked") {
			t.Errorf("browser got %d %v", g.code, g.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("browser never got the answer")
	}

	// Errors come back as errors.
	go func() {
		code, body := browserCall(t, srv, `{"wrapper":"wB","tool":"book"}`, nil)
		done <- got{code, body}
	}()
	_, calls := poll(t, srv, "wB", tok)
	for len(calls) == 0 {
		_, calls = poll(t, srv, "wB", tok)
	}
	answer(t, srv, calls[0].ID, `{"error":{"code":-32602,"message":"tool \"book\" can't be run from a page"}}`)
	g := <-done
	if g.code != 200 || !strings.Contains(string(g.body["error"]), "-32602") {
		t.Errorf("error answer: %d %v", g.code, g.body)
	}
}

func TestMcpAppCallWrapperNotRunning(t *testing.T) {
	useMcpAppBase(t)
	useMcpAppRelay(t)
	srv := newMcpAppMux(t)
	start := time.Now()
	code, body := browserCall(t, srv, `{"wrapper":"never-seen","tool":"book"}`, nil)
	if code != http.StatusServiceUnavailable || !strings.Contains(string(body["error"]), "not running") {
		t.Errorf("got %d %v", code, body)
	}
	if time.Since(start) > time.Second {
		t.Errorf("took %v, want an immediate answer", time.Since(start))
	}
	// Seen once, then silent for longer than mcpAppPollStale: not running.
	mcpAppPollStale = 50 * time.Millisecond
	poll(t, srv, "old", mcpAppToken())
	time.Sleep(100 * time.Millisecond)
	if code, _ := browserCall(t, srv, `{"wrapper":"old","tool":"book"}`, nil); code != http.StatusServiceUnavailable {
		t.Errorf("stale wrapper: HTTP %d, want 503", code)
	}
	if code, _ := browserCall(t, srv, `{"tool":"book"}`, nil); code != http.StatusServiceUnavailable {
		t.Errorf("no wrapper (page from an older wrap): HTTP %d, want 503", code)
	}
}

func TestMcpAppCallsNeedPasswordAndThisMachine(t *testing.T) {
	useMcpAppBase(t)
	useMcpAppRelay(t)
	srv := newMcpAppMux(t)
	if code, _ := poll(t, srv, "w", ""); code != http.StatusUnauthorized {
		t.Errorf("poll without password: HTTP %d", code)
	}
	if code, _ := poll(t, srv, "w", strings.Repeat("0", 64)); code != http.StatusUnauthorized {
		t.Errorf("poll with wrong password: HTTP %d", code)
	}
	req := httptest.NewRequest(http.MethodGet, mcpAppCallsPath+"?wrapper=w", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set(mcpAppTokenHeader, mcpAppToken())
	rec := httptest.NewRecorder()
	handleMcpAppCalls(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("poll from another machine: HTTP %d", rec.Code)
	}
	if code, _ := poll(t, srv, "", mcpAppToken()); code != http.StatusBadRequest {
		t.Errorf("poll without wrapper id: HTTP %d", code)
	}
	if code := answer(t, srv, "no-such-call", `{"result":{}}`); code != http.StatusNotFound {
		t.Errorf("answer for unknown call: HTTP %d", code)
	}
}

func TestMcpAppCallOnlyFromTheChatPage(t *testing.T) {
	useMcpAppBase(t)
	useMcpAppRelay(t)
	srv := newMcpAppMux(t)
	poll(t, srv, "w", mcpAppToken())
	if code, _ := browserCall(t, srv, `{"wrapper":"w","tool":"book"}`, map[string]string{"Sec-Fetch-Site": "cross-site"}); code != http.StatusForbidden {
		t.Errorf("cross-site: HTTP %d, want 403", code)
	}
	if code, _ := browserCall(t, srv, `{"wrapper":"w","tool":"book"}`, map[string]string{"Content-Type": "text/plain"}); code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain (no preflight): HTTP %d, want 415", code)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+mcpAppCallPath, nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: HTTP %d", resp.StatusCode)
	}
}

// The real chain, minus the browser: Claude-style client → wrap → fake tool
// server; the page goes to real agent-chat handlers, and a browser-style
// request runs a tool through the wrapper.
func TestWrapRunsPageToolCallsFromAgentChat(t *testing.T) {
	useMcpAppBase(t)
	events := useTestBus(t)
	useMcpAppRelay(t)
	srv := newMcpAppMux(t)
	if err := writeMcpAppToken(srv.Listener.Addr().(*net.TCPAddr).Port); err != nil {
		t.Fatal(err)
	}
	s := startWrapSession(t, "--name", "cal", "--agent-chat-url", srv.URL+"/api/mcp-app")
	s.handshake()
	s.request("tools/call", `{"name":"show"}`)
	var e Event
	select {
	case e = <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("no page event")
	}
	if e.App == nil || len(e.App.Wrapper) != 32 {
		t.Fatalf("page event should name its wrapper: %+v", e.App)
	}

	// The wrapper starts waiting for work after its first page.
	var code int
	var body map[string]json.RawMessage
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, body = browserCall(t, srv, fmt.Sprintf(`{"wrapper":%q,"server":"cal","tool":"plain","arguments":{"x":1}}`, e.App.Wrapper), nil)
		if code != http.StatusServiceUnavailable || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code != 200 || !strings.Contains(string(body["result"]), "ok plain") {
		t.Fatalf("page tool run: %d %s", code, mustJSON(body))
	}
	// Claude's side saw nothing of it (recv fails on the wrapper's own ids).
	s.request("ping", "")
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(bytes.TrimSpace(b))
}

func TestWrapStopsWaitingForWorkOnOldAgentChat(t *testing.T) {
	ac := newFakeAgentChat(t, nil) // answers 404 on /calls, like before step 2
	s := startWrapSession(t, "--agent-chat-url", ac.url())
	s.handshake()
	s.request("tools/call", `{"name":"show"}`)
	ac.wait(t, 1)
	waitStderr(t, s, "can't run page tools")
	time.Sleep(300 * time.Millisecond)
	if n := ac.pollCount(); n != 1 {
		t.Errorf("polled %d times after a 404, want 1", n)
	}
	s.request("ping", "")
}

func TestWrapRetriesWhenAgentChatFails(t *testing.T) {
	orig := wrapPollBackoffMin
	wrapPollBackoffMin = 20 * time.Millisecond
	t.Cleanup(func() { wrapPollBackoffMin = orig })
	ac := newFakeAgentChat(t, nil)
	ac.mu.Lock()
	ac.pollStatus = http.StatusInternalServerError
	ac.mu.Unlock()
	s := startWrapSession(t, "--agent-chat-url", ac.url())
	s.handshake()
	s.request("tools/call", `{"name":"show"}`)
	ac.wait(t, 1)
	deadline := time.Now().Add(3 * time.Second)
	for ac.pollCount() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("polled %d times, want retries", ac.pollCount())
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.request("ping", "") // the relay is untouched
}
