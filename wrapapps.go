package main

// Phase 2 of tasks/2026-11-17-mcp-apps-step1.md: `agent-chat wrap` notices
// when a tool that comes with an MCP Apps page answers, fetches the page from
// the server itself, and hands page + answer to agent-chat.
//
//   - tools/list responses teach it which tools have pages
//     (tool._meta.ui.resourceUri, or the deprecated _meta["ui/resourceUri"]).
//   - tools/call requests are remembered by id (name, arguments).
//   - When a page tool's answer arrives it is forwarded to the client FIRST;
//     only then does the wrapper send resources/read to the server under its
//     own string ids ("acw-N", which can't collide with the client's), swallow
//     the response, and POST everything to agent-chat's /api/mcp-app.
//
// Every failure here is logged to stderr and otherwise ignored: the tool's
// own traffic is never delayed or changed.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	wrapIDPrefix     = "acw-"
	wrapFetchTimeout = 10 * time.Second
	wrapPostTimeout  = 2 * time.Second
)

// wrapCallTimeout caps how long a page's tool run may take (a var for tests).
var wrapCallTimeout = 2 * time.Minute

// wrapReply is the server's answer to one of the wrapper's own requests.
type wrapReply struct {
	result json.RawMessage
	err    json.RawMessage // JSON-RPC error object, if any
	closed bool            // the server connection ended before an answer
}

// wrapCallError is a JSON-RPC style error for a page's tool run.
type wrapCallError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *wrapCallError) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

type wrapCall struct {
	tool      string
	arguments json.RawMessage
}

type wrapTool struct {
	uri  string
	meta json.RawMessage // the tool's whole _meta, passed on to the host
}

// wrapPage is one fetched resource, cached per uri.
type wrapPage struct {
	done     chan struct{}
	html     string
	mimeType string
	meta     json.RawMessage
	err      error
}

// wrapAppPost is the body POSTed to agent-chat's /api/mcp-app.
type wrapAppPost struct {
	Server       string          `json:"server"`
	Tool         string          `json:"tool"`
	Arguments    json.RawMessage `json:"arguments,omitempty"`
	Result       json.RawMessage `json:"result"`
	ResourceURI  string          `json:"resourceUri"`
	MimeType     string          `json:"mimeType"`
	HTML         string          `json:"html"`
	ResourceMeta json.RawMessage `json:"resourceMeta,omitempty"`
	ToolMeta     json.RawMessage `json:"toolMeta,omitempty"`
}

type wrapApps struct {
	server  string
	postURL string
	toChild *wrapLineWriter
	log     io.Writer
	http    *http.Client
	guard   *wrapGuard // nil in unit tests that build a wrapApps directly

	mu          sync.Mutex
	initialized bool
	closed      bool
	tools       map[string]wrapTool       // tool name → page
	appTools    map[string]bool           // tool name → pages may run it (_meta.ui.visibility)
	listReqs    map[string]bool           // tools/list id → is first page
	calls       map[string]wrapCall       // tools/call id → call
	pending     map[string]chan wrapReply // acw id → waiter (nil once abandoned)
	pages       map[string]*wrapPage      // uri → page
	nextID      int
	wg          sync.WaitGroup
}

func newWrapApps(server, postURL string, toChild *wrapLineWriter, log io.Writer) *wrapApps {
	return &wrapApps{
		server:   server,
		postURL:  postURL,
		toChild:  toChild,
		log:      log,
		http:     &http.Client{Timeout: wrapPostTimeout},
		tools:    map[string]wrapTool{},
		appTools: map[string]bool{},
		listReqs: map[string]bool{},
		calls:    map[string]wrapCall{},
		pending:  map[string]chan wrapReply{},
		pages:    map[string]*wrapPage{},
	}
}

func (a *wrapApps) logf(format string, args ...any) {
	fmt.Fprintf(a.log, "agent-chat wrap: "+format+"\n", args...)
}

// wrapMsg is the part of a JSON-RPC message the tracker looks at.
type wrapMsg struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func parseWrapMsg(line []byte) (wrapMsg, bool) {
	var m wrapMsg
	t := bytes.TrimSpace(line)
	if len(t) == 0 || t[0] != '{' {
		return m, false
	}
	if json.Unmarshal(t, &m) != nil {
		return m, false
	}
	return m, true
}

func wrapIDKey(raw json.RawMessage) string {
	return string(bytes.TrimSpace(raw))
}

// clientHook observes client→server traffic, then applies edit (may be nil).
func (a *wrapApps) clientHook(edit wrapEditFunc) wrapHook {
	return func(line []byte) wrapVerdict {
		wrapMaybePanic("client-hook")
		a.observeClient(line)
		if edit == nil {
			return wrapVerdict{}
		}
		return wrapVerdict{repl: edit(line)}
	}
}

func (a *wrapApps) observeClient(line []byte) {
	if !bytes.Contains(line, []byte(`"method"`)) {
		return // responses (to elicitation, roots, …) are never tracked
	}
	m, ok := parseWrapMsg(line)
	if !ok {
		return
	}
	id := wrapIDKey(m.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	switch m.Method {
	case "notifications/initialized":
		a.initialized = true
	case "tools/list":
		if id != "" {
			var p struct {
				Cursor *string `json:"cursor"`
			}
			json.Unmarshal(m.Params, &p)
			a.listReqs[id] = p.Cursor == nil
		}
	case "tools/call":
		if id != "" {
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal(m.Params, &p) == nil && p.Name != "" {
				a.calls[id] = wrapCall{tool: p.Name, arguments: p.Arguments}
			}
		}
	}
}

// serverHook observes server→client traffic: learns page tools, swallows
// replies to the wrapper's own requests, and schedules page delivery after a
// page tool's answer has been forwarded.
func (a *wrapApps) serverHook(line []byte) wrapVerdict {
	wrapMaybePanic("server-hook")
	a.mu.Lock()
	idle := len(a.listReqs) == 0 && len(a.calls) == 0 && len(a.pending) == 0
	a.mu.Unlock()
	if idle && !bytes.Contains(line, []byte("list_changed")) {
		return wrapVerdict{}
	}
	m, ok := parseWrapMsg(line)
	if !ok {
		return wrapVerdict{}
	}
	if m.Method != "" {
		switch m.Method {
		case "notifications/tools/list_changed", "notifications/resources/list_changed":
			a.mu.Lock()
			a.pages = map[string]*wrapPage{} // pages may have changed; refetch
			if m.Method == "notifications/tools/list_changed" {
				a.appTools = map[string]bool{} // relearned from the client's next tools/list
			}
			a.mu.Unlock()
		}
		return wrapVerdict{}
	}
	id := wrapIDKey(m.ID)
	if id == "" {
		return wrapVerdict{}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if ch, ok := a.pending[id]; ok {
		delete(a.pending, id)
		if ch != nil {
			ch <- wrapReply{result: m.Result, err: m.Error}
		}
		return wrapVerdict{drop: true}
	}
	if first, ok := a.listReqs[id]; ok {
		delete(a.listReqs, id)
		if len(m.Result) > 0 {
			a.learnTools(m.Result, first)
		}
		return wrapVerdict{}
	}
	if call, ok := a.calls[id]; ok {
		delete(a.calls, id)
		tool, isPage := a.tools[call.tool]
		if !isPage || len(m.Result) == 0 || !a.initialized || a.closed {
			return wrapVerdict{}
		}
		var r struct {
			IsError bool `json:"isError"`
		}
		if json.Unmarshal(m.Result, &r) == nil && r.IsError {
			return wrapVerdict{}
		}
		result := append(json.RawMessage{}, m.Result...)
		a.wg.Add(1)
		return wrapVerdict{after: func() {
			go func() {
				defer a.wg.Done()
				defer a.guard.catch()
				a.deliver(call, tool, result)
			}()
		}}
	}
	return wrapVerdict{}
}

// learnTools records page tools from a tools/list result. A first page
// replaces what was known (the list may have changed); later pages add.
// Caller holds a.mu.
func (a *wrapApps) learnTools(result json.RawMessage, first bool) {
	var r struct {
		Tools []struct {
			Name string          `json:"name"`
			Meta json.RawMessage `json:"_meta"`
		} `json:"tools"`
	}
	if json.Unmarshal(result, &r) != nil {
		return
	}
	if first {
		a.tools = map[string]wrapTool{}
		a.appTools = map[string]bool{}
	}
	for _, t := range r.Tools {
		if t.Name != "" {
			a.appTools[t.Name] = wrapAppVisible(t.Meta)
		}
		if uri := wrapToolPageURI(t.Meta); uri != "" && t.Name != "" {
			a.tools[t.Name] = wrapTool{uri: uri, meta: append(json.RawMessage{}, t.Meta...)}
		}
	}
}

// wrapAppVisible reads _meta.ui.visibility: pages may run a tool when it
// includes "app". No visibility means ["model","app"] (spec default).
func wrapAppVisible(meta json.RawMessage) bool {
	var m struct {
		UI struct {
			Visibility *[]string `json:"visibility"`
		} `json:"ui"`
	}
	if len(meta) == 0 || json.Unmarshal(meta, &m) != nil || m.UI.Visibility == nil {
		return true
	}
	for _, v := range *m.UI.Visibility {
		if v == "app" {
			return true
		}
	}
	return false
}

// wrapToolPageURI reads _meta.ui.resourceUri, falling back to the
// deprecated flat key _meta["ui/resourceUri"].
func wrapToolPageURI(meta json.RawMessage) string {
	if len(meta) == 0 {
		return ""
	}
	var m struct {
		UI struct {
			ResourceURI string `json:"resourceUri"`
		} `json:"ui"`
		Flat string `json:"ui/resourceUri"`
	}
	if json.Unmarshal(meta, &m) != nil {
		return ""
	}
	if strings.HasPrefix(m.UI.ResourceURI, "ui://") {
		return m.UI.ResourceURI
	}
	if strings.HasPrefix(m.Flat, "ui://") {
		return m.Flat
	}
	return ""
}

func (a *wrapApps) deliver(call wrapCall, tool wrapTool, result json.RawMessage) {
	wrapMaybePanic("deliver")
	page := a.page(tool.uri)
	if page.err != nil {
		a.logf("page %s for %s: %v", tool.uri, call.tool, page.err)
		return
	}
	body, err := json.Marshal(wrapAppPost{
		Server:       a.server,
		Tool:         call.tool,
		Arguments:    call.arguments,
		Result:       result,
		ResourceURI:  tool.uri,
		MimeType:     page.mimeType,
		HTML:         page.html,
		ResourceMeta: page.meta,
		ToolMeta:     tool.meta,
	})
	if err != nil {
		a.logf("encode page for %s: %v", call.tool, err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, a.postURL, bytes.NewReader(body))
	if err != nil {
		a.logf("send page for %s to agent-chat: %v", call.tool, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	tok, err := a.token()
	if err != nil {
		// An agent-chat without MCP Apps support writes no token: skip quietly-ish.
		a.logf("send page for %s to agent-chat: no password: %v", call.tool, err)
		return
	}
	req.Header.Set(mcpAppTokenHeader, tok)
	resp, err := a.http.Do(req)
	if err != nil {
		a.logf("send page for %s to agent-chat: %v", call.tool, err)
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		a.logf("send page for %s to agent-chat: HTTP %d", call.tool, resp.StatusCode)
	}
}

// token reads agent-chat's password for the port in postURL. Read on every
// post, so a restarted agent-chat (new password) is picked up.
func (a *wrapApps) token() (string, error) {
	u, err := url.Parse(a.postURL)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", fmt.Errorf("no port in %s", a.postURL)
	}
	return readMcpAppToken(port)
}

// page returns the cached page for uri, fetching it once. A failed fetch is
// forgotten so the next call retries.
func (a *wrapApps) page(uri string) *wrapPage {
	a.mu.Lock()
	if p, ok := a.pages[uri]; ok {
		a.mu.Unlock()
		<-p.done
		if p.err == nil {
			return p
		}
		a.mu.Lock()
		if a.pages[uri] == p {
			delete(a.pages, uri)
		}
		a.mu.Unlock()
		return a.page(uri)
	}
	p := &wrapPage{done: make(chan struct{})}
	a.pages[uri] = p
	a.mu.Unlock()

	p.html, p.mimeType, p.meta, p.err = a.fetch(uri)
	close(p.done)
	if p.err != nil {
		a.mu.Lock()
		if a.pages[uri] == p {
			delete(a.pages, uri)
		}
		a.mu.Unlock()
	}
	return p
}

// fetch sends resources/read to the server under the wrapper's own id and
// waits for the (swallowed) reply.
func (a *wrapApps) fetch(uri string) (html, mimeType string, meta json.RawMessage, err error) {
	r, err := a.request("resources/read", map[string]string{"uri": uri}, wrapFetchTimeout)
	var ce *wrapCallError
	if errors.As(err, &ce) && ce.Code == -32001 {
		return "", "", nil, errors.New("resources/read timed out")
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("resources/read: %w", err)
	}
	if len(r.err) > 0 {
		return "", "", nil, errors.New("resources/read failed")
	}
	return wrapPickPage(r.result, uri)
}

// callTool runs one of the server's tools for a page (step 2), under the
// wrapper's own id so the reply never reaches the client. Only tools the
// server lets pages use (wrapAppVisible) run; an isError result is returned
// as an ordinary result for the page to show.
func (a *wrapApps) callTool(name string, args json.RawMessage) (json.RawMessage, error) {
	a.mu.Lock()
	allowed := a.appTools[name]
	a.mu.Unlock()
	if !allowed {
		return nil, &wrapCallError{Code: -32602, Message: fmt.Sprintf("tool %q can't be run from a page", name)}
	}
	params := map[string]any{"name": name}
	if len(bytes.TrimSpace(args)) > 0 && string(bytes.TrimSpace(args)) != "null" {
		params["arguments"] = args
	}
	r, err := a.request("tools/call", params, wrapCallTimeout)
	if err != nil {
		return nil, err
	}
	if len(r.err) > 0 {
		ce := &wrapCallError{}
		if json.Unmarshal(r.err, ce) != nil || ce.Message == "" {
			ce = &wrapCallError{Code: -32603, Message: "tool failed: " + string(r.err)}
		}
		return nil, ce
	}
	return r.result, nil
}

// request sends method to the server under a fresh "acw-N" id and waits up to
// timeout for the reply, which serverHook swallows.
func (a *wrapApps) request(method string, params any, timeout time.Duration) (wrapReply, error) {
	notRunning := &wrapCallError{Code: -32000, Message: "tool program not running"}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return wrapReply{}, notRunning
	}
	a.nextID++
	id := fmt.Sprintf("%s%d", wrapIDPrefix, a.nextID)
	idJSON, _ := json.Marshal(id)
	key := string(idJSON)
	ch := make(chan wrapReply, 1)
	a.pending[key] = ch
	a.mu.Unlock()

	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		a.abandon(key)
		return wrapReply{}, &wrapCallError{Code: -32602, Message: err.Error()}
	}
	if err := a.toChild.writeLine(req); err != nil {
		a.abandon(key)
		return wrapReply{}, notRunning
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		if r.closed {
			return wrapReply{}, notRunning
		}
		return r, nil
	case <-timer.C:
		a.abandon(key)
		return wrapReply{}, &wrapCallError{Code: -32001, Message: "tool took too long"}
	}
}

// abandon stops waiting for id but keeps it registered, so a late reply is
// still swallowed instead of reaching the client.
func (a *wrapApps) abandon(key string) {
	a.mu.Lock()
	if _, ok := a.pending[key]; ok {
		a.pending[key] = nil
	}
	a.mu.Unlock()
}

// wrapPickPage extracts the page from a resources/read result: the content
// with the MCP Apps mime type (else the one matching uri, else the first).
func wrapPickPage(result json.RawMessage, uri string) (html, mimeType string, meta json.RawMessage, err error) {
	var r struct {
		Contents []struct {
			URI      string          `json:"uri"`
			MimeType string          `json:"mimeType"`
			Text     *string         `json:"text"`
			Blob     *string         `json:"blob"`
			Meta     json.RawMessage `json:"_meta"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return "", "", nil, fmt.Errorf("bad resources/read result: %w", err)
	}
	pick := -1
	for i, c := range r.Contents {
		if c.MimeType == wrapUIMimeType {
			pick = i
			break
		}
	}
	for i, c := range r.Contents {
		if pick < 0 && c.URI == uri {
			pick = i
		}
	}
	if pick < 0 && len(r.Contents) > 0 {
		pick = 0
	}
	if pick < 0 {
		return "", "", nil, errors.New("resources/read returned no contents")
	}
	c := r.Contents[pick]
	switch {
	case c.Text != nil:
		html = *c.Text
	case c.Blob != nil:
		b, err := base64.StdEncoding.DecodeString(*c.Blob)
		if err != nil {
			return "", "", nil, fmt.Errorf("bad blob: %w", err)
		}
		html = string(b)
	default:
		return "", "", nil, errors.New("page has neither text nor blob")
	}
	mimeType = c.MimeType
	if mimeType == "" {
		mimeType = wrapUIMimeType
	}
	return html, mimeType, c.Meta, nil
}

// shutdown runs when the server's output has ended: fail every waiting fetch
// (no reply can come now), then give in-flight deliveries up to max to finish.
func (a *wrapApps) shutdown(max time.Duration) {
	a.mu.Lock()
	a.closed = true
	for k, ch := range a.pending {
		if ch != nil {
			ch <- wrapReply{closed: true}
		}
		delete(a.pending, k)
	}
	a.mu.Unlock()
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(max):
	}
}
