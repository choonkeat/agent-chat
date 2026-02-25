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

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/ws", handleWebSocket)
	mux.HandleFunc("/upload", handleUpload)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))
	mux.HandleFunc("/config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, "var THEME_COOKIE_NAME = %q;\n", themeCookieName)
	})
	mux.Handle("/", fileServer)

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
	connectMsg := map[string]any{"type": "connected"}
	if pendingAckID := bus.PendingAckID(); pendingAckID != "" {
		connectMsg["pendingAckId"] = pendingAckID
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
