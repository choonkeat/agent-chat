package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Per-process ordinal counters for each MCP tool whose call surfaces in the
// agent's own .jsonl. Stamped onto the matching event when the tool fires so
// downstream consumers (swe-swe-server's /api/fork resolver) can correlate a
// chat bubble with its tool_use_id / call_id in the agent's rollout.
//
// On agent-chat restart these are seeded from the existing event log via
// SeedToolCounters so the next stamped event continues the agent's own count.
var (
	sendMessageCount        atomic.Int64
	sendProgressCount       atomic.Int64
	sendVerbalReplyCount    atomic.Int64
	sendVerbalProgressCount atomic.Int64
	checkMessagesCount      atomic.Int64
)

// SeedToolCounters scans events and advances each tool counter past the
// highest AgentToolSeq it sees for that tool. Call once after the on-disk
// event log has been loaded, before any tool handler can fire.
func SeedToolCounters(events []Event) {
	var sm, sp, svr, svp, cm int64
	for _, e := range events {
		switch e.AgentToolName {
		case "send_message":
			if e.AgentToolSeq > sm {
				sm = e.AgentToolSeq
			}
		case "send_progress":
			if e.AgentToolSeq > sp {
				sp = e.AgentToolSeq
			}
		case "send_verbal_reply":
			if e.AgentToolSeq > svr {
				svr = e.AgentToolSeq
			}
		case "send_verbal_progress":
			if e.AgentToolSeq > svp {
				svp = e.AgentToolSeq
			}
		case "check_messages":
			if e.AgentToolSeq > cm {
				cm = e.AgentToolSeq
			}
		}
	}
	sendMessageCount.Store(sm)
	sendProgressCount.Store(sp)
	sendVerbalReplyCount.Store(svr)
	sendVerbalProgressCount.Store(svp)
	checkMessagesCount.Store(cm)
}

// isVoiceMessage returns true if any message is a voice message (prefixed with 🎤).
func isVoiceMessage(msgs []UserMessage) bool {
	for _, m := range msgs {
		if strings.HasPrefix(m.Text, "\U0001f3a4 ") {
			return true
		}
	}
	return false
}

// voiceSuffix returns the appropriate reply instruction suffix.
func voiceSuffix(msgs []UserMessage) string {
	return execTemplate("reply-instructions", replyInstructionsData{IsVoice: isVoiceMessage(msgs)})
}

// executeNotEchoGuidance is appended after every user message delivered to the
// agent (via send_message return, send_verbal_reply return, check_messages, or
// barge-in append) so the framing is uniform regardless of delivery path. The
// wording was added after observing the agent reply "OK." to substantive user
// requests; uniform delivery prevents the bypass where a path-specific wrapper
// is missing.
const executeNotEchoGuidance = "This IS the user's message — execute the request, do not echo it back as an acknowledgment. When the requested work is done, call send_message (or send_verbal_reply in voice mode) to deliver the result — never end your turn without sending a user-visible message."

// emptyQueueGuidance is returned from check_messages when the queue is empty.
// The literal `{"queue":"empty"}` shape is kept so any programmatic check still
// works, but extra guidance is appended to stop the agent from sending a
// vacuous "Queue is empty." reply to the user — which was observed when an
// agent treated the empty-queue payload as the body of a send_message reply.
const emptyQueueGuidance = `{"queue":"empty"} — no user message is pending. Do NOT call send_message just to report this; the user did not ask anything. Return to your previous task, or stay silent and wait for the next user message.`

// composeCheckMessagesResult builds the check_messages result from the fresh
// queue drain plus any un-acked limbo batch (see EventBus.SetLimbo). A limbo
// batch was already handed to the agent once, but that delivery may have died
// in transit (harness idle abort, stdio reset), so it is redelivered behind a
// sentinel with ignore-if-already-handled framing. Fresh messages lead: they
// are the authoritative current instruction.
func composeCheckMessagesResult(limbo, fresh []UserMessage) string {
	redelivery := ""
	if len(limbo) > 0 {
		redelivery = "---REDELIVERY---\nRedelivering earlier user message(s) whose delivery to you may have been lost in transit (e.g. a timed-out send_message). If you have already seen and handled these, ignore them — do NOT re-execute or re-reply. Otherwise treat them as the user's message now.\nUser said: " + FormatMessages(limbo)
	}
	switch {
	case len(fresh) == 0 && len(limbo) == 0:
		return emptyQueueGuidance
	case len(fresh) == 0:
		return redelivery + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(limbo)
	case len(limbo) == 0:
		return "User said: " + FormatMessages(fresh) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(fresh)
	default:
		return "User said: " + FormatMessages(fresh) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(fresh) + "\n\n" + redelivery
	}
}

// progressKeepaliveInterval is how often a blocking tool call emits an MCP
// progress notification to keep the in-flight request alive. Claude Code's
// stdio idle timeout (CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT, default 30 min)
// aborts calls that send no response or progress for the window — and the
// abort sends no notifications/cancelled, orphaning the server-side wait. A
// periodic progress ping resets that window (verified empirically), so a
// send_message can block on a human indefinitely. Clients that ignore
// progress simply see harmless extra notifications.
var progressKeepaliveInterval = 60 * time.Second

// progressNotifier is the slice of *mcp.ServerSession used by the keepalive
// (an interface so tests can observe notifications without MCP plumbing).
type progressNotifier interface {
	NotifyProgress(context.Context, *mcp.ProgressNotificationParams) error
}

// startProgressKeepalive emits notifications/progress on token every interval
// until the returned stop func is called or ctx is cancelled.
func startProgressKeepalive(ctx context.Context, session progressNotifier, token any, interval time.Duration, message string) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var n float64
		for {
			select {
			case <-ticker.C:
				n++
				session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: token,
					Progress:      n,
					Message:       message,
				})
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// keepaliveForRequest starts a progress keepalive for an in-flight tool call,
// or returns a no-op stopper when the client sent no progress token.
func keepaliveForRequest(ctx context.Context, req *mcp.CallToolRequest, message string) (stop func()) {
	if req == nil || req.Session == nil || req.Params == nil {
		return func() {}
	}
	token := req.Params.GetProgressToken()
	if token == nil {
		return func() {}
	}
	return startProgressKeepalive(ctx, req.Session, token, progressKeepaliveInterval, message)
}

// appendBargeIn drains any queued user messages and appends them to text with a
// sentinel header so the agent reads them as a fresh user instruction without
// having to poll via check_messages. Returns text unchanged when the queue is
// empty.
func appendBargeIn(bus *EventBus, text string) string {
	msgs := bus.DrainMessages()
	if len(msgs) == 0 {
		return text
	}
	bus.SetLastVoice(isVoiceMessage(msgs))
	return text + "\n\n---BARGE-IN---\nUser said: " + FormatMessages(msgs) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(msgs)
}

// MessageParams are the parameters for the send_message tool.
type MessageParams struct {
	Text             string   `json:"text"`
	QuickReply       string   `json:"first_quick_reply"`
	MoreQuickReplies []string `json:"more_quick_replies,omitempty"`
	ImageURLs        []string `json:"image_urls,omitempty"`
}

// VerbalReplyParams are the parameters for the send_verbal_reply tool.
type VerbalReplyParams struct {
	Text             string   `json:"text"`
	QuickReply       string   `json:"first_quick_reply"`
	MoreQuickReplies []string `json:"more_quick_replies,omitempty"`
	ImageURLs        []string `json:"image_urls,omitempty"`
}

// resolveImageFiles copies local image files into the upload directory and returns FileRefs.
func resolveImageFiles(paths []string) []FileRef {
	var refs []FileRef
	for _, p := range paths {
		if p == "" {
			continue
		}
		src, err := os.Open(p)
		if err != nil {
			continue
		}

		info, err := src.Stat()
		if err != nil {
			src.Close()
			continue
		}

		base := filepath.Base(p)
		prefix := uuid.New().String()[:8]
		savedName := prefix + "-" + base
		destPath := filepath.Join(uploadDir, savedName)

		dst, err := os.Create(destPath)
		if err != nil {
			src.Close()
			continue
		}

		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			src.Close()
			continue
		}
		dst.Close()
		src.Close()

		mimeType := mime.TypeByExtension(filepath.Ext(base))
		if mimeType == "" {
			mimeType = "image/png"
		}

		refs = append(refs, FileRef{
			Name: base,
			Path: destPath,
			URL:  "/uploads/" + savedName,
			Size: info.Size(),
			Type: mimeType,
		})
	}
	return refs
}

// slugifyTitle normalises an agent-supplied title into a filesystem-safe
// kebab-case slug: lowercased, with each run of non-[a-z0-9] characters
// collapsed to a single dash, and leading/trailing dashes trimmed.
func slugifyTitle(title string) string {
	var b strings.Builder
	prevDash := true // treat start as if preceded by a dash so we trim leading dashes
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := b.String()
	for strings.HasSuffix(out, "-") {
		out = out[:len(out)-1]
	}
	return out
}

// nextDailyIndex returns the next per-day running index for dir. It looks for
// files named `{date}-NN-…` where NN is 2 or 3 digits and returns max(NN)+1,
// or 1 if no matching file exists.
func nextDailyIndex(dir, date string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1
	}
	prefix := date + "-"
	maxIdx := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := name[len(prefix):]
		dash := strings.IndexByte(rest, '-')
		// Require NN to be 2 or 3 digits followed by '-'; this avoids treating
		// a slug like "1234567890-foo" as an index.
		if dash != 2 && dash != 3 {
			continue
		}
		nStr := rest[:dash]
		n, err := strconv.Atoi(nStr)
		if err != nil || n < 1 {
			continue
		}
		if n > maxIdx {
			maxIdx = n
		}
	}
	return maxIdx + 1
}

func registerTools(server *mcp.Server, bus *EventBus) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_message",
		Description: "The ONLY channel the user sees in text mode. Use it for EVERY user-visible message: questions, status, final answers, errors, acknowledgments. Plain text in your response is invisible to the user — if you don't call send_message, the user sees nothing. Blocks until the user responds; the user's reply is RETURNED by this call as `User responded: …` — that IS the message. Always end a task by calling send_message with the result and waiting; never end your turn silently. You do NOT need to poll for user messages — any barge-in the user sends while you are working will be appended to the next send_progress (or draw) return after a `---BARGE-IN---` sentinel.\n\n`first_quick_reply` is a SINGLE plain string — the primary suggested reply shown to the user (e.g. \"Yes, proceed\"). `more_quick_replies` is an array of additional option strings (e.g. [\"Wait\", \"Cancel\"]). Do NOT pass a JSON-encoded array as `first_quick_reply`; it must be a plain string.\n\nOptionally pass `image_urls` with an array of absolute paths to local image files (e.g., screenshots) to include them inline in the message.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *MessageParams) (*mcp.CallToolResult, any, error) {
		// Tick the ordinal regardless of whether we actually publish a bubble:
		// the corresponding tool_use entry IS written to the agent's .jsonl
		// even for the voice-mode-rejection branch, so the .jsonl-side count
		// and the stamp-side count must advance together.
		toolSeq := sendMessageCount.Add(1)

		// A new call proves any previously blocked call is dead client-side;
		// kill it before it can steal the next user reply. No AckLimbo here:
		// this call might be a recap after a lost delivery, and its own
		// successful return overwrites limbo anyway.
		bus.CancelActiveWait()

		// Reject send_message when user is in voice mode — agent must use send_verbal_reply
		if bus.LastVoice() {
			// Marker keeps the on-disk count aligned with the agent's .jsonl,
			// which records this tool_use despite the early return.
			bus.PublishToolMarker("send_message", toolSeq)
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "ERROR: The user is in voice mode. Use send_verbal_reply instead of send_message to respond."},
				},
				IsError: true,
			}, nil, nil
		}

		// Lazily start HTTP server + open browser
		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		// Open browser if not already opened this session
		httpMu.Lock()
		shouldOpen := uiURL != "" && !browserOpened
		if shouldOpen {
			openBrowser(uiURL)
			browserOpened = true
		}
		httpMu.Unlock()

		// Wait for at least one viewer (browser) to be connected
		if err := bus.WaitForSubscriber(ctx); err != nil {
			return nil, nil, fmt.Errorf("waiting for browser: %w", err)
		}

		replies := append([]string{params.QuickReply}, params.MoreQuickReplies...)
		files := resolveImageFiles(params.ImageURLs)

		// If user already sent messages, strip quick_replies and return
		// queued messages immediately — the replies would be stale.
		// Register as THE active waiter (cancellable by the next call) and
		// keep the in-flight MCP request alive past harness idle timeouts.
		waitCtx, endWait := bus.BeginBlockingWait(ctx)
		defer endWait()
		stopKeepalive := keepaliveForRequest(waitCtx, req, "waiting for user reply")
		defer stopKeepalive()

		if bus.HasQueuedMessages() {
			bus.Publish(Event{Type: "agentMessage", Text: params.Text, Files: files, AgentToolSeq: toolSeq, AgentToolName: "send_message"})
			msgs, err := bus.WaitForMessagesStamped(waitCtx, "send_message", toolSeq)
			if err != nil {
				return nil, nil, fmt.Errorf("waiting for user message: %w", err)
			}
			bus.SetLastVoice(isVoiceMessage(msgs))
			text := "User responded: " + FormatMessages(msgs) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(msgs)
			if uiURL != "" {
				text += "\nChat UI: " + uiURL
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: text},
				},
			}, nil, nil
		}

		bus.Publish(Event{Type: "agentMessage", Text: params.Text, QuickReplies: replies, Files: files, AgentToolSeq: toolSeq, AgentToolName: "send_message"})

		msgs, err := bus.WaitForMessagesStamped(waitCtx, "send_message", toolSeq)
		if err != nil {
			return nil, nil, fmt.Errorf("waiting for user message: %w", err)
		}

		bus.SetLastVoice(isVoiceMessage(msgs))
		text := "User responded: " + FormatMessages(msgs) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(msgs)
		if uiURL != "" {
			text += "\nChat UI: " + uiURL
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_verbal_reply",
		Description: "Send a spoken reply to the user in voice mode. Use this tool when the user's message starts with 🎙 (microphone emoji), indicating they are using voice input. Keep replies conversational, concise, and plain text only — no markdown, no code blocks, no links. The text will be spoken aloud via browser text-to-speech. After speaking, the browser automatically listens for the user's next voice input.\n\n`first_quick_reply` is a SINGLE plain string — the primary suggested reply shown to the user (e.g. \"Yes, proceed\"). `more_quick_replies` is an array of additional option strings. Do NOT pass a JSON-encoded array as `first_quick_reply`; it must be a plain string.\n\nOptionally pass `image_urls` with an array of absolute paths to local image files (e.g., screenshots) to include them inline in the message.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *VerbalReplyParams) (*mcp.CallToolResult, any, error) {
		toolSeq := sendVerbalReplyCount.Add(1)
		bus.CancelActiveWait()

		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		httpMu.Lock()
		shouldOpen := uiURL != "" && !browserOpened
		if shouldOpen {
			openBrowser(uiURL)
			browserOpened = true
		}
		httpMu.Unlock()

		if err := bus.WaitForSubscriber(ctx); err != nil {
			return nil, nil, fmt.Errorf("waiting for browser: %w", err)
		}

		replies := append([]string{params.QuickReply}, params.MoreQuickReplies...)
		files := resolveImageFiles(params.ImageURLs)

		waitCtx, endWait := bus.BeginBlockingWait(ctx)
		defer endWait()
		stopKeepalive := keepaliveForRequest(waitCtx, req, "waiting for user reply")
		defer stopKeepalive()

		// If user already sent messages, strip quick_replies and return
		// queued messages immediately — the replies would be stale.
		if bus.HasQueuedMessages() {
			bus.Publish(Event{Type: "verbalReply", Text: params.Text, Files: files, AgentToolSeq: toolSeq, AgentToolName: "send_verbal_reply"})
			msgs, err := bus.WaitForMessagesStamped(waitCtx, "send_verbal_reply", toolSeq)
			if err != nil {
				return nil, nil, fmt.Errorf("waiting for user message: %w", err)
			}
			bus.SetLastVoice(isVoiceMessage(msgs))
			text := "User responded: " + FormatMessages(msgs) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(msgs)
			if uiURL != "" {
				text += "\nChat UI: " + uiURL
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: text},
				},
			}, nil, nil
		}

		bus.Publish(Event{Type: "verbalReply", Text: params.Text, QuickReplies: replies, Files: files, AgentToolSeq: toolSeq, AgentToolName: "send_verbal_reply"})

		msgs, err := bus.WaitForMessagesStamped(waitCtx, "send_verbal_reply", toolSeq)
		if err != nil {
			return nil, nil, fmt.Errorf("waiting for user message: %w", err)
		}

		bus.SetLastVoice(isVoiceMessage(msgs))
		text := "User responded: " + FormatMessages(msgs) + "\n\n" + executeNotEchoGuidance + "\n\n" + voiceSuffix(msgs)
		if uiURL != "" {
			text += "\nChat UI: " + uiURL
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})

	// DrawParams are the parameters for the draw tool.
	type DrawParams struct {
		Text             string   `json:"text"`
		Instructions     []any    `json:"instructions"`
		QuickReply       string   `json:"first_quick_reply"`
		MoreQuickReplies []string `json:"more_quick_replies,omitempty"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "draw",
		Description: `Draw a diagram as an inline canvas bubble in the chat and wait for viewer response.

Each draw call creates a new canvas bubble in the chat history, rendered with a hand-drawn aesthetic.
Use send_message for explanatory text before or after drawing.

HOW IT WORKS:
• Each draw call = one slide. Build complex diagrams across multiple slides (gradual reveal).
• Viewer clicks Continue (or gives feedback like "Slower pace") before this tool returns.
• The result tells you what the viewer said—adjust your next slide accordingly.

INSTRUCTIONS FORMAT — JSON objects with "type" field:
  [{"type":"drawRect","x":100,"y":100,"width":150,"height":60,"fill":"#E3F2FD"},
   {"type":"writeText","text":"Client","x":130,"y":140,"fontSize":16},
   {"type":"moveTo","x":250,"y":130},{"type":"lineTo","x":350,"y":130}]

COMMON TYPES: moveTo, lineTo, drawRect, drawCircle, writeText, setColor

Read whiteboard://instructions for all instruction types with parameters.
Read whiteboard://diagramming-guide for layout rules and cognitive principles.

` + "`first_quick_reply`" + ` is a SINGLE plain string — the primary reply option shown to the viewer. ` + "`more_quick_replies`" + ` is an array of additional option strings. Do NOT pass a JSON-encoded array as ` + "`first_quick_reply`" + `; it must be a plain string.`,
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *DrawParams) (*mcp.CallToolResult, any, error) {
		// Kill any orphaned blocking wait, and ack limbo: a draw call means
		// the agent is actively working, so the previous delivery arrived.
		bus.CancelActiveWait()
		bus.AckLimbo()

		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		httpMu.Lock()
		shouldOpen := uiURL != "" && !browserOpened
		if shouldOpen {
			openBrowser(uiURL)
			browserOpened = true
		}
		httpMu.Unlock()

		if err := bus.WaitForSubscriber(ctx); err != nil {
			return nil, nil, fmt.Errorf("waiting for browser: %w", err)
		}

		// Publish text as a chat bubble before the canvas
		bus.Publish(Event{Type: "agentMessage", Text: params.Text})

		// If user already sent messages, show the draw without quick_replies
		// and return immediately — the replies would be stale.
		if bus.HasQueuedMessages() {
			bus.Publish(Event{
				Type:         "draw",
				Instructions: params.Instructions,
			})
			text := appendBargeIn(bus, "Draw displayed.")
			if uiURL != "" {
				text += "\nChat UI: " + uiURL
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: text},
				},
			}, nil, nil
		}

		replies := append([]string{params.QuickReply}, params.MoreQuickReplies...)
		ack := bus.CreateAck()
		bus.Publish(Event{
			Type:         "draw",
			Instructions: params.Instructions,
			QuickReplies: replies,
			AckID:        ack.ID,
		})

		waitCtx, endWait := bus.BeginBlockingWait(ctx)
		defer endWait()
		stopKeepalive := keepaliveForRequest(waitCtx, req, "waiting for viewer response")
		defer stopKeepalive()

		var result string
		select {
		case result = <-ack.Ch:
		case <-waitCtx.Done():
			return nil, nil, fmt.Errorf("draw cancelled: %w", waitCtx.Err())
		}

		text := "Viewer acknowledged."
		if result != "ack" && len(result) > 4 {
			msg := result[4:] // strip "ack:" prefix
			text = "Viewer responded: " + msg + "\n\n(Reply to user in chat when done)"
		}

		if uiURL != "" {
			text += "\nChat UI: " + uiURL
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})

	// ProgressParams are the parameters for the send_progress tool.
	type ProgressParams struct {
		Text      string   `json:"text"`
		ImageURLs []string `json:"image_urls,omitempty"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_progress",
		Description: "Send a progress update to the chat UI without blocking. Use this for status updates (e.g., 'Working on it...', 'Found 3 matching files') when you want to keep the user informed but don't need a response. Unlike send_message, this returns immediately. If the user has sent a barge-in message since your last tool call, it will be appended to this call's return value after a `---BARGE-IN---` sentinel — treat that as a new instruction.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *ProgressParams) (*mcp.CallToolResult, any, error) {
		toolSeq := sendProgressCount.Add(1)
		// A progress update means the agent is actively working: kill any
		// orphaned blocking wait and ack the previous delivery as received.
		bus.CancelActiveWait()
		bus.AckLimbo()

		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		files := resolveImageFiles(params.ImageURLs)
		bus.Publish(Event{Type: "agentMessage", Text: params.Text, Files: files, AgentToolSeq: toolSeq, AgentToolName: "send_progress"})

		ack := appendBargeIn(bus, "Progress sent. If you've finished your task, use send_message to present final results and wait for the user's next request.")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: ack},
			},
		}, nil, nil
	})

	// VerbalProgressParams are the parameters for the send_verbal_progress tool.
	type VerbalProgressParams struct {
		Text      string   `json:"text"`
		ImageURLs []string `json:"image_urls,omitempty"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_verbal_progress",
		Description: "Send a spoken progress update to the user in voice mode without blocking. Use this for non-blocking status updates that should be spoken aloud (e.g., 'Looking into that now', 'Found the issue'). Unlike send_verbal_reply, this returns immediately without waiting for a response. The text will be spoken via browser text-to-speech. Keep it conversational, concise, and plain text only — no markdown, no code blocks, no links. If the user has sent a barge-in message since your last tool call, it will be appended to this call's return value after a `---BARGE-IN---` sentinel — treat that as a new instruction.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *VerbalProgressParams) (*mcp.CallToolResult, any, error) {
		toolSeq := sendVerbalProgressCount.Add(1)
		bus.CancelActiveWait()
		bus.AckLimbo()

		if err := ensureHTTPServer(); err != nil {
			return nil, nil, fmt.Errorf("failed to start chat server: %w", err)
		}

		files := resolveImageFiles(params.ImageURLs)
		bus.Publish(Event{Type: "verbalReply", Text: params.Text, Files: files, AgentToolSeq: toolSeq, AgentToolName: "send_verbal_progress"})

		ack := appendBargeIn(bus, "Verbal progress sent. If you've finished your task, use send_verbal_reply to present final results and wait for the user's next request.")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: ack},
			},
		}, nil, nil
	})

	type EmptyParams struct{}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_messages",
		Description: "Drain pending user messages from the queue. Returns user messages prefixed with `User said: …` when present. When the queue is empty, returns `{\"queue\":\"empty\"}` followed by guidance NOT to send a user-visible reply just to report the empty state — return to your previous task or wait silently. The result may also carry a `---REDELIVERY---` section repeating earlier message(s) whose delivery to you may have been lost (e.g. a timed-out send_message) — ignore any you have already handled.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *EmptyParams) (*mcp.CallToolResult, any, error) {
		// Tick per call (empty or not) so the ordinal stays aligned with the
		// .jsonl-side count of check_messages tool_use entries.
		toolSeq := checkMessagesCount.Add(1)
		bus.CancelActiveWait()
		// Capture limbo BEFORE draining — a non-empty drain overwrites it.
		// Un-acked limbo gets redelivered: if the call that first carried it
		// died in transit, this is the recovery path; if not, the sentinel
		// framing tells the agent to ignore the duplicate.
		limbo := bus.Limbo()
		fresh := bus.DrainMessagesStamped("check_messages", toolSeq)
		if len(fresh) == 0 {
			// Empty drain publishes no userMessagesConsumed event, so record a
			// marker to keep the on-disk count aligned with the agent's .jsonl.
			bus.PublishToolMarker("check_messages", toolSeq)
		} else {
			bus.SetLastVoice(isVoiceMessage(fresh))
		}
		result := composeCheckMessagesResult(limbo, fresh)
		if len(limbo) > 0 {
			// The union just delivered becomes the new un-acked batch.
			bus.SetLimbo(append(limbo, fresh...))
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: result},
			},
		}, nil, nil
	})

	type SetChatTitleParams struct {
		Title string `json:"title" jsonschema:"Short human-readable chat title (e.g. 'Auth bug fix'). Slugified for the filename."`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_chat_title",
		Description: "Name the streaming chat-log export (enabled when AGENT_CHAT_EXPORT_DIR is set). Call it once the task at hand is clear — the auto-written ./agent-chats/YYYY-MM-DD-NN-untitled.md is renamed to …-{slugified-title}.md and its header rewritten; call again anytime to rename. Titles are per-session; keep them short and descriptive (e.g. 'Auth bug fix').",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *SetChatTitleParams) (*mcp.CallToolResult, any, error) {
		bus.CancelActiveWait()
		bus.AckLimbo()
		if chatStream == nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: streaming chat-log export is disabled — set AGENT_CHAT_EXPORT_DIR to enable it (export_chat_md still works for manual exports)"}},
				IsError: true,
			}, nil, nil
		}
		events, _ := bus.History()
		if err := chatStream.SetTitle(params.Title, events); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: " + err.Error()}},
				IsError: true,
			}, nil, nil
		}
		if err := regenerateIndexHTML(chatStream.Dir()); err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Chat log renamed to " + chatStream.MDPath()}},
		}, nil, nil
	})

	type ExportChatMDParams struct {
		Title      string `json:"title" jsonschema:"Short kebab-case slug describing the chat (e.g. 'auth-bug-fix'). Used to name the output file."`
		TargetDir  string `json:"target_dir,omitempty" jsonschema:"Optional override directory. If set, must resolve inside the current working directory. Defaults to ./agent-chats."`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "export_chat_md",
		Description: "Export the current chat as a markdown file (script-style: `**USER**` / `**AGENT**` markers with `> ` blockquoted bodies, elapsed-time annotations, and trailing `[Quick replies]` blocks) for review on GitHub/GitLab and viewing in a sibling bubble UI. Writes ./agent-chats/YYYY-MM-DD-NN-{title}.md, copies any user-uploaded image attachments into ./agent-chats/assets/YYYY-MM-DD-NN-N.{ext} (relative-path links from the .md), and upserts ./agent-chats/index.html — the chat-archive landing page — by prepending a manifest entry on top (newest first). On the first export, also writes ./agent-chats/assets/viewer.css and viewer.js (idempotent: never overwritten on subsequent calls). Path safety: target_dir cannot escape cwd.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *ExportChatMDParams) (*mcp.CallToolResult, any, error) {
		bus.CancelActiveWait()
		bus.AckLimbo()
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, fmt.Errorf("get cwd: %w", err)
		}
		cwdClean := filepath.Clean(cwd)

		slug := slugifyTitle(params.Title)
		if slug == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: title is required (a short kebab-case slug, e.g. 'auth-bug-fix')"}},
				IsError: true,
			}, nil, nil
		}

		var rootDir string
		if params.TargetDir != "" {
			rootDir = params.TargetDir
			if !filepath.IsAbs(rootDir) {
				rootDir = filepath.Join(cwd, rootDir)
			}
			rootDir = filepath.Clean(rootDir)
			rel, err := filepath.Rel(cwdClean, rootDir)
			if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("error: target_dir %q is outside the current working directory %q", params.TargetDir, cwdClean)}},
					IsError: true,
				}, nil, nil
			}
		} else {
			rootDir = filepath.Join(cwd, "agent-chats")
		}

		events, _ := bus.History()
		mdPath, warnings, err := runChatMarkdownExport(rootDir, slug, events, "claude", version+" ("+commit+")", time.Now())
		if err != nil {
			return nil, nil, err
		}

		summary := fmt.Sprintf("Exported chat to %s. Open %s in a browser to browse the archive.",
			mdPath, filepath.Join(rootDir, "index.html"))
		if len(warnings) > 0 {
			summary += fmt.Sprintf("\n\n%d warning(s):\n- %s", len(warnings), strings.Join(warnings, "\n- "))
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: summary}},
		}, nil, nil
	})
}

// registerOrchestratorTools registers tools on a separate MCP server for
// external orchestrators (e.g. swe-swe server) to interact with the chat.
func registerOrchestratorTools(server *mcp.Server, bus *EventBus) {
	type PushMessageParams struct {
		Text string `json:"text" jsonschema:"Message text to inject into the chat"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "send_chat_message",
		Description: "Send a message into the agent's chat queue, as if a user sent it from the browser.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *PushMessageParams) (*mcp.CallToolResult, any, error) {
		if params.Text == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "error: text is required"}},
				IsError: true,
			}, nil, nil
		}
		bus.ReceiveUserMessage(params.Text, nil)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "message pushed"}},
		}, nil, nil
	})

	type GetHistoryParams struct {
		Cursor int64 `json:"cursor,omitempty" jsonschema:"Return events with seq > cursor. 0 returns all."`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_chat_history",
		Description: "Get chat event history. Returns all events since the given cursor (sequence number).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, params *GetHistoryParams) (*mcp.CallToolResult, any, error) {
		events := bus.EventsSince(params.Cursor)
		data, err := json.Marshal(events)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal events: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}
