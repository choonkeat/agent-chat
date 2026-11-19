package main

// `agent-chat wrap -- <server command> [args...]` is a pass-through stdio
// wrapper for an MCP server. The agent (e.g. Claude Code) starts it in place of
// the server; it starts the real server as a child and relays newline-delimited
// JSON-RPC both ways, unchanged, except for one edit (phase 1 of
// tasks/2026-11-17-mcp-apps-step1.md; page delivery, phase 2, is in
// wrapapps.go):
//
//   - client→server `initialize`, and any client→server request whose
//     params._meta carries "io.modelcontextprotocol/clientCapabilities" (the
//     2026-07-28 per-request form), gain the MCP Apps extension
//     capabilities.extensions["io.modelcontextprotocol/ui"] — i.e. the wrapper
//     says "I can show pages" on the agent's behalf, so servers that follow the
//     spec's SHOULD-hide rule still attach their pages.
//
// Rule: the wrapper must NEVER break a tool. Any line it can't parse, or
// doesn't need to edit, is forwarded byte-for-byte; an over-long line is
// streamed through unparsed.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	wrapUIExtension = "io.modelcontextprotocol/ui"
	wrapUIMimeType  = "text/html;profile=mcp-app"
	// wrapClientCapsMeta is the params._meta key under which the 2026-07-28
	// protocol carries client capabilities on every request.
	wrapClientCapsMeta = "io.modelcontextprotocol/clientCapabilities"
	// wrapMaxLineBytes caps how much of one line is buffered for parsing
	// (matches swe-swe's mcp-cli-proxy). Longer lines are streamed unedited.
	wrapMaxLineBytes = 16 * 1024 * 1024
)

// wrapEditFunc returns the replacement for one complete line (without its line
// ending), or nil to forward the original bytes untouched.
type wrapEditFunc func(line []byte) []byte

func wrapUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: agent-chat wrap [--name NAME] [--no-advertise-ui] [--agent-chat-url URL] -- <mcp server command> [args...]")
	fmt.Fprintln(w, "  Runs a stdio MCP server, relaying its traffic unchanged except that the")
	fmt.Fprintln(w, "  client's capabilities also advertise MCP Apps (\"I can show pages\").")
	fmt.Fprintln(w, "  When a tool with an MCP Apps page answers, the page and the answer are")
	fmt.Fprintln(w, "  also sent to agent-chat, which shows the page in the chat.")
}

// runWrap is the `agent-chat wrap` entry point. stdin/stdout are the agent's
// side of the stdio MCP connection; the child's stderr goes to stderr.
func runWrap(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("wrap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noAdvertise := fs.Bool("no-advertise-ui", false, "don't add the MCP Apps capability to the client's")
	name := fs.String("name", "", "server name shown with its pages in agent-chat (default: the command's base name)")
	postURL := fs.String("agent-chat-url", "", "where to send pages (default: http://127.0.0.1:$AGENT_CHAT_PORT/api/mcp-app; neither set = relay only)")
	fs.Usage = func() { wrapUsage(stderr); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		wrapUsage(stderr)
		return 2
	}

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stderr = stderr
	childIn, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(stderr, "agent-chat wrap: %v\n", err)
		return 1
	}
	childOut, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(stderr, "agent-chat wrap: %v\n", err)
		return 1
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "agent-chat wrap: start %q: %v\n", cmdArgs[0], err)
		return 127
	}

	// Pass termination signals on to the server, so it can shut down cleanly.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				cmd.Process.Signal(s)
			}
		}
	}()

	toChild := &wrapLineWriter{w: childIn}
	toClient := &wrapLineWriter{w: stdout}

	var advertise wrapEditFunc
	if !*noAdvertise {
		advertise = wrapAdvertiseUI
	}
	toServer, fromServer := wrapEditHook(advertise), wrapHook(nil)

	// Page delivery is on only when there is an agent-chat to deliver to;
	// otherwise the wrapper is a plain relay (plus the capability edit).
	url := *postURL
	if url == "" {
		if port := os.Getenv("AGENT_CHAT_PORT"); port != "" {
			url = "http://127.0.0.1:" + port + "/api/mcp-app"
		}
	}
	var apps *wrapApps
	if url != "" {
		serverName := *name
		if serverName == "" {
			serverName = filepath.Base(cmdArgs[0])
		}
		apps = newWrapApps(serverName, url, toChild, stderr)
		toServer = apps.clientHook(advertise)
		fromServer = apps.serverHook
	}

	// client → server. When the agent closes our stdin, close the server's.
	go func() {
		if err := wrapRelay(stdin, toChild, toServer); err != nil {
			fmt.Fprintf(stderr, "agent-chat wrap: client→server: %v\n", err)
		}
		toChild.mu.Lock()
		childIn.Close()
		toChild.mu.Unlock()
	}()

	// server → client. Must drain before cmd.Wait (it closes the pipe).
	if err := wrapRelay(childOut, toClient, fromServer); err != nil {
		fmt.Fprintf(stderr, "agent-chat wrap: server→client: %v\n", err)
	}
	if apps != nil {
		apps.shutdown(3 * time.Second)
	}

	return wrapExitCode(cmd.Wait())
}

// wrapExitCode maps the child's exit to ours: same code, or 128+signal when
// it was killed by a signal (shell convention).
func wrapExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		if c := ee.ExitCode(); c >= 0 {
			return c
		}
	}
	return 1
}

// wrapVerdict is what a relay hook decides about one complete line.
type wrapVerdict struct {
	repl  []byte // replacement body (line ending re-attached); nil keeps the original
	drop  bool   // swallow the line entirely
	after func() // run after the line has been written (e.g. start a page fetch)
}

// wrapHook inspects one complete line (without its line ending).
type wrapHook func(line []byte) wrapVerdict

// wrapEditHook adapts an edit-only function to a hook.
func wrapEditHook(edit wrapEditFunc) wrapHook {
	if edit == nil {
		return nil
	}
	return func(line []byte) wrapVerdict { return wrapVerdict{repl: edit(line)} }
}

// wrapLineWriter serialises whole lines onto one stream, so the wrapper's own
// requests to the server never land in the middle of a relayed line.
type wrapLineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// writeLine writes b plus "\n" as one unit.
func (lw *wrapLineWriter) writeLine(b []byte) error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	_, err := lw.w.Write(append(append([]byte{}, b...), '\n'))
	return err
}

// wrapRelay copies newline-delimited messages from r to w, one line at a time,
// writing each as soon as it is complete. hook (may be nil) can replace or
// drop a complete line; the line ending ("\n" or "\r\n") is preserved. A line
// longer than wrapMaxLineBytes is streamed through without the hook.
func wrapRelay(r io.Reader, lw *wrapLineWriter, hook wrapHook) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			line = append(line, chunk...)
			if len(line) > wrapMaxLineBytes {
				// Too long to parse: stream it raw up to its newline, holding
				// the stream so nothing is interleaved.
				lw.mu.Lock()
				_, werr := lw.w.Write(line)
				var rerr error
				if werr == nil {
					rerr, werr = wrapStreamRest(br, lw.w)
				}
				lw.mu.Unlock()
				line = line[:0]
				if werr != nil {
					return werr
				}
				if rerr != nil {
					return wrapEOF(rerr)
				}
			}
			continue
		}
		line = append(line, chunk...)
		if len(line) > 0 {
			out := line
			var v wrapVerdict
			if hook != nil && err == nil && len(line) <= wrapMaxLineBytes {
				body, ending := wrapSplitEnding(line)
				v = hook(body)
				if v.repl != nil {
					out = append(v.repl, ending...)
				}
			}
			if !v.drop {
				lw.mu.Lock()
				_, werr := lw.w.Write(out)
				lw.mu.Unlock()
				if werr != nil {
					return werr
				}
			}
			if v.after != nil {
				v.after()
			}
			line = line[:0]
		}
		if err != nil {
			return wrapEOF(err)
		}
	}
}

// wrapStreamRest copies br to w through the next newline (inclusive),
// returning the read error and the write error separately.
func wrapStreamRest(br *bufio.Reader, w io.Writer) (rerr, werr error) {
	for {
		chunk, err := br.ReadSlice('\n')
		if _, werr := w.Write(chunk); werr != nil {
			return nil, werr
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return err, nil
	}
}

func wrapEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// wrapSplitEnding splits a line into its body and its "\n"/"\r\n" ending.
func wrapSplitEnding(line []byte) (body, ending []byte) {
	if bytes.HasSuffix(line, []byte("\r\n")) {
		return line[:len(line)-2], line[len(line)-2:]
	}
	if bytes.HasSuffix(line, []byte("\n")) {
		return line[:len(line)-1], line[len(line)-1:]
	}
	return line, nil
}

// wrapAdvertiseUI adds the MCP Apps capability to a client→server message
// that declares client capabilities. Returns nil (forward unchanged) for
// anything else, including unparseable lines and batches.
func wrapAdvertiseUI(line []byte) []byte {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	// Cheap pre-filter: only lines that could declare capabilities.
	if !bytes.Contains(trimmed, []byte(`"initialize"`)) && !bytes.Contains(trimmed, []byte(wrapClientCapsMeta)) {
		return nil
	}
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &msg); err != nil {
		return nil
	}
	var method string
	if raw, ok := msg["method"]; !ok || json.Unmarshal(raw, &method) != nil || method == "" {
		return nil // responses and non-requests are never edited
	}
	var params map[string]json.RawMessage
	if raw, ok := msg["params"]; !ok || json.Unmarshal(raw, &params) != nil || params == nil {
		return nil
	}

	changed := false
	if method == "initialize" {
		if caps, ok := wrapAddUIExtension(params["capabilities"]); ok {
			params["capabilities"] = caps
			changed = true
		}
	}
	if rawMeta, ok := params["_meta"]; ok {
		var meta map[string]json.RawMessage
		if json.Unmarshal(rawMeta, &meta) == nil && meta != nil {
			if rawCaps, ok := meta[wrapClientCapsMeta]; ok {
				if caps, ok := wrapAddUIExtension(rawCaps); ok {
					meta[wrapClientCapsMeta] = caps
					if b, err := json.Marshal(meta); err == nil {
						params["_meta"] = b
						changed = true
					}
				}
			}
		}
	}
	if !changed {
		return nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	msg["params"] = b
	out, err := json.Marshal(msg)
	if err != nil {
		return nil
	}
	return out
}

// wrapAddUIExtension returns capabilities with extensions[ui] set, keeping
// every other field and extension. ok=false means leave it alone: the value
// isn't an object, or the client already declares the ui extension.
func wrapAddUIExtension(raw json.RawMessage) (json.RawMessage, bool) {
	caps := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &caps); err != nil || caps == nil {
			return nil, false
		}
	}
	exts := map[string]json.RawMessage{}
	if rawExts, ok := caps["extensions"]; ok && string(bytes.TrimSpace(rawExts)) != "null" {
		if err := json.Unmarshal(rawExts, &exts); err != nil || exts == nil {
			return nil, false
		}
	}
	if _, ok := exts[wrapUIExtension]; ok {
		return nil, false // the client already speaks MCP Apps; respect its own declaration
	}
	ui, _ := json.Marshal(map[string][]string{"mimeTypes": {wrapUIMimeType}})
	exts[wrapUIExtension] = ui
	b, err := json.Marshal(exts)
	if err != nil {
		return nil, false
	}
	caps["extensions"] = b
	out, err := json.Marshal(caps)
	if err != nil {
		return nil, false
	}
	return out, true
}
