package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
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
	useMcpAppRelay(t)
	srv := newMcpAppMux(t)
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

	// Step 2: the chat window runs book_slot for the page, through agent-chat
	// and the wrapper, on the same sample server.
	var code int
	var body map[string]json.RawMessage
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, body = browserCall(t, srv, `{"wrapper":"`+e.App.Wrapper+`","server":"mcp-apps-sample","tool":"book_slot","arguments":{"person":"Bob","day":"Wed 25"}}`, nil)
		if code != http.StatusServiceUnavailable || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code != 200 || !strings.Contains(string(body["result"]), "Pencilled in Bob on Wed 25") {
		t.Fatalf("page tool run: %d %s", code, mustJSON(body))
	}
	if !strings.Contains(string(body["result"]), `"Wed 25":"pending"`) {
		t.Errorf("the page should get the updated grid: %s", body["result"])
	}
	select {
	case e2 := <-events:
		if e2.Type != "mcpAppCall" || e2.AppCall == nil || e2.AppCall.Tool != "book_slot" {
			t.Errorf("event = %+v", e2)
		}
	case <-time.After(3 * time.Second):
		t.Error("no mcpAppCall event")
	}
	// Claude Code's side saw nothing of it, and later traffic still flows.
	reply = s.request("tools/call", `{"name":"show_calendar","arguments":{}}`)
	if !strings.Contains(stringify(reply), "pending") {
		t.Errorf("the booking made from the page should be visible to Claude's own calls: %v", reply)
	}
	// Claude hears about it with the user's next message.
	bus.ReceiveUserMessage("what changed?", nil, "")
	if got := FormatMessages(bus.DrainMessages()); !strings.Contains(got, "page ran `book_slot` (day: Wed 25, person: Bob): Pencilled in Bob on Wed 25") {
		t.Errorf("Claude's note:\n%s", got)
	}
}

func stringify(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}
