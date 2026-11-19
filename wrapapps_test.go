package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAppsServer is the child for the page-delivery tests (TestMain runs it
// when WRAP_FAKE_CHILD=apps). It is a minimal MCP server with three tools:
//
//	show   — has a page (ui://t/page.html, via _meta.ui.resourceUri)
//	legacy — has a page via the deprecated _meta["ui/resourceUri"]
//	plain  — no page
//
// and logs "READ <uri>" to stderr for every resources/read it serves.
// A tool called "fail" answers with isError.
func fakeAppsServer() int {
	out := json.NewEncoder(os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var m wrapMsg
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.Method == "" || len(m.ID) == 0 {
			continue
		}
		reply := func(result any) {
			out.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		}
		switch m.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}}})
		case "tools/list":
			reply(map[string]any{"tools": []any{
				map[string]any{"name": "show", "inputSchema": map[string]any{"type": "object"},
					"_meta": map[string]any{"ui": map[string]any{"resourceUri": "ui://t/page.html", "visibility": []string{"model", "app"}}}},
				map[string]any{"name": "legacy", "inputSchema": map[string]any{"type": "object"},
					"_meta": map[string]any{"ui/resourceUri": "ui://t/legacy.html"}},
				map[string]any{"name": "plain", "inputSchema": map[string]any{"type": "object"}},
				map[string]any{"name": "fail", "inputSchema": map[string]any{"type": "object"},
					"_meta": map[string]any{"ui": map[string]any{"resourceUri": "ui://t/page.html"}}},
			}})
		case "tools/call":
			var p struct {
				Name string `json:"name"`
			}
			json.Unmarshal(m.Params, &p)
			reply(map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "ok " + p.Name}},
				"isError": p.Name == "fail",
			})
		case "resources/read":
			var p struct {
				URI string `json:"uri"`
			}
			json.Unmarshal(m.Params, &p)
			fmt.Fprintf(os.Stderr, "READ %s\n", p.URI)
			reply(map[string]any{"contents": []any{map[string]any{
				"uri": p.URI, "mimeType": wrapUIMimeType,
				"text":  "<html>PAGE " + p.URI + "</html>",
				"_meta": map[string]any{"ui": map[string]any{"prefersBorder": true}},
			}}})
		case "ping":
			reply(map[string]any{})
		}
	}
	return 0
}

// syncBuf is a goroutine-safe buffer (the child's stderr and the wrapper's
// own log both write to it).
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// wrapSession drives `agent-chat wrap -- <fakeAppsServer>` as Claude Code would.
type wrapSession struct {
	t      *testing.T
	in     *io.PipeWriter
	lines  chan string
	stderr *syncBuf
	exit   chan int
	nextID int
}

func startWrapSession(t *testing.T, flags ...string) *wrapSession {
	t.Helper()
	t.Setenv("WRAP_FAKE_CHILD", "apps")
	return startWrap(t, append(append([]string{}, flags...), "--", os.Args[0]))
}

// startWrap runs `agent-chat wrap <args>` in-process with piped stdio.
func startWrap(t *testing.T, args []string) *wrapSession {
	t.Helper()
	t.Setenv("AGENT_CHAT_PORT", "")
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &wrapSession{t: t, in: inW, lines: make(chan string, 100), stderr: &syncBuf{}, exit: make(chan int, 1)}
	go func() {
		code := runWrap(args, inR, outW, s.stderr)
		outW.Close()
		s.exit <- code
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
	}()
	t.Cleanup(func() {
		inW.Close()
		select {
		case <-s.exit:
		case <-time.After(5 * time.Second):
			t.Errorf("wrapper did not exit after stdin closed")
		}
		t.Logf("stderr:\n%s", s.stderr.String())
	})
	return s
}

func (s *wrapSession) send(line string) {
	s.t.Helper()
	if _, err := io.WriteString(s.in, line+"\n"); err != nil {
		s.t.Fatalf("send: %v", err)
	}
}

// recv returns the next line the client gets, failing on timeout or any
// reply to the wrapper's own requests.
func (s *wrapSession) recv() map[string]any {
	s.t.Helper()
	select {
	case line, ok := <-s.lines:
		if !ok {
			s.t.Fatalf("wrapper output closed")
		}
		if strings.Contains(line, wrapIDPrefix) {
			s.t.Fatalf("the wrapper's own traffic reached the client: %s", line)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			s.t.Fatalf("bad line %q: %v", line, err)
		}
		return m
	case <-time.After(5 * time.Second):
		s.t.Fatalf("timed out waiting for the server's reply")
	}
	return nil
}

// request sends a request and returns its reply.
func (s *wrapSession) request(method, params string) map[string]any {
	s.t.Helper()
	s.nextID++
	if params == "" {
		params = "{}"
	}
	s.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, s.nextID, method, params))
	m := s.recv()
	if m["id"] != float64(s.nextID) {
		s.t.Fatalf("reply id = %v, want %d: %v", m["id"], s.nextID, m)
	}
	return m
}

func (s *wrapSession) handshake() {
	s.t.Helper()
	s.request("initialize", `{"protocolVersion":"2025-11-25","capabilities":{}}`)
	s.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	s.request("tools/list", "")
}

func (s *wrapSession) reads() int {
	return strings.Count(s.stderr.String(), "READ ")
}

// fakeAgentChat records page POSTs. While gate is non-nil, each request
// blocks until gate is closed.
type fakeAgentChat struct {
	srv   *httptest.Server
	mu    sync.Mutex
	posts []wrapAppPost
	got   chan struct{}
	gate  chan struct{}
	// polls counts GET /api/mcp-app/calls; pollStatus is its answer (0 = 404).
	polls      int
	pollStatus int
}

func (f *fakeAgentChat) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

// It also writes this process's password file for its port (in a temp base
// dir) and rejects posts without that password, like the real handler.
func newFakeAgentChat(t *testing.T, gate chan struct{}) *fakeAgentChat {
	useMcpAppBase(t)
	f := &fakeAgentChat{got: make(chan struct{}, 100), gate: gate}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/mcp-app/calls" {
			f.mu.Lock()
			f.polls++
			status := f.pollStatus
			f.mu.Unlock()
			if status == 0 {
				status = http.StatusNotFound // like an agent-chat from before step 2
			}
			http.Error(w, "no", status)
			return
		}
		if r.URL.Path != "/api/mcp-app" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		if r.Header.Get(mcpAppTokenHeader) != mcpAppToken() {
			http.Error(w, "bad password", http.StatusUnauthorized)
			return
		}
		var p wrapAppPost
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if f.gate != nil {
			<-f.gate
		}
		f.mu.Lock()
		f.posts = append(f.posts, p)
		f.mu.Unlock()
		f.got <- struct{}{}
	}))
	t.Cleanup(f.srv.Close)
	if err := writeMcpAppToken(f.srv.Listener.Addr().(*net.TCPAddr).Port); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fakeAgentChat) url() string { return f.srv.URL + "/api/mcp-app" }

func (f *fakeAgentChat) wait(t *testing.T, n int) []wrapAppPost {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-f.got:
		case <-time.After(5 * time.Second):
			t.Fatalf("got %d page posts, want %d", i, n)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wrapAppPost{}, f.posts...)
}

func (f *fakeAgentChat) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posts)
}

func TestWrapAppsDeliversPageAfterForwardingTheAnswer(t *testing.T) {
	gate := make(chan struct{})
	ac := newFakeAgentChat(t, gate)
	s := startWrapSession(t, "--name", "cal", "--agent-chat-url", ac.url())
	s.handshake()

	// agent-chat is stuck (gate closed), yet the answer reaches the client
	// and later requests keep flowing.
	reply := s.request("tools/call", `{"name":"show","arguments":{"who":"Bob"}}`)
	if !strings.Contains(fmt.Sprint(reply["result"]), "ok show") {
		t.Fatalf("tool answer changed: %v", reply)
	}
	s.request("ping", "")
	close(gate)

	posts := ac.wait(t, 1)
	p := posts[0]
	if p.Server != "cal" || p.Tool != "show" || p.ResourceURI != "ui://t/page.html" {
		t.Errorf("post header = %+v", p)
	}
	if p.HTML != "<html>PAGE ui://t/page.html</html>" || p.MimeType != wrapUIMimeType {
		t.Errorf("post page = %q (%s)", p.HTML, p.MimeType)
	}
	if string(p.Arguments) != `{"who":"Bob"}` {
		t.Errorf("arguments = %s", p.Arguments)
	}
	if !strings.Contains(string(p.Result), `"ok show"`) {
		t.Errorf("result = %s", p.Result)
	}
	if !strings.Contains(string(p.ResourceMeta), `"prefersBorder":true`) {
		t.Errorf("resourceMeta = %s", p.ResourceMeta)
	}
	if !strings.Contains(string(p.ToolMeta), `"resourceUri":"ui://t/page.html"`) {
		t.Errorf("toolMeta = %s", p.ToolMeta)
	}
}

func TestWrapAppsFetchesEachPageOnce(t *testing.T) {
	ac := newFakeAgentChat(t, nil)
	s := startWrapSession(t, "--agent-chat-url", ac.url())
	s.handshake()

	s.request("tools/call", `{"name":"show"}`)
	ac.wait(t, 1)
	s.request("tools/call", `{"name":"show"}`)
	s.request("tools/call", `{"name":"legacy"}`)
	posts := ac.wait(t, 2)

	if got := strings.Count(s.stderr.String(), "READ ui://t/page.html"); got != 1 {
		t.Errorf("page.html read %d times, want 1", got)
	}
	if got := strings.Count(s.stderr.String(), "READ ui://t/legacy.html"); got != 1 {
		t.Errorf("legacy.html read %d times, want 1 (deprecated _meta key)", got)
	}
	if posts[0].Server != os.Args[0][strings.LastIndex(os.Args[0], "/")+1:] {
		t.Errorf("default server name = %q, want the command's base name", posts[0].Server)
	}
}

func TestWrapAppsIgnoresToolsWithoutPagesAndErrors(t *testing.T) {
	ac := newFakeAgentChat(t, nil)
	s := startWrapSession(t, "--agent-chat-url", ac.url())
	s.handshake()

	s.request("tools/call", `{"name":"plain"}`)
	s.request("tools/call", `{"name":"fail"}`)
	s.request("ping", "")
	time.Sleep(200 * time.Millisecond) // nothing should be in flight; give it a chance to misbehave
	if n := ac.count(); n != 0 {
		t.Errorf("got %d page posts, want 0", n)
	}
	if n := s.reads(); n != 0 {
		t.Errorf("got %d resources/read, want 0", n)
	}
}

func TestWrapAppsFailedPostDoesNotAffectTheTool(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL + "/api/mcp-app"
	dead.Close() // nothing listens there now
	s := startWrapSession(t, "--agent-chat-url", url)
	s.handshake()

	reply := s.request("tools/call", `{"name":"show"}`)
	if !strings.Contains(fmt.Sprint(reply["result"]), "ok show") {
		t.Fatalf("tool answer changed: %v", reply)
	}
	s.request("ping", "")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(s.stderr.String(), "send page for show to agent-chat") {
		if time.Now().After(deadline) {
			t.Fatalf("no log line for the failed post; stderr:\n%s", s.stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.request("ping", "")
}

func TestWrapAppsOffWithoutAgentChat(t *testing.T) {
	s := startWrapSession(t) // no --agent-chat-url, AGENT_CHAT_PORT empty
	s.handshake()
	s.request("tools/call", `{"name":"show"}`)
	s.request("ping", "")
	time.Sleep(200 * time.Millisecond)
	if n := s.reads(); n != 0 {
		t.Errorf("got %d resources/read with no agent-chat, want 0", n)
	}
}

func TestWrapAppsRelearnsToolsOnFirstPage(t *testing.T) {
	a := newWrapApps("x", "http://unused", &wrapLineWriter{w: io.Discard}, io.Discard)
	a.learnTools(json.RawMessage(`{"tools":[{"name":"a","_meta":{"ui":{"resourceUri":"ui://x/a"}}}]}`), true)
	a.learnTools(json.RawMessage(`{"tools":[{"name":"b","_meta":{"ui":{"resourceUri":"ui://x/b"}}}]}`), false)
	if len(a.tools) != 2 {
		t.Fatalf("after two pages: %v", a.tools)
	}
	a.learnTools(json.RawMessage(`{"tools":[{"name":"c","_meta":{"ui":{"resourceUri":"ui://x/c"}}},{"name":"d","_meta":{"ui":{"resourceUri":"https://not-ui"}}}]}`), true)
	if _, ok := a.tools["c"]; !ok || len(a.tools) != 1 {
		t.Errorf("a fresh first page should replace the list (and skip non-ui:// uris): %v", a.tools)
	}
}

func TestWrapPickPageBlob(t *testing.T) {
	html, mime, _, err := wrapPickPage(json.RawMessage(`{"contents":[{"uri":"ui://x/other","mimeType":"text/plain","text":"no"},{"uri":"ui://x/p","mimeType":"text/html;profile=mcp-app","blob":"PGI+aGk8L2I+"}]}`), "ui://x/p")
	if err != nil || html != "<b>hi</b>" || mime != wrapUIMimeType {
		t.Errorf("got %q %q %v", html, mime, err)
	}
	if _, _, _, err := wrapPickPage(json.RawMessage(`{"contents":[]}`), "ui://x/p"); err == nil {
		t.Errorf("empty contents should be an error")
	}
}

func TestWrapAppsSkipsPostWithoutPassword(t *testing.T) {
	ac := newFakeAgentChat(t, nil)
	useMcpAppBase(t) // a fresh, empty base: as if agent-chat were too old to write one
	s := startWrapSession(t, "--agent-chat-url", ac.url())
	s.handshake()
	s.request("tools/call", `{"name":"show"}`)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(s.stderr.String(), "no password") {
		if time.Now().After(deadline) {
			t.Fatalf("no 'no password' log line; stderr:\n%s", s.stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.request("ping", "")
	if n := ac.count(); n != 0 {
		t.Errorf("got %d posts without a password, want 0", n)
	}
}
