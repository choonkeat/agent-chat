package main

// Step 2 phase 2 of tasks/2026-11-18-mcp-apps-step2-page-tools.md: agent-chat
// hands a page's tool request to the `agent-chat wrap` process that showed
// the page, and returns the answer.
//
//	browser  POST /api/mcp-app/call        {wrapper, server, tool, arguments}
//	wrapper  GET  /api/mcp-app/calls?wrapper=<id>    (waits up to 25 s for work)
//	wrapper  POST /api/mcp-app/calls/<callId>        {result} | {error}
//
// The wrapper side needs the same password and this-machine check as page
// posts. The browser side only accepts JSON from the chat page itself: a
// cross-site page can't send application/json without a CORS preflight, which
// agent-chat never grants, and Sec-Fetch-Site says where a request came from.
//
// Everything is in memory. After an agent-chat restart, wrappers check in
// again on their next poll.

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	mcpAppCallPath  = "/api/mcp-app/call"
	mcpAppCallsPath = "/api/mcp-app/calls"
)

var (
	// mcpAppPollWait is how long a wrapper's poll is held open with no work.
	mcpAppPollWait = 25 * time.Second
	// mcpAppPollStale: a wrapper silent for longer counts as not running.
	mcpAppPollStale = 35 * time.Second
	// mcpAppCallWait caps the browser's wait (the wrapper gives up at 2 min).
	mcpAppCallWait = 2*time.Minute + 10*time.Second

	mcpAppCalls = newMcpAppRelay()
)

// mcpAppCallReq is one queued tool run, as the wrapper receives it.
type mcpAppCallReq struct {
	ID        string          `json:"id"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// mcpAppCallAnswer is the wrapper's answer, passed to the browser as is.
type mcpAppCallAnswer struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *wrapCallError  `json:"error,omitempty"`
}

type mcpAppWrapper struct {
	lastSeen time.Time
	polling  int
	queue    []mcpAppCallReq
	wake     chan struct{} // closed and replaced when work is queued
}

type mcpAppRelay struct {
	mu       sync.Mutex
	wrappers map[string]*mcpAppWrapper
	waiting  map[string]chan mcpAppCallAnswer // call id → browser waiting for it
}

func newMcpAppRelay() *mcpAppRelay {
	return &mcpAppRelay{wrappers: map[string]*mcpAppWrapper{}, waiting: map[string]chan mcpAppCallAnswer{}}
}

func (r *mcpAppRelay) wrapper(id string) *mcpAppWrapper {
	w := r.wrappers[id]
	if w == nil {
		w = &mcpAppWrapper{wake: make(chan struct{})}
		r.wrappers[id] = w
	}
	return w
}

// running reports whether the wrapper is waiting now or checked in recently.
// Caller holds r.mu.
func (r *mcpAppRelay) running(id string) bool {
	w := r.wrappers[id]
	return w != nil && (w.polling > 0 || time.Since(w.lastSeen) < mcpAppPollStale)
}

// take waits up to wait for queued calls for wrapper id and returns them.
func (r *mcpAppRelay) take(id string, wait time.Duration, done <-chan struct{}) []mcpAppCallReq {
	r.mu.Lock()
	w := r.wrapper(id)
	w.polling++
	w.lastSeen = time.Now()
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		w.polling--
		w.lastSeen = time.Now()
		r.mu.Unlock()
	}()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		r.mu.Lock()
		if len(w.queue) > 0 {
			calls := w.queue
			w.queue = nil
			r.mu.Unlock()
			return calls
		}
		wake := w.wake
		r.mu.Unlock()
		select {
		case <-wake:
		case <-timer.C:
			return []mcpAppCallReq{}
		case <-done:
			return []mcpAppCallReq{}
		}
	}
}

// call queues a tool run for wrapper id and waits for its answer.
func (r *mcpAppRelay) call(id, tool string, args json.RawMessage, done <-chan struct{}) (mcpAppCallAnswer, bool) {
	r.mu.Lock()
	if !r.running(id) {
		r.mu.Unlock()
		return mcpAppCallAnswer{}, false
	}
	w := r.wrapper(id)
	c := mcpAppCallReq{ID: uuid.New().String(), Tool: tool, Arguments: args}
	ch := make(chan mcpAppCallAnswer, 1)
	r.waiting[c.ID] = ch
	w.queue = append(w.queue, c)
	close(w.wake)
	w.wake = make(chan struct{})
	r.mu.Unlock()

	forget := func() {
		r.mu.Lock()
		delete(r.waiting, c.ID)
		for i, q := range w.queue {
			if q.ID == c.ID {
				w.queue = append(w.queue[:i], w.queue[i+1:]...)
				break
			}
		}
		r.mu.Unlock()
	}
	timer := time.NewTimer(mcpAppCallWait)
	defer timer.Stop()
	select {
	case a := <-ch:
		return a, true
	case <-timer.C:
		forget()
		return mcpAppCallAnswer{Error: &wrapCallError{Code: -32001, Message: "tool took too long"}}, true
	case <-done:
		forget()
		return mcpAppCallAnswer{Error: &wrapCallError{Code: -32000, Message: "cancelled"}}, true
	}
}

// answer delivers the wrapper's answer for call id. False if nobody waits.
func (r *mcpAppRelay) answer(id string, a mcpAppCallAnswer) bool {
	r.mu.Lock()
	ch, ok := r.waiting[id]
	delete(r.waiting, id)
	r.mu.Unlock()
	if ok {
		ch <- a
	}
	return ok
}

// mcpAppWrapperAuth is the wrapper-side check shared with page posts.
func mcpAppWrapperAuth(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRemote(r.RemoteAddr) {
		http.Error(w, "only accepted from this machine", http.StatusForbidden)
		return false
	}
	want := mcpAppToken()
	got := r.Header.Get(mcpAppTokenHeader)
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		http.Error(w, "wrong or missing "+mcpAppTokenHeader, http.StatusUnauthorized)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// handleMcpAppCalls: a wrapper waits for work.
func handleMcpAppCalls(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !mcpAppWrapperAuth(w, r) {
		return
	}
	id := r.URL.Query().Get("wrapper")
	if id == "" || len(id) > 128 {
		http.Error(w, "wrapper is required", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, mcpAppCalls.take(id, mcpAppPollWait, r.Context().Done()))
}

// handleMcpAppCallResult: a wrapper answers one call.
func handleMcpAppCallResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !mcpAppWrapperAuth(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, mcpAppCallsPath+"/")
	var a mcpAppCallAnswer
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, mcpAppMaxBody)).Decode(&a); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !mcpAppCalls.answer(id, a) {
		http.Error(w, "no such call", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// mcpAppCallBody is the chat window's request to run a tool for a page.
type mcpAppCallBody struct {
	Wrapper   string          `json:"wrapper"`
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// handleMcpAppCall: the chat window runs a tool for a page.
func handleMcpAppCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !mcpAppFromChatPage(w, r, true) {
		return
	}
	var b mcpAppCallBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, mcpAppMaxBody)).Decode(&b); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if b.Tool == "" {
		http.Error(w, "tool is required", http.StatusBadRequest)
		return
	}
	a, ok := mcpAppCalls.call(b.Wrapper, b.Tool, b.Arguments, r.Context().Done())
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, mcpAppCallAnswer{Error: &wrapCallError{Code: -32000, Message: "tool program not running"}})
		return
	}
	if c := mcpAppCallRecord(b, a); c != nil {
		c.Line = mcpAppCallLine(c)
		bus.Publish(Event{Type: "mcpAppCall", AppCall: c})
		bus.AddAgentNote(mcpAppCallLine(c))
	}
	writeJSON(w, http.StatusOK, a)
}

// McpAppCall is the payload of an "mcpAppCall" event (step 2 phase 4): a page
// ran one of its tools. Shown as a line in the chat, and told to the agent
// with the user's next message.
type McpAppCall struct {
	Server    string          `json:"server,omitempty"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Summary   string          `json:"summary"` // first paragraph of the answer's text, cut to mcpAppCallSummaryMax
	IsError   bool            `json:"isError,omitempty"`
	Line      string          `json:"line"` // mcpAppCallLine, for the chat window
}

const mcpAppCallSummaryMax = 300

// mcpAppCallRecord turns an answer into a record, or nil when the tool never
// ran (the wrapper refused it, or the tool program is gone).
func mcpAppCallRecord(b mcpAppCallBody, a mcpAppCallAnswer) *McpAppCall {
	c := &McpAppCall{Server: b.Server, Tool: b.Tool, Arguments: b.Arguments}
	if a.Error != nil {
		if a.Error.Code == -32602 || a.Error.Code == -32000 {
			return nil
		}
		c.IsError = true
		c.Summary = a.Error.Message
	} else {
		var r struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		json.Unmarshal(a.Result, &r)
		c.IsError = r.IsError
		for _, p := range r.Content {
			if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
				// The first paragraph: tools often add a table or details after it.
				c.Summary = strings.SplitN(strings.TrimSpace(p.Text), "\n\n", 2)[0]
				break
			}
		}
	}
	c.Summary = mcpAppCut(strings.Join(strings.Fields(c.Summary), " "), mcpAppCallSummaryMax)
	return c
}

func mcpAppCut(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

// mcpAppCallLine is the one-line description used in the chat, the chat log
// and the agent's note:
// 📱 `cal` page ran `book_slot` (day: Tue 24, person: Bob): Pencilled in…
func mcpAppCallLine(c *McpAppCall) string {
	var b strings.Builder
	b.WriteString("📱 ")
	if c.Server != "" {
		b.WriteString("`" + strings.ReplaceAll(c.Server, "`", "") + "` page")
	} else {
		b.WriteString("A page")
	}
	b.WriteString(" ran `" + strings.ReplaceAll(c.Tool, "`", "") + "`")
	if args := mcpAppArgsText(c.Arguments); args != "" {
		b.WriteString(" (" + args + ")")
	}
	b.WriteString(": ")
	if c.IsError {
		b.WriteString("error: ")
	}
	if c.Summary == "" {
		b.WriteString("done")
	} else {
		b.WriteString(c.Summary)
	}
	return b.String()
}

// mcpAppArgsText shows tool arguments as "key: value, …" (keys sorted),
// cut to 120 characters.
func mcpAppArgsText(raw json.RawMessage) string {
	var m map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := m[k]
		var s string
		switch x := v.(type) {
		case string:
			s = x
		default:
			bs, _ := json.Marshal(x)
			s = string(bs)
		}
		parts = append(parts, k+": "+s)
	}
	return mcpAppCut(strings.Join(strings.Fields(strings.Join(parts, ", ")), " "), 120)
}
