# Cursor-Based Event Synchronization for Browser Reconnect

**Date:** 2026-02-22
**Status:** Accepted

## Context

Browsers disconnect from the WebSocket due to network issues, sleep/wake, or
tab backgrounding. On reconnect, the client needs to catch up on missed events
without duplicating messages already displayed.

## Decision

Every event gets a monotonically increasing sequence number (`Seq`). On
reconnect, the client sends its last-seen `cursor` value. The server replays
only events with `Seq > cursor`, then sends a `historyEnd` sentinel so the
client knows replay is complete.

During replay, the client reconstructs the full conversation (bubbles, frozen
quick replies, canvas drawings) and defers showing live quick replies until
after `historyEnd`.

## Alternatives Considered

- **Full reload** — simple but causes visible flash; loses scroll position.
- **Session tokens with server-side diffing** — more server complexity.
- **Timestamp-based sync** — clock skew issues between client and server.

## Consequences

- Reconnects are seamless — no duplicate or missing messages.
- Server must keep full event log in memory (bounded by conversation length).
- Quick replies are deferred until `historyEnd` to prevent premature freezing.
- Works naturally with JSONL persistence (events already have sequence numbers).
