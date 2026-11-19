package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The wrap tests re-exec this test binary as a fake MCP server. When
// WRAP_FAKE_CHILD is set, TestMain plays the child instead of running tests:
//
//	WRAP_FAKE_EMIT  lines written to stdout before anything is read
//	                (server→client traffic)
//	then every stdin byte is echoed to stdout verbatim (so the test sees
//	exactly what the server received), then exit with WRAP_FAKE_EXIT.
func TestMain(m *testing.M) {
	if os.Getenv("WRAP_FAKE_CHILD") == "1" {
		os.Stdout.WriteString(os.Getenv("WRAP_FAKE_EMIT"))
		io.Copy(os.Stdout, os.Stdin)
		code, _ := strconv.Atoi(os.Getenv("WRAP_FAKE_EXIT"))
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// runFakeWrap runs `agent-chat wrap [flags] -- <fake child>` with the given
// client input, and returns the client-side output and exit code.
func runFakeWrap(t *testing.T, flags []string, emit, input string, exit int) (string, int) {
	t.Helper()
	t.Setenv("WRAP_FAKE_CHILD", "1")
	t.Setenv("WRAP_FAKE_EMIT", emit)
	t.Setenv("WRAP_FAKE_EXIT", strconv.Itoa(exit))
	args := append(append([]string{}, flags...), "--", os.Args[0])
	var out, errb bytes.Buffer
	code := runWrap(args, strings.NewReader(input), &out, &errb)
	if errb.Len() > 0 {
		t.Logf("wrap stderr: %s", errb.String())
	}
	return out.String(), code
}

func TestWrapForwardsUneditedLinesByteForByte(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{ "jsonrpc" : "2.0", "id" : 2, "method" : "tools/call", "params" : {"name":"x","arguments":{"b":1,"a":2}} }`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2,"reason":"user"}}`,
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"t","progress":1}}`,
		`{"jsonrpc":"2.0","id":"srv-1","result":{"action":"accept","content":{"ok":true}}}`, // elicitation reply
		`{"jsonrpc":"2.0","id":"srv-2","result":{"roots":[{"uri":"file:///x"}]}}`,           // roots reply
		`not json at all {"initialize"`,
		`{"jsonrpc":"2.0","id":3,"method":"initialize","params":` + "\r",                // truncated, CRLF
		`[{"jsonrpc":"2.0","id":4,"method":"initialize","params":{"capabilities":{}}}]`, // batch: left alone
		`{"jsonrpc":"2.0","id":5,"result":{"method":"initialize","params":{"capabilities":{}}}}`,
		``,
		`{"jsonrpc":"2.0","id":6,"method":"ping"}`, // no trailing newline at EOF
	}, "\n")
	got, code := runFakeWrap(t, nil, "", input, 0)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got != input {
		t.Fatalf("output differs from input\n got: %q\nwant: %q", got, input)
	}
}

func TestWrapPassesServerTrafficThrough(t *testing.T) {
	emit := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"srv-1","method":"elicitation/create","params":{"message":"Name?","requestedSchema":{"type":"object"}}}`,
		`{"jsonrpc":"2.0","id":"srv-2","method":"roots/list"}`,
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"t","progress":0.5}}`,
		`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"srv-1"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"extensions":{}}},"method":"initialize"}`,
	}, "\n") + "\n"
	got, code := runFakeWrap(t, nil, emit, "", 0)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if got != emit {
		t.Fatalf("server traffic changed\n got: %q\nwant: %q", got, emit)
	}
}

// uiExt digs extensions[ui] out of a capabilities object.
func uiExt(t *testing.T, caps map[string]any) any {
	t.Helper()
	exts, _ := caps["extensions"].(map[string]any)
	return exts[wrapUIExtension]
}

func decodeLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("output line isn't JSON: %v: %q", err, line)
	}
	return m
}

func TestWrapAdvertisesUIOnInitialize(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"roots":{},"elicitation":{},"extensions":{"io.modelcontextprotocol/tasks":{"v":1}}},"clientInfo":{"name":"claude-code","version":"2.1.325"}}}` + "\r\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	got, _ := runFakeWrap(t, nil, "", input, 0)
	lines := strings.SplitAfter(got, "\n")
	if !strings.HasSuffix(lines[0], "\r\n") {
		t.Errorf("CRLF ending lost: %q", lines[0])
	}
	if lines[1] != `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n" {
		t.Errorf("following line changed: %q", lines[1])
	}
	msg := decodeLine(t, lines[0])
	if msg["id"] != float64(0) || msg["method"] != "initialize" || msg["jsonrpc"] != "2.0" {
		t.Errorf("envelope changed: %v", msg)
	}
	params := msg["params"].(map[string]any)
	if params["protocolVersion"] != "2025-11-25" {
		t.Errorf("protocolVersion lost: %v", params)
	}
	if ci := params["clientInfo"].(map[string]any); ci["name"] != "claude-code" {
		t.Errorf("clientInfo lost: %v", ci)
	}
	caps := params["capabilities"].(map[string]any)
	if _, ok := caps["roots"]; !ok {
		t.Errorf("roots capability lost: %v", caps)
	}
	if _, ok := caps["elicitation"]; !ok {
		t.Errorf("elicitation capability lost: %v", caps)
	}
	exts := caps["extensions"].(map[string]any)
	if tasks, _ := exts["io.modelcontextprotocol/tasks"].(map[string]any); tasks["v"] != float64(1) {
		t.Errorf("existing tasks extension lost: %v", exts)
	}
	ui, _ := uiExt(t, caps).(map[string]any)
	mimes, _ := ui["mimeTypes"].([]any)
	if len(mimes) != 1 || mimes[0] != wrapUIMimeType {
		t.Errorf("ui extension = %v, want mimeTypes [%s]", ui, wrapUIMimeType)
	}
}

func TestWrapAdvertisesUIWhenCapabilitiesMissing(t *testing.T) {
	got, _ := runFakeWrap(t, nil, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`+"\n", 0)
	caps := decodeLine(t, got)["params"].(map[string]any)["capabilities"].(map[string]any)
	if uiExt(t, caps) == nil {
		t.Errorf("ui extension not added: %v", caps)
	}
}

func TestWrapAdvertisesUIInPerRequestMeta(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"show_calendar","arguments":{},"_meta":{"progressToken":"p1","io.modelcontextprotocol/clientCapabilities":{"elicitation":{}}}}}` + "\n" +
		`{"jsonrpc":"2.0","id":8,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/clientCapabilities":{}}}}` + "\n"
	got, _ := runFakeWrap(t, nil, "", input, 0)
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %q", len(lines), got)
	}
	for i, line := range lines {
		params := decodeLine(t, line)["params"].(map[string]any)
		meta := params["_meta"].(map[string]any)
		caps := meta[wrapClientCapsMeta].(map[string]any)
		if uiExt(t, caps) == nil {
			t.Errorf("line %d: ui extension not added: %v", i, caps)
		}
		if i == 0 {
			if meta["progressToken"] != "p1" {
				t.Errorf("progressToken lost: %v", meta)
			}
			if _, ok := caps["elicitation"]; !ok {
				t.Errorf("elicitation lost: %v", caps)
			}
			if params["name"] != "show_calendar" {
				t.Errorf("tool name lost: %v", params)
			}
		}
	}
}

func TestWrapRespectsClientsOwnUIDeclaration(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{"extensions":{"io.modelcontextprotocol/ui":{"mimeTypes":["text/html;profile=mcp-app","x/other"]}}}}}` + "\n"
	got, _ := runFakeWrap(t, nil, "", input, 0)
	if got != input {
		t.Errorf("a client that already declares ui was edited\n got: %q\nwant: %q", got, input)
	}
}

func TestWrapNoAdvertiseUIFlag(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}` + "\n"
	got, _ := runFakeWrap(t, []string{"--no-advertise-ui"}, "", input, 0)
	if got != input {
		t.Errorf("--no-advertise-ui still edited\n got: %q\nwant: %q", got, input)
	}
}

func TestWrapPropagatesExitCode(t *testing.T) {
	if _, code := runFakeWrap(t, nil, "", "", 3); code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}

func TestWrapUsageErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runWrap(nil, strings.NewReader(""), &out, &errb); code != 2 {
		t.Errorf("no command: exit %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "usage: agent-chat wrap") {
		t.Errorf("no usage text: %q", errb.String())
	}
	errb.Reset()
	if code := runWrap([]string{"--", "/nonexistent/mcp-server"}, strings.NewReader(""), &out, &errb); code != 127 {
		t.Errorf("missing binary: exit %d, want 127", code)
	}
}

func TestWrapRelayStreamsOverLongLineUnedited(t *testing.T) {
	long := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{},"pad":"` +
		strings.Repeat("x", wrapMaxLineBytes) + `"}}` + "\n"
	input := long + `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"capabilities":{}}}` + "\n"
	var out bytes.Buffer
	if err := wrapRelay(strings.NewReader(input), &out, wrapAdvertiseUI); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(&out)
	first, _ := r.ReadString('\n')
	if first != long {
		t.Errorf("over-long line was changed (len %d, want %d)", len(first), len(long))
	}
	second, _ := r.ReadString('\n')
	if !strings.Contains(second, wrapUIExtension) {
		t.Errorf("line after the over-long one wasn't edited: %q", second)
	}
}
