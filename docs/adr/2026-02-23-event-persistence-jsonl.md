# Event Persistence to JSONL

**Date:** 2026-02-23
**Status:** Accepted

## Context

The event bus holds the full conversation in memory. If the server restarts
(crash, deploy, or intentional restart), all history is lost. Users expect to
see their conversation when they reconnect.

## Decision

Optionally persist events to a JSONL file (`AGENT_CHAT_EVENT_LOG` env var).
Each event is appended as a single JSON line. On startup, the server reads the
file to reconstruct the in-memory event log, including `lastQuickReplies` state.

## Alternatives Considered

- **SQLite** — overkill for append-only event log; adds dependency.
- **Redis** — external dependency; not needed for single-server architecture.
- **No persistence** — acceptable for ephemeral sessions but poor UX for long
  conversations.

## Consequences

- Conversation survives server restarts with zero external dependencies.
- JSONL is human-readable and easy to debug (`cat`, `jq`).
- File grows unbounded for long conversations (acceptable given typical usage).
- Playback mode can replay any saved JSONL file for debugging.
