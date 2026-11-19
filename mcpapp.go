package main

// Phase 3 of tasks/2026-11-17-mcp-apps-step1.md: agent-chat receives MCP Apps
// pages from `agent-chat wrap` (wrapapps.go) on POST /api/mcp-app and
// publishes them as "mcpApp" chat events.
//
// Who may post: only processes on this machine (loopback), and only with the
// password ("token") this agent-chat writes at startup to a file only the
// user's account can read: <user cache dir>/agent-chat/mcp-app-tokens/<port>.
// The wrapper reads that file for the port it posts to. Claude Code starts the
// wrapper and agent-chat separately, so a file is the one place both can
// find it without a swe-swe change.
//
// Pages are stored once, by content hash, under
// <user cache dir>/agent-chat/mcp-app-pages/, so the event log and every
// history replay carry a short id instead of the whole page, and pages
// survive an agent-chat restart. They are served back as text/plain with a
// sandbox CSP: the browser must only ever run them inside the chat's
// sandboxed iframe, never as a page on agent-chat's own origin.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const (
	mcpAppTokenHeader = "X-Agent-Chat-Token"
	mcpAppMaxBody     = 4 << 20
	mcpAppPagePrefix  = "/api/mcp-app/page/"
)

// McpApp is the payload of an "mcpApp" event: one tool page to show, with the
// tool call that produced it.
type McpApp struct {
	ID           string          `json:"id"`
	Server       string          `json:"server"`
	Tool         string          `json:"tool"`
	Arguments    json.RawMessage `json:"arguments,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	ResourceURI  string          `json:"resourceUri"`
	PageID       string          `json:"pageId"` // sha256 of the html; fetch from /api/mcp-app/page/<pageId>
	ResourceMeta json.RawMessage `json:"resourceMeta,omitempty"`
	ToolMeta     json.RawMessage `json:"toolMeta,omitempty"`
}

var (
	mcpAppTokenOnce sync.Once
	mcpAppTokenVal  string

	// mcpAppBaseDir is <user cache dir>/agent-chat; tests point it elsewhere.
	mcpAppBaseDir = func() (string, error) {
		dir, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "agent-chat"), nil
	}
)

// mcpAppToken is this process's password, made up once per run.
func mcpAppToken() string {
	mcpAppTokenOnce.Do(func() {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Printf("agent-chat: mcp-app token: %v", err)
			return // empty token: every post is rejected
		}
		mcpAppTokenVal = hex.EncodeToString(b)
	})
	return mcpAppTokenVal
}

func mcpAppTokenPath(port int) (string, error) {
	base, err := mcpAppBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mcp-app-tokens", strconv.Itoa(port)), nil
}

// writeMcpAppToken saves the token where `agent-chat wrap` will look for it.
// The directory is 0700 and the file 0600; it is written to a temp file and
// renamed, so a reader never sees half a token.
func writeMcpAppToken(port int) error {
	tok := mcpAppToken()
	if tok == "" {
		return errors.New("no token")
	}
	path, err := mcpAppTokenPath(port)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil { // MkdirAll leaves an existing dir's mode alone
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(tok + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readMcpAppToken is the wrapper's side: the token for the agent-chat on port.
func readMcpAppToken(port int) (string, error) {
	path, err := mcpAppTokenPath(port)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return tok, nil
}

func mcpAppPagesDir() (string, error) {
	base, err := mcpAppBaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "mcp-app-pages")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// storeMcpAppPage writes html once under its sha256 and returns the hash.
func storeMcpAppPage(html string) (string, error) {
	dir, err := mcpAppPagesDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(html))
	id := hex.EncodeToString(sum[:])
	path := filepath.Join(dir, id+".html")
	if _, err := os.Stat(path); err == nil {
		return id, nil
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(html); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return id, nil
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// handleMcpApp accepts one page from `agent-chat wrap` and publishes it.
func handleMcpApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackRemote(r.RemoteAddr) {
		http.Error(w, "only accepted from this machine", http.StatusForbidden)
		return
	}
	want := mcpAppToken()
	got := r.Header.Get(mcpAppTokenHeader)
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		http.Error(w, "wrong or missing "+mcpAppTokenHeader, http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mcpAppMaxBody)
	var p wrapAppPost
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "page too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch {
	case p.Tool == "":
		http.Error(w, "tool is required", http.StatusBadRequest)
		return
	case p.HTML == "":
		http.Error(w, "html is required", http.StatusBadRequest)
		return
	case p.MimeType != wrapUIMimeType:
		http.Error(w, "mimeType must be "+wrapUIMimeType, http.StatusUnsupportedMediaType)
		return
	case !strings.HasPrefix(p.ResourceURI, "ui://"):
		http.Error(w, "resourceUri must be a ui:// uri", http.StatusBadRequest)
		return
	}
	pageID, err := storeMcpAppPage(p.HTML)
	if err != nil {
		log.Printf("agent-chat: store mcp-app page: %v", err)
		http.Error(w, "could not store page", http.StatusInternalServerError)
		return
	}
	app := &McpApp{
		ID:           uuid.New().String(),
		Server:       p.Server,
		Tool:         p.Tool,
		Arguments:    p.Arguments,
		Result:       p.Result,
		ResourceURI:  p.ResourceURI,
		PageID:       pageID,
		ResourceMeta: p.ResourceMeta,
		ToolMeta:     p.ToolMeta,
	}
	bus.Publish(Event{Type: "mcpApp", App: app})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": app.ID})
}

var mcpAppPageIDRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// handleMcpAppPage serves a stored page as plain text, for the chat to load
// into a sandboxed iframe via srcdoc. Never as text/html: opened directly, it
// would otherwise run with agent-chat's own origin.
func handleMcpAppPage(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, mcpAppPagePrefix)
	if !mcpAppPageIDRe.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	dir, err := mcpAppPagesDir()
	if err != nil {
		http.Error(w, "no page store", http.StatusInternalServerError)
		return
	}
	b, err := os.ReadFile(filepath.Join(dir, id+".html"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, max-age=31536000, immutable") // content-addressed
	w.Write(b)
}

// mcpAppSummary is the chat-log line for a page (the page itself isn't
// embedded in the exported markdown).
func mcpAppSummary(app *McpApp) string {
	name := app.Tool
	if app.Server != "" {
		name = app.Server + "/" + app.Tool
	}
	return "📱 `" + name + "` page shown"
}
