# Parent Frame Notification for Agent Interrupt

**Date:** 2026-02-12
**Status:** Accepted

## Context

Agent-chat runs inside an iframe in swe-swe. When the user sends their first
message or an interrupt phrase, the parent frame (terminal) needs to know so it
can prompt the agent to call `check_messages` or send Esc-Esc to abort the
current tool call.

The agent has no way to know a message arrived unless it calls `check_messages`,
and it won't call that unless prompted.

## Decision

Use `window.parent.postMessage()` to notify the parent frame of two events:

1. **First user message** — `{type: 'agent-chat-first-user-message', text:
   'check_messages; i sent u a chat message'}`. The text is injected into the
   agent's terminal as a nudge.

2. **Interrupt** — `{type: 'agent-chat-interrupt', text: 'check_messages; ask
   me how to proceed'}`. The parent sends Esc-Esc to abort the current tool,
   then injects the nudge text.

Messages are queued before the postMessage to avoid race conditions (the agent
might call `check_messages` before the message is in the queue).

## Alternatives Considered

- **Agent polls `check_messages` on a timer** — wastes tool calls; adds latency.
- **Server-side PTY injection** — agent-chat doesn't control the agent's PTY.
- **WebSocket from parent to agent-chat server** — unnecessary complexity.

## Consequences

- Near-instant agent awareness of user messages and interrupts.
- Works only when embedded in a parent frame (standalone mode degrades
  gracefully — postMessage is a no-op).
- Parent frame must have a matching postMessage listener.
- Queue-before-notify ordering prevents race conditions.
