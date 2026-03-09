# Interrupt Phrase Detection

**Date:** 2026-02-24
**Status:** Accepted

## Context

When the agent is working on the wrong thing or the user wants to change
direction, they need a way to interrupt. In voice mode, the user can't press
Ctrl+C. In text mode, typing "stop" and waiting for the agent to call
`check_messages` is too slow.

## Decision

Detect interrupt phrases in both voice transcripts and typed messages. When
detected, send a `postMessage` to the parent frame (swe-swe terminal) carrying
an Esc-Esc signal to abort the agent's current tool call.

**Interrupt phrases:** `stop`, `wait`, `cancel`, `hold on`, `abort`, `halt`,
`pause`

**Special case:** "stop stop stop" (triple) disables voice mode entirely.

**Flow:**
1. Client detects phrase → sets `pendingInterrupt = true`
2. Message is queued normally via WebSocket
3. `postMessage({type: 'agent-chat-interrupt'})` sent to parent frame
4. Parent frame writes Esc-Esc to agent's PTY

## Alternatives Considered

- **Dedicated interrupt button** — works for text but not voice-only users.
- **Agent-side detection** — too late; agent must call `check_messages` first.
- **WebSocket signal to server** — server doesn't control the agent's PTY.

## Consequences

- Voice-only users can interrupt naturally by saying "stop" or "cancel".
- Text users get immediate interruption without waiting for `check_messages`.
- Requires parent frame cooperation (postMessage listener).
- Works in standalone mode too (postMessage is a no-op if no parent).
