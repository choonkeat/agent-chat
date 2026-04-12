# Channel Permission Relay via Stdin Interceptor

**Date:** 2026-04-06
**Status:** Accepted

## Context

Claude Code sends permission prompts via a `notifications/claude/channel/
permission_request` JSON-RPC notification on stdin.  The MCP SDK does not
recognise custom notification methods, so these messages were silently
dropped — the user never saw the prompt and the tool call stalled until
Claude Code timed it out.

Agent-chat needs to surface these prompts in its own chat UI, collect an
Allow/Deny verdict from the user, and relay the verdict back to Claude Code
on stdout — all without modifying the MCP SDK.

## Decision

### Stdin interceptor architecture

Introduce a `channelInterceptor` goroutine that sits between real stdin and
the MCP SDK's `IOTransport`.  A pipe connects them: the interceptor reads
stdin line-by-line, inspects each JSON-RPC message, and either handles it
internally (channel notifications) or forwards it to the pipe for the SDK.

```
real stdin ──► channelInterceptor ──► io.Pipe ──► mcp.IOTransport (SDK)
                      │
                      ▼
                   os.Stdout  (verdict notifications, bypassing SDK)
```

### Capability advertisement

The server advertises two experimental capabilities at startup:

- `claude/channel`
- `claude/channel/permission`

This opts in to the protocol so Claude Code knows it can send permission
notifications to this server.

### Permission lifecycle

1. **Intercept**: A `notifications/claude/channel/permission_request`
   message arrives on stdin.  The interceptor parses the `request_id`,
   `tool_name`, `description`, and `input_preview`.
2. **Present**: The interceptor publishes an `agentMessage` (or
   `verbalReply` if the user is in voice mode) containing a formatted
   permission prompt with Allow/Deny quick replies.  The agent's
   existing quick replies are saved aside.
3. **Collect**: The next user message is checked by `HandleUserResponse`
   before it reaches the agent's message queue.
   - **"Allow"** → allow verdict, message consumed (not forwarded to agent).
   - **"Deny"** → deny verdict, message consumed.
   - **Anything else** → implicit deny verdict, message **not** consumed
     (forwarded to agent as a normal message so the user's intent is not
     lost).
4. **Relay**: The verdict is written as a
   `notifications/claude/channel/permission` JSON-RPC notification directly
   to stdout, bypassing the MCP SDK.
5. **Restore**: The agent's saved quick replies are re-published so the UI
   returns to its pre-permission state.

### Voice-mode integration

When the user is in voice mode (`bus.LastVoice()` is true), the permission
prompt is published as a `verbalReply` event rather than `agentMessage`,
ensuring it is spoken aloud via TTS.  The voice-prefix microphone emoji
(`🎤`) is stripped before matching Allow/Deny.

### Truncated input repair

Claude Code may truncate `input_preview` mid-string.  The `prettyJSON`
formatter attempts to re-close any open JSON strings, brackets, and braces
before pretty-printing, inserting a `…` truncation marker.  If repair
fails, the raw string is shown as-is.

## Alternatives Considered

- **Extend the MCP SDK to handle custom notifications.**  Would require
  forking or patching an upstream dependency.  Rejected: the interceptor
  is local, self-contained, and decoupled from SDK version.
- **Run a separate transport for channel messages.**  Rejected: Claude Code
  sends these on the same stdio stream; a second transport would require
  protocol-level multiplexing on the host side.
- **Forward permission notifications into the agent's normal message
  queue.**  Rejected: the agent would need to understand the permission
  protocol, parse request IDs, and emit correctly-formatted verdicts.
  The interceptor keeps this complexity out of the agent's prompt.

## Consequences

- Permission prompts now appear in the chat UI as regular messages with
  Allow/Deny buttons, matching the experience users expect from
  interactive permission flows.
- The agent is unaware that a permission prompt occurred.  Its message
  queue is not polluted with Allow/Deny traffic unless the user responds
  with free text (implicit deny + forwarded message).
- The interceptor is a new concurrency boundary: the read loop, user
  response handling, and verdict writes all touch shared state under
  `permMu`.  The locking is straightforward (single mutex, short critical
  sections), but future changes should be aware of it.
- Only one permission prompt can be pending at a time.  If Claude Code
  sends a second prompt before the first is resolved, the first is
  silently replaced.  This matches current Claude Code behavior, which
  serialises permission prompts.
