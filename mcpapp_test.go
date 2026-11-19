package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// useMcpAppBase points the token and page store at a fresh temp dir.
func useMcpAppBase(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := mcpAppBaseDir
	mcpAppBaseDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { mcpAppBaseDir = orig })
	return dir
}

// useTestBus swaps the global bus for a fresh one and subscribes to it.
func useTestBus(t *testing.T) chan Event {
	t.Helper()
	orig := bus
	bus = NewEventBus()
	ch := bus.Subscribe()
	t.Cleanup(func() { bus.Unsubscribe(ch); bus = orig })
	return ch
}

const samplePost = `{"server":"cal","tool":"show","arguments":{"who":"Bob"},"result":{"content":[{"type":"text","text":"ok"}]},"resourceUri":"ui://t/page.html","mimeType":"text/html;profile=mcp-app","html":"<html>PAGE</html>","resourceMeta":{"ui":{"prefersBorder":true}},"toolMeta":{"ui":{"resourceUri":"ui://t/page.html"}}}`

func postMcpApp(remote, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-app", strings.NewReader(body))
	req.RemoteAddr = remote
	if token != "" {
		req.Header.Set(mcpAppTokenHeader, token)
	}
	rec := httptest.NewRecorder()
	handleMcpApp(rec, req)
	return rec
}

func TestMcpAppTokenFile(t *testing.T) {
	base := useMcpAppBase(t)
	if err := writeMcpAppToken(4321); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "mcp-app-tokens", "4321")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("token file mode = %o, want 600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("token dir mode = %o, want 700", di.Mode().Perm())
	}
	got, err := readMcpAppToken(4321)
	if err != nil || got != mcpAppToken() || len(got) != 64 {
		t.Errorf("read back %q, %v; want this process's 64-char token", got, err)
	}
	if _, err := readMcpAppToken(4322); err == nil {
		t.Errorf("a port with no agent-chat should have no token")
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".tmp-*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestMcpAppRejectsOtherMachinesAndWrongPasswords(t *testing.T) {
	useMcpAppBase(t)
	events := useTestBus(t)
	tok := mcpAppToken()
	cases := []struct {
		name, remote, token string
		want                int
	}{
		{"other machine, right password", "10.1.2.3:5555", tok, http.StatusForbidden},
		{"no password", "127.0.0.1:5555", "", http.StatusUnauthorized},
		{"wrong password", "127.0.0.1:5555", strings.Repeat("0", 64), http.StatusUnauthorized},
		{"password prefix", "127.0.0.1:5555", tok[:10], http.StatusUnauthorized},
	}
	for _, c := range cases {
		if rec := postMcpApp(c.remote, c.token, samplePost); rec.Code != c.want {
			t.Errorf("%s: HTTP %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
	select {
	case e := <-events:
		t.Errorf("a rejected post published %+v", e)
	default:
	}
}

func TestMcpAppValidatesThePage(t *testing.T) {
	useMcpAppBase(t)
	useTestBus(t)
	tok := mcpAppToken()
	edit := func(field string, v any) string {
		var m map[string]any
		json.Unmarshal([]byte(samplePost), &m)
		m[field] = v
		b, _ := json.Marshal(m)
		return string(b)
	}
	cases := []struct {
		name, body string
		want       int
	}{
		{"not json", "{", http.StatusBadRequest},
		{"no tool", edit("tool", ""), http.StatusBadRequest},
		{"no html", edit("html", ""), http.StatusBadRequest},
		{"plain html mime type", edit("mimeType", "text/html"), http.StatusUnsupportedMediaType},
		{"https resource", edit("resourceUri", "https://evil/page"), http.StatusBadRequest},
		{"too big", edit("html", strings.Repeat("x", mcpAppMaxBody)), http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		if rec := postMcpApp("127.0.0.1:1", tok, c.body); rec.Code != c.want {
			t.Errorf("%s: HTTP %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/mcp-app", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	handleMcpApp(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: HTTP %d, want 405", rec.Code)
	}
}

func TestMcpAppPublishesEventAndServesPageSafely(t *testing.T) {
	base := useMcpAppBase(t)
	events := useTestBus(t)

	rec := postMcpApp("[::1]:5555", mcpAppToken(), samplePost)
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var e Event
	select {
	case e = <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("no event published")
	}
	if e.Type != "mcpApp" || e.App == nil {
		t.Fatalf("event = %+v", e)
	}
	a := e.App
	if a.Server != "cal" || a.Tool != "show" || a.ResourceURI != "ui://t/page.html" || a.ID == "" {
		t.Errorf("app = %+v", a)
	}
	if string(a.Arguments) != `{"who":"Bob"}` || !strings.Contains(string(a.Result), `"ok"`) {
		t.Errorf("call = %s → %s", a.Arguments, a.Result)
	}
	if !strings.Contains(string(a.ResourceMeta), "prefersBorder") || !strings.Contains(string(a.ToolMeta), "resourceUri") {
		t.Errorf("meta = %s / %s", a.ResourceMeta, a.ToolMeta)
	}
	if !mcpAppPageIDRe.MatchString(a.PageID) {
		t.Fatalf("pageId = %q", a.PageID)
	}
	// The html travels by id, not inside the event (keeps history replays small).
	raw, _ := json.Marshal(e)
	if strings.Contains(string(raw), "PAGE") {
		t.Errorf("event carries the page itself: %s", raw)
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["id"] != a.ID {
		t.Errorf("response id %q, event id %q", resp["id"], a.ID)
	}

	// Served as inert text, never as a page on agent-chat's own origin.
	get := httptest.NewRecorder()
	handleMcpAppPage(get, httptest.NewRequest(http.MethodGet, mcpAppPagePrefix+a.PageID, nil))
	if get.Code != 200 || get.Body.String() != "<html>PAGE</html>" {
		t.Fatalf("page GET: %d %q", get.Code, get.Body.String())
	}
	h := get.Header()
	if h.Get("Content-Type") != "text/plain; charset=utf-8" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("page headers = %v", h)
	}

	// Same page again: same id, stored once.
	postMcpApp("127.0.0.1:1", mcpAppToken(), samplePost)
	e2 := <-events
	if e2.App.PageID != a.PageID || e2.App.ID == a.ID {
		t.Errorf("second post: pageId %q (want %q), id %q (want new)", e2.App.PageID, a.PageID, e2.App.ID)
	}
	files, _ := filepath.Glob(filepath.Join(base, "mcp-app-pages", "*"))
	if len(files) != 1 {
		t.Errorf("page store = %v, want one file", files)
	}

	for _, bad := range []string{"../../etc/passwd", "abc", strings.Repeat("A", 64), strings.Repeat("0", 64)} {
		r := httptest.NewRecorder()
		handleMcpAppPage(r, httptest.NewRequest(http.MethodGet, mcpAppPagePrefix+bad, nil))
		if r.Code != http.StatusNotFound {
			t.Errorf("GET %q: HTTP %d, want 404", bad, r.Code)
		}
	}
}

func TestMcpAppChatLogLine(t *testing.T) {
	st := &renderState{}
	user := Event{Type: "userMessage", Text: "show it", Timestamp: 1000}
	app := Event{Type: "mcpApp", Timestamp: 3500, App: &McpApp{Server: "cal", Tool: "show", PageID: "x"}}
	renderChatBubble(user, st, nil)
	got := renderChatBubble(app, st, nil)
	want := "<small>took 2.5s</small><br>\n**AGENT**\n\n> 📱 `cal/show` page shown\n\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if renderChatBubble(Event{Type: "mcpApp"}, &renderState{}, nil) != "" {
		t.Errorf("an mcpApp event without an app should render nothing")
	}
}
