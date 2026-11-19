package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMcpAppEndToEnd runs the whole chain in-process except the tool server:
// a client speaking like Claude Code → `agent-chat wrap` → the real
// testdata/mcp-apps-sample server (built here, --ui=auto, so it only shows
// its page because the wrapper says "I can show pages") → POST to agent-chat's
// real /api/mcp-app handler → an mcpApp event on the bus → the page served
// back from /api/mcp-app/page/<id>.
func TestMcpAppEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the sample server")
	}
	sample := filepath.Join(t.TempDir(), "mcp-apps-sample")
	build := exec.Command("go", "build", "-o", sample, "./testdata/mcp-apps-sample")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sample: %v\n%s", err, out)
	}
	wantHTML, err := os.ReadFile("testdata/mcp-apps-sample/calendar.html")
	if err != nil {
		t.Fatal(err)
	}

	useMcpAppBase(t)
	events := useTestBus(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp-app", handleMcpApp)
	mux.HandleFunc(mcpAppPagePrefix, handleMcpAppPage)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	if err := writeMcpAppToken(srv.Listener.Addr().(*net.TCPAddr).Port); err != nil {
		t.Fatal(err)
	}

	s := startWrap(t, []string{"--name", "mcp-apps-sample", "--agent-chat-url", srv.URL + "/api/mcp-app", "--", sample, "--ui=auto"})

	// Claude Code 2.1.325's own initialize: no ui extension of its own.
	s.request("initialize", `{"protocolVersion":"2025-11-25","capabilities":{"roots":{},"elicitation":{}},"clientInfo":{"name":"claude-code","version":"2.1.325"}}`)
	s.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	list := s.request("tools/list", "")
	if !strings.Contains(stringify(list), "ui://mcp-apps-sample/calendar.html") {
		t.Fatalf("sample hid its page: %v", list)
	}
	reply := s.request("tools/call", `{"name":"show_calendar","arguments":{}}`)
	if !strings.Contains(stringify(reply), "structuredContent") {
		t.Fatalf("tool answer = %v", reply)
	}

	var e Event
	select {
	case e = <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("no mcpApp event")
	}
	if e.Type != "mcpApp" || e.App == nil || e.App.Server != "mcp-apps-sample" || e.App.Tool != "show_calendar" {
		t.Fatalf("event = %+v", e)
	}
	if !strings.Contains(string(e.App.Result), `"structuredContent"`) {
		t.Errorf("event result = %s", e.App.Result)
	}
	resp, err := http.Get(srv.URL + mcpAppPagePrefix + e.App.PageID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != string(wantHTML) {
		t.Errorf("served page (%d bytes) differs from calendar.html (%d bytes)", len(got), len(wantHTML))
	}
	if !strings.Contains(s.stderr.String(), "ui=true") {
		t.Errorf("sample didn't log ui=true")
	}
}

func stringify(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}
