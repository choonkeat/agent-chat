package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed client-dist
var staticFS embed.FS

var bus *EventBus

// version and commit are set at build time via -ldflags.
var version = "dev"
var commit = "unknown"

// themeCookieName is the cookie the browser reads for light/dark theme.
var themeCookieName string

// uploadDir is the directory for uploaded files.
var uploadDir string

// autocompleteURL is a legacy flag: external HTTP endpoint used as fallback URL
// for trigger entries that don't specify their own URL.
var autocompleteURL string

// autocompleteTriggers is the raw flag value (e.g. "/=http://host/api,@=filepath").
var autocompleteTriggers string

// welcomeReplies are the hardcoded quick-reply chips shown on a genuinely empty
// chat (zero events) so the opening state signals "your turn" instead of looking
// frozen. They vanish the moment the agent sends its first message (with its own
// context-aware replies) or any other history exists. Overridable via the
// -welcome-replies flag; set to "" to disable.
var welcomeReplies []string

// triggerMap is the resolved flat map of trigger character → URL.
// A URL of "builtin:filepath" signals the built-in filepath handler.
// Populated by parseTriggerConfig at startup.
var triggerMap map[string]string

// filepathRoot is the directory used by the built-in filepath autocompleter.
var filepathRoot = "."

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// uiURL is set once the HTTP server starts, used in tool results.
var uiURL string

// browserOpened tracks whether we've already opened a browser this session.
var browserOpened bool

// httpMu guards httpRunning and httpListener for crash-recovery restarts.
var httpMu sync.Mutex
var httpRunning bool
var httpListener net.Listener

// mcpServerRef holds a reference to the MCP server for lazy HTTP startup.
var mcpServerRef *mcp.Server

// channelInterceptorRef holds the channel interceptor for permission handling.
var channelInterceptorRef *channelInterceptor

// nopWriteCloser wraps an io.Writer with a no-op Close method.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// ensureHTTPServer lazily starts the HTTP server and opens the browser.
// If the server has crashed since the last call, it restarts automatically.
func ensureHTTPServer() error {
	httpMu.Lock()
	defer httpMu.Unlock()
	if httpRunning {
		return nil
	}
	url, ln, err := startHTTPServer(mcpServerRef)
	if err != nil {
		return err
	}
	uiURL = url
	httpListener = ln
	httpRunning = true
	fmt.Fprintf(os.Stderr, "Agent Chat UI: %s\n", uiURL)
	fmt.Fprintf(os.Stderr, "MCP endpoint: POST %s/mcp\n", uiURL)
	openBrowser(uiURL)
	browserOpened = true
	return nil
}

// parseWelcomeReplies splits the -welcome-replies flag into trimmed, non-empty
// chips. An empty/whitespace-only flag disables welcome replies entirely.
func parseWelcomeReplies(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func main() {
	showVersion := flag.Bool("v", false, "print version and exit")
	noStdio := flag.Bool("no-stdio-mcp", false, "disable stdio MCP transport (HTTP MCP is always available)")
	flag.StringVar(&themeCookieName, "theme-cookie", "agent-chat-theme", "cookie name for light/dark theme toggle")
	flag.StringVar(&uploadDir, "upload-dir", "", "directory for uploaded files (default: temp dir)")
	flag.StringVar(&autocompleteURL, "autocomplete-url", "", "legacy: fallback URL for triggers without an explicit URL")
	flag.StringVar(&autocompleteTriggers, "autocomplete-triggers", "", "trigger characters mapped to URLs (e.g. '/=http://host/api')")
	defaultWelcome := "What can you help me with?,Give me an overview of this project,What's changed recently?"
	welcomeRepliesFlag := flag.String("welcome-replies", defaultWelcome, "comma-separated quick replies shown on an empty chat ('' to disable)")
	flag.Parse()

	welcomeReplies = parseWelcomeReplies(*welcomeRepliesFlag)

	if *showVersion {
		fmt.Printf("agent-chat %s (%s)\n", version, commit)
		os.Exit(0)
	}

	// Set up upload directory
	if uploadDir == "" {
		dir, err := os.MkdirTemp("", "agent-chat-uploads-*")
		if err != nil {
			log.Fatalf("failed to create temp upload dir: %v", err)
		}
		uploadDir = dir
	} else {
		if !filepath.IsAbs(uploadDir) {
			wd, _ := os.Getwd()
			uploadDir = filepath.Join(wd, uploadDir)
		}
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			log.Fatalf("failed to create upload dir %s: %v", uploadDir, err)
		}
	}

	// Initialize event bus, optionally with JSONL file logging.
	if logPath := os.Getenv("AGENT_CHAT_EVENT_LOG"); logPath != "" {
		var err error
		bus, err = NewEventBusWithLog(logPath)
		if err != nil {
			log.Printf("Warning: failed to open event log %s: %v (falling back to in-memory only)", logPath, err)
			bus = NewEventBus()
		}
	} else {
		bus = NewEventBus()
	}
	defer bus.Close()

	// Top-level context cancelled on shutdown — all goroutines should use this.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGHUP (and INT/TERM) so we exit gracefully in all modes.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		<-sig
		cancel()
	}()

	disabled := os.Getenv("AGENT_CHAT_DISABLE") != ""

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-chat",
		Version: version,
	}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{
			Experimental: map[string]any{
				"claude/channel":            map[string]any{},
				"claude/channel/permission": map[string]any{},
			},
		},
	})
	mcpServerRef = server
	if !disabled {
		registerTools(server, bus)
		registerResources(server)

		if err := ensureHTTPServer(); err != nil {
			log.Fatalf("failed to start HTTP server: %v", err)
		}
	}

	// Channel interceptor sits between real stdin and the MCP SDK,
	// handling Claude Code channel notifications (e.g. permission prompts).
	channelInterceptorRef = newChannelInterceptor(bus)

	if !*noStdio {
		// Run MCP over intercepted stdio (blocks until client disconnects)
		transport := &mcp.IOTransport{
			Reader: channelInterceptorRef.pipeReader,
			Writer: nopWriteCloser{os.Stdout},
		}
		if err := server.Run(ctx, transport); err != nil {
			log.Fatalf("mcp server error: %v", err)
		}
	} else {
		// No stdio — block until signal cancels context
		fmt.Fprintf(os.Stderr, "Running in HTTP-only mode (no stdio MCP). Press Ctrl+C to stop.\n")
		<-ctx.Done()
	}
}

// startHTTPServer starts the HTTP server with the browser UI, WebSocket endpoint,
// and StreamableHTTP MCP endpoint. Returns the base URL and the listener.
func startHTTPServer(mcpServer *mcp.Server) (string, net.Listener, error) {
	staticSub, err := fs.Sub(staticFS, "client-dist")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create sub filesystem: %w", err)
	}
	fileServer := http.FileServer(http.FS(staticSub))

	// StreamableHTTP MCP handler
	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})

	// Orchestrator MCP server — separate from the agent-facing MCP server.
	// Exposes tools for external callers (e.g. swe-swe server) to push messages
	// and read chat history without interfering with the agent's message queue.
	orchServer := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-chat-orchestrator",
		Version: version,
	}, nil)
	registerOrchestratorTools(orchServer, bus)
	orchHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return orchServer
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/mcp/orchestrator", orchHandler)
	mux.HandleFunc("/ws", handleWebSocket)
	mux.HandleFunc("/upload", handleUpload)
	mux.HandleFunc("/api/export", handleExport)
	mux.HandleFunc("/autocomplete", handleAutocomplete)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))
	// Serve index.html with inlined config (replaces the old /config.js endpoint).
	// This avoids relative-path resolution failures when the page is served
	// behind a reverse proxy at a non-root path (e.g. /session/UUID).
	indexHTML, _ := fs.ReadFile(staticSub, "index.html")
	triggerMap = buildTriggerMap(autocompleteTriggers, autocompleteURL)
	triggerCharsJSON, _ := json.Marshal(triggerChars(triggerMap))
	configScript := fmt.Sprintf("<script>var THEME_COOKIE_NAME=%q,SERVER_VERSION=%q,AUTOCOMPLETE_TRIGGERS=%s;</script>",
		themeCookieName, version+" ("+commit+")", string(triggerCharsJSON))
	indexPage := strings.Replace(string(indexHTML), "<!--CONFIG-->", configScript, 1)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, indexPage)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	port := 0
	if s := os.Getenv("AGENT_CHAT_PORT"); s != "" {
		port, _ = strconv.Atoi(s)
	} else if s := os.Getenv("PORT"); s != "" {
		port, _ = strconv.Atoi(s)
	}
	addr := "0.0.0.0:0"
	if port > 0 {
		addr = fmt.Sprintf("0.0.0.0:%d", port)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("listen error: %w", err)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	go func() {
		http.Serve(ln, mux)
		// Server stopped — mark as not running so next call restarts it
		httpMu.Lock()
		httpRunning = false
		httpMu.Unlock()
	}()

	return fmt.Sprintf("http://localhost:%d", actualPort), ln, nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		cmd = exec.Command("cmd", "/c", "start", url)
	}
	cmd.Start() // fire and forget
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit request body to 50MB
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "file too large or invalid multipart form", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "no files provided", http.StatusBadRequest)
		return
	}

	var refs []FileRef
	for _, fh := range files {
		ref, err := saveUploadedFile(fh)
		if err != nil {
			http.Error(w, "failed to save file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		refs = append(refs, ref)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(refs)
}

// maxExportBytes caps the size of a posted export to prevent abuse.
const maxExportBytes = 200 << 20 // 200MB

// handleExport receives a rendered HTML export from a connected browser and
// resolves the matching pending export. The browser sends the bytes as the
// raw request body, with the token in the query string. If the browser
// reports a render error instead, it sets ?error=1 and the body is the error
// message.
func handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxExportBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	result := ExportResult{}
	if r.URL.Query().Get("error") == "1" {
		result.Error = string(body)
	} else {
		result.HTML = body
	}
	if !bus.ResolveExport(token, result) {
		http.Error(w, "unknown or already-resolved token", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func saveUploadedFile(fh *multipart.FileHeader) (FileRef, error) {
	src, err := fh.Open()
	if err != nil {
		return FileRef{}, err
	}
	defer src.Close()

	prefix := uuid.New().String()[:8]
	savedName := prefix + "-" + fh.Filename
	destPath := filepath.Join(uploadDir, savedName)

	dst, err := os.Create(destPath)
	if err != nil {
		return FileRef{}, err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return FileRef{}, err
	}

	return FileRef{
		Name: fh.Filename,
		Path: destPath,
		URL:  "/uploads/" + savedName,
		Size: fh.Size,
		Type: fh.Header.Get("Content-Type"),
	}, nil
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Read cursor from query param — client sends last seen seq number.
	cursor := int64(0)
	if s := r.URL.Query().Get("cursor"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			cursor = v
		}
	}

	// Send connected handshake (no history array — we stream events after).
	connectMsg := map[string]any{"type": "connected", "version": version + " (" + commit + ")"}
	if pendingAckID := bus.PendingAckID(); pendingAckID != "" {
		connectMsg["pendingAckId"] = pendingAckID
	}
	if qr := bus.LastQuickReplies(); len(qr) > 0 {
		connectMsg["quickReplies"] = qr
	} else if len(welcomeReplies) > 0 && !bus.HasHistory() {
		// Genuinely empty chat: seed welcome replies so the opening state
		// signals "your turn" instead of looking frozen. Suppressed once any
		// history exists (including a send_progress-only opening).
		connectMsg["quickReplies"] = welcomeReplies
	}
	conn.WriteJSON(connectMsg)

	// Subscribe to event bus BEFORE streaming history to avoid gaps.
	sub := bus.Subscribe()
	defer bus.Unsubscribe(sub)

	// Stream missed events (seq > cursor) to the client individually.
	missed := bus.EventsSince(cursor)
	for _, event := range missed {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
	// Signal end of history replay so the client can finalize UI state.
	conn.WriteJSON(map[string]any{"type": "historyEnd"})

	// Track the highest seq we've sent so the subscriber loop can skip duplicates.
	highSeq := cursor
	if len(missed) > 0 {
		highSeq = missed[len(missed)-1].Seq
	}

	// writeCh allows the read loop to send messages back to the client.
	// Only the writer goroutine writes to the WebSocket connection.
	writeCh := make(chan any, 16)

	// Register writeCh as a transient broadcast sink so non-logged messages
	// (e.g. exportRequest) reach this connection.
	bus.SubscribeTransient(writeCh)
	defer bus.UnsubscribeTransient(writeCh)

	// Forward events to WebSocket client
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case event, ok := <-sub:
				if !ok {
					return
				}
				// Skip events already sent via the history stream.
				if event.Seq <= highSeq {
					continue
				}
				data, err := json.Marshal(event)
				if err != nil {
					continue
				}
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			case msg, ok := <-writeCh:
				if !ok {
					return
				}
				data, err := json.Marshal(msg)
				if err != nil {
					continue
				}
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			}
		}
	}()

	// Read incoming messages
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var m struct {
			Type    string    `json:"type"`
			Text    string    `json:"text"`
			Files   []FileRef `json:"files"`
			ID      string    `json:"id"`
			Message string    `json:"message"`
		}
		if json.Unmarshal(msg, &m) != nil {
			continue
		}
		switch m.Type {
		case "message":
			if m.Text != "" || len(m.Files) > 0 {
				// Check if this is a response to a pending permission prompt.
				consumed := false
				if channelInterceptorRef != nil && len(m.Files) == 0 {
					consumed = channelInterceptorRef.HandleUserResponse(m.Text)
				}
				if consumed {
					// Permission response handled — broadcast as userMessage for
					// display, then immediately mark consumed (the message never
					// hits the agent's queue).
					bus.PublishConsumedUserMessage(m.Text, nil)
				} else {
					// ReceiveUserMessage publishes the userMessage event BEFORE
					// queuing so browsers always see the bubble before any
					// consumption signal that the agent may race-fire.
					bus.ReceiveUserMessage(m.Text, m.Files)
					// Notify browser that message is queued — it waits for this
					// before telling the parent frame to call check_messages.
					select {
					case writeCh <- map[string]string{"type": "messageQueued"}:
					default:
					}
				}
			}
		case "ack":
			if m.ID != "" {
				result := "ack"
				if m.Message != "" {
					result = "ack:" + m.Message
				}
				bus.ResolveAck(m.ID, result)
				// Broadcast ack reply as a userMessage to all browsers; the ack
				// itself is the "agent received it" signal, so emit consumed
				// immediately too.
				bus.PublishConsumedUserMessage(m.Message, nil)
			}
		case "unsend":
			// User clicked × on a pending bubble — withdraw it from the queue
			// before the agent sees it. Broadcast deletion so every tab drops
			// the bubble; if the message was already drained, tell the sender.
			if m.ID == "" {
				break
			}
			if bus.RemoveFromQueue(m.ID) {
				bus.Publish(Event{Type: "userMessageDeleted", ID: m.ID})
			} else {
				select {
				case writeCh <- map[string]any{"type": "unsendFailed", "id": m.ID}:
				default:
				}
			}
		}
	}
}

// buildTriggerMap builds the flat trigger-char → URL map from command-line flags.
// Default: "@" → "builtin:filepath". The triggers flag adds/overrides entries.
//
// New format: CHAR=URL (e.g. "/=http://host/api").
// Legacy format: CHAR=TYPE=URL or CHAR=TYPE (type is ignored; if no URL,
// the fallbackURL is used).
func buildTriggerMap(triggers, fallbackURL string) map[string]string {
	m := map[string]string{"@": "builtin:filepath", ":": "builtin:emoji"}
	if triggers == "" {
		return m
	}
	for _, part := range strings.Split(triggers, ",") {
		parts := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(parts) < 2 {
			continue
		}
		char := strings.TrimSpace(parts[0])
		rest := strings.TrimSpace(parts[1])
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") || strings.HasPrefix(rest, "builtin:") {
			// New format: CHAR=URL
			m[char] = rest
		} else {
			// Legacy format: CHAR=TYPE or CHAR=TYPE=URL
			legacy := strings.SplitN(rest, "=", 2)
			if len(legacy) == 2 {
				m[char] = strings.TrimSpace(legacy[1])
			} else if fallbackURL != "" {
				m[char] = fallbackURL
			}
			// else: no URL available, skip (char won't be registered)
		}
	}
	return m
}

// triggerChars returns the list of trigger characters from a trigger map.
func triggerChars(m map[string]string) []string {
	chars := make([]string, 0, len(m))
	for ch := range m {
		chars = append(chars, ch)
	}
	sort.Strings(chars)
	return chars
}

// autocompleteItem is a single autocomplete result with a value and optional hint.
type autocompleteItem struct {
	V string `json:"v"`
	H string `json:"h,omitempty"`
}

// autocompleteResponse is the structured response from /autocomplete.
type autocompleteResponse struct {
	Results        []autocompleteItem `json:"results"`
	Info           string             `json:"info,omitempty"`
	HasMore        bool               `json:"has_more,omitempty"`
	ReplaceTrigger bool               `json:"replace_trigger,omitempty"`
}

// handleAutocomplete looks up the trigger character in the flat map and either
// delegates to a built-in handler or proxies to the configured URL.
func handleAutocomplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	var req struct {
		Trigger string `json:"trigger"`
		Query   string `json:"query"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	providerURL := triggerMap[req.Trigger]

	// Built-in emoji handler.
	if providerURL == "builtin:emoji" {
		results, hasMore := builtinEmojiComplete(req.Query)
		info := ""
		if len(results) == 0 {
			info = fmt.Sprintf("No emoji matching %q", req.Query)
		}
		writeAutocompleteResponse(w, results, info, hasMore, true)
		return
	}

	// Built-in filepath handler.
	if providerURL == "builtin:filepath" {
		root := filepathRoot
		if strings.HasPrefix(req.Query, "/") {
			root = "/"
		}
		absRoot, _ := filepath.Abs(root)
		results, hasMore := builtinFilepathComplete(root, req.Query)
		info := ""
		if len(results) == 0 {
			info = fmt.Sprintf("No files matching %q in %s", req.Query, absRoot)
		}
		writeAutocompleteResponse(w, stringsToItems(results), info, hasMore, false)
		return
	}

	// External URL proxy.
	if providerURL != "" {
		proxyBody, _ := json.Marshal(map[string]string{"query": req.Query})
		resp, err := http.Post(providerURL, "application/json", bytes.NewReader(proxyBody))
		if err != nil {
			http.Error(w, "autocomplete upstream error: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		upstreamBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		var items []autocompleteItem
		hasMore := false
		replaceTrigger := false
		var structured struct {
			Results        json.RawMessage `json:"results"`
			HasMore        bool            `json:"has_more"`
			ReplaceTrigger bool            `json:"replace_trigger"`
		}
		if json.Unmarshal(upstreamBody, &structured) == nil && len(structured.Results) > 0 {
			hasMore = structured.HasMore
			replaceTrigger = structured.ReplaceTrigger
			if json.Unmarshal(structured.Results, &items) != nil {
				var strings []string
				if json.Unmarshal(structured.Results, &strings) == nil {
					items = stringsToItems(strings)
				}
			}
		} else if json.Unmarshal(upstreamBody, &items) != nil {
			var strings []string
			if json.Unmarshal(upstreamBody, &strings) == nil {
				items = stringsToItems(strings)
			}
		}
		writeAutocompleteResponse(w, items, "", hasMore, replaceTrigger)
		return
	}

	writeAutocompleteResponse(w, []autocompleteItem{}, "", false, false)
}

func writeAutocompleteResponse(w http.ResponseWriter, results []autocompleteItem, info string, hasMore bool, replaceTrigger bool) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(autocompleteResponse{Results: results, Info: info, HasMore: hasMore, ReplaceTrigger: replaceTrigger})
}

// stringsToItems converts plain string results to autocompleteItems.
func stringsToItems(ss []string) []autocompleteItem {
	items := make([]autocompleteItem, len(ss))
	for i, s := range ss {
		items[i] = autocompleteItem{V: s}
	}
	return items
}

// builtinFilepathComplete returns file paths under root that fuzzy-match the query,
// sorted by match quality (lower score = better match). Collects up to 500 candidates,
// scores them, and returns the top 50.
func builtinFilepathComplete(root, query string) ([]string, bool) {
	type scored struct {
		path  string
		score int
	}
	// If query contains "/.", the user is explicitly targeting hidden dirs.
	skipHidden := !strings.Contains(query, "/.")
	var candidates []scored
	walkCapped := false
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Skip the root entry itself (before the hidden-dir check,
		// because "." has a dot prefix but is not a hidden directory).
		if path == root {
			return nil
		}
		if skipHidden && d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if s, ok := fuzzyScorePath(path, query); ok {
			candidates = append(candidates, scored{path, s})
		}
		if len(candidates) >= 500 {
			walkCapped = true
			return filepath.SkipAll
		}
		return nil
	})
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score < candidates[j].score
		}
		return candidates[i].path < candidates[j].path
	})
	limit := 50
	if len(candidates) < limit {
		limit = len(candidates)
	}
	hasMore := walkCapped || len(candidates) > limit
	results := make([]string, limit)
	for i := 0; i < limit; i++ {
		results[i] = candidates[i].path
	}
	return results, hasMore
}

// fuzzyScorePath checks if all query characters appear in s in order
// (case-insensitive) and returns a composite score indicating match quality.
// Lower scores are better matches.
//
// Tiers (lower = better) are encoded into the high bits of the score so a
// single int comparison ranks by tier first, then within-tier by
// (longestRun desc, span asc, length asc):
//
//	tier 0 = path contains query as a contiguous substring
//	tier 1 = path fuzzy-matches (subsequence) only
//
// Within a tier, longestRun (longest block of query chars landing on
// consecutive positions in the path) wins, then a tighter span, then a
// shorter overall path. This is the conventional fzf-style heuristic.
func fuzzyScorePath(s, query string) (int, bool) {
	if query == "" {
		return 0, true
	}
	ls := strings.ToLower(s)
	lq := strings.ToLower(query)
	qLen := len(lq)
	sLen := len(ls)
	qi := 0
	first := -1
	last := -1
	longest := 0
	current := 0
	lastMatch := -2
	for i := 0; i < sLen && qi < qLen; i++ {
		if ls[i] == lq[qi] {
			if first < 0 {
				first = i
			}
			last = i
			if i == lastMatch+1 {
				current++
			} else {
				current = 1
			}
			if current > longest {
				longest = current
			}
			lastMatch = i
			qi++
		}
	}
	if qi < qLen {
		return 0, false
	}
	span := last - first
	tier := 1
	if strings.Contains(ls, lq) {
		tier = 0
		// Contiguous match: longestRun and span are determined by qLen.
		longest = qLen
		span = qLen - 1
	}
	// Encode (tier, -longestRun, span, sLen) into a single comparable int.
	// Caps below are loose upper bounds; sLen is bounded by walkCapped paths.
	const (
		spanRange = 1 << 16
		runRange  = 1 << 12
		lenRange  = 1 << 16
	)
	score := tier*runRange*spanRange*lenRange +
		(runRange-1-min(longest, runRange-1))*spanRange*lenRange +
		min(span, spanRange-1)*lenRange +
		min(sLen, lenRange-1)
	return score, true
}

// fuzzyMatchPath checks if all query characters appear in s in order (case-insensitive).
func fuzzyMatchPath(s, query string) bool {
	_, ok := fuzzyScorePath(s, query)
	return ok
}
