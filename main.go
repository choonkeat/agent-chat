package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
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

var bus = NewEventBus()

// bootID is a unique identifier for this MCP server instance.
// It is sent to the browser in the WebSocket handshake and used to
// locate the Claude Code session JSONL file that contains this ID.
var bootID = uuid.New().String()

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
	noStdio := flag.Bool("no-stdio-mcp", false, "disable stdio MCP transport (HTTP MCP is always available)")
	flag.Parse()

	// Top-level context cancelled on shutdown — all goroutines should use this.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-chat",
		Version: "0.1.0",
	}, nil)
	mcpServerRef = server
	registerTools(server, bus, ctx)
	registerResources(server)

	// Always start HTTP server eagerly
	if err := ensureHTTPServer(); err != nil {
		log.Fatalf("failed to start HTTP server: %v", err)
	}

	if !*noStdio {
		// Run MCP over stdio (blocks until client disconnects)
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatalf("mcp server error: %v", err)
		}
	} else {
		// No stdio — block until signal
		fmt.Fprintf(os.Stderr, "Running in HTTP-only mode (no stdio MCP). Press Ctrl+C to stop.\n")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
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

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Send connected handshake with history for reconnect replay
	history, pendingAckID := bus.History()
	connectMsg := map[string]any{"type": "connected", "bootID": bootID}
	if len(history) > 0 {
		connectMsg["history"] = history
	}
	if pendingAckID != "" {
		connectMsg["pendingAckId"] = pendingAckID
	}
	conn.WriteJSON(connectMsg)

	// Subscribe to event bus
	sub := bus.Subscribe()
	defer bus.Unsubscribe(sub)

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
			Type    string `json:"type"`
			Text    string `json:"text"`
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		if json.Unmarshal(msg, &m) != nil {
			continue
		}
		switch m.Type {
		case "message":
			if m.Text != "" {
				bus.PushMessage(m.Text)
				bus.LogUserMessage(m.Text)
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
				bus.LogUserMessage(m.Message)
			}
		}
	}
}
