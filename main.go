package main

import (
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

// autocompleteURL is the external HTTP endpoint to proxy autocomplete requests to.
var autocompleteURL string

// autocompleteTriggers maps trigger characters to type names (e.g. "/=slash-command,@=filepath").
var autocompleteTriggers string

// triggerURLs maps autocomplete type names to per-trigger provider URLs.
// Populated by parseTriggerConfig at startup.
var triggerURLs map[string]string

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

func main() {
	showVersion := flag.Bool("v", false, "print version and exit")
	noStdio := flag.Bool("no-stdio-mcp", false, "disable stdio MCP transport (HTTP MCP is always available)")
	flag.StringVar(&themeCookieName, "theme-cookie", "agent-chat-theme", "cookie name for light/dark theme toggle")
	flag.StringVar(&uploadDir, "upload-dir", "", "directory for uploaded files (default: temp dir)")
	flag.StringVar(&autocompleteURL, "autocomplete-url", "", "external HTTP endpoint for autocomplete suggestions")
	flag.StringVar(&autocompleteTriggers, "autocomplete-triggers", "", "trigger characters mapped to types (e.g. '/=slash-command,@=filepath')")
	flag.Parse()

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
	}, nil)
	mcpServerRef = server
	if !disabled {
		registerTools(server, bus)
		registerResources(server)

		if err := ensureHTTPServer(); err != nil {
			log.Fatalf("failed to start HTTP server: %v", err)
		}
	}

	if !*noStdio {
		// Run MCP over stdio (blocks until client disconnects)
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
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
	mux.HandleFunc("/autocomplete", handleAutocomplete)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))
	// Serve index.html with inlined config (replaces the old /config.js endpoint).
	// This avoids relative-path resolution failures when the page is served
	// behind a reverse proxy at a non-root path (e.g. /session/UUID).
	indexHTML, _ := fs.ReadFile(staticSub, "index.html")
	triggers := autocompleteTriggers
	if triggers == "" {
		triggers = "@=filepath"
	}
	triggerJSON, urls := parseTriggerConfig(triggers)
	triggerURLs = urls
	configScript := fmt.Sprintf("<script>var THEME_COOKIE_NAME=%q,SERVER_VERSION=%q,AUTOCOMPLETE_TRIGGERS=%s;</script>",
		themeCookieName, version+" ("+commit+")", triggerJSON)
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
				bus.PushMessage(m.Text, m.Files)
				// Broadcast userMessage to all connected browsers (including sender)
				// so every tab shows the bubble. Also appends to event log for reconnect replay.
				bus.Publish(Event{Type: "userMessage", Text: m.Text, Files: m.Files})
				// Notify browser that message is queued — it waits for this
				// before telling the parent frame to call check_messages.
				select {
				case writeCh <- map[string]string{"type": "messageQueued"}:
				default:
				}
			}
		case "ack":
			if m.ID != "" {
				result := "ack"
				if m.Message != "" {
					result = "ack:" + m.Message
				}
				bus.ResolveAck(m.ID, result)
				// Broadcast ack reply as a userMessage to all browsers.
				bus.Publish(Event{Type: "userMessage", Text: m.Message})
			}
		}
	}
}

// parseTriggerConfig parses trigger flag strings like "/=slash-command,@=filepath=http://host/path"
// into client-side JSON ({"char":"type",...}) and a server-side map of type→URL for per-trigger routing.
// Returns "{}" and nil if the input is empty.
func parseTriggerConfig(s string) (string, map[string]string) {
	if s == "" {
		return "{}", nil
	}
	clientMap := make(map[string]string)
	urlMap := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		parts := strings.SplitN(strings.TrimSpace(part), "=", 3)
		if len(parts) < 2 {
			continue
		}
		char := strings.TrimSpace(parts[0])
		typeName := strings.TrimSpace(parts[1])
		clientMap[char] = typeName
		if len(parts) == 3 {
			urlMap[typeName] = strings.TrimSpace(parts[2])
		}
	}
	b, _ := json.Marshal(clientMap)
	return string(b), urlMap
}

// autocompleteResponse is the structured response from /autocomplete.
type autocompleteResponse struct {
	Results []string `json:"results"`
	Info    string   `json:"info,omitempty"`
}

// handleAutocomplete routes autocomplete requests to per-trigger URLs, the global
// autocomplete URL, or built-in handlers (e.g. filepath completion).
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
		Type  string `json:"type"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Resolve provider URL: per-trigger URL > global URL > built-in handler.
	providerURL := ""
	if triggerURLs != nil {
		providerURL = triggerURLs[req.Type]
	}
	if providerURL == "" {
		providerURL = autocompleteURL
	}

	if providerURL != "" {
		resp, err := http.Post(providerURL, "application/json", strings.NewReader(string(body)))
		if err != nil {
			http.Error(w, "autocomplete upstream error: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		upstreamBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		// Wrap upstream array in structured format.
		var results []string
		if json.Unmarshal(upstreamBody, &results) != nil {
			results = []string{}
		}
		writeAutocompleteResponse(w, results, "")
		return
	}

	// Built-in handlers.
	if req.Type == "filepath" {
		root, _ := filepath.Abs(filepathRoot)
		results := builtinFilepathComplete(filepathRoot, req.Query)
		info := ""
		if len(results) == 0 {
			info = fmt.Sprintf("No files matching %q in %s", req.Query, root)
		}
		writeAutocompleteResponse(w, results, info)
		return
	}

	writeAutocompleteResponse(w, []string{}, "")
}

func writeAutocompleteResponse(w http.ResponseWriter, results []string, info string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(autocompleteResponse{Results: results, Info: info})
}

// builtinFilepathComplete returns file paths under root that fuzzy-match the query.
func builtinFilepathComplete(root, query string) []string {
	var results []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Skip the root entry itself (before the hidden-dir check,
		// because "." has a dot prefix but is not a hidden directory).
		if path == root {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if fuzzyMatchPath(path, query) {
			results = append(results, path)
		}
		if len(results) >= 50 {
			return filepath.SkipAll
		}
		return nil
	})
	if results == nil {
		results = []string{}
	}
	return results
}

// fuzzyMatchPath checks if all query characters appear in s in order (case-insensitive).
func fuzzyMatchPath(s, query string) bool {
	if query == "" {
		return true
	}
	ls := strings.ToLower(s)
	lq := strings.ToLower(query)
	qi := 0
	for i := 0; i < len(ls) && qi < len(lq); i++ {
		if ls[i] == lq[qi] {
			qi++
		}
	}
	return qi == len(lq)
}
