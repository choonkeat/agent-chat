# WebSocket + Event Bus for Real-Time Chat

**Date:** 2026-02-09
**Status:** Accepted

## Context

Agent-chat needs low-latency, bidirectional communication between the browser
and the Go server. The agent sends messages, quick replies, drawings, and
progress updates; the browser sends user messages and file uploads. Multiple
browser tabs may be open simultaneously.

## Decision

Use a single WebSocket connection per browser tab, backed by an in-memory
publish/subscribe event bus (`eventbus.go`). All events flow through the bus:
agent messages, user messages, draws, progress updates. Each WebSocket
subscriber receives events in order via a buffered channel.

The HTTP server also exposes REST endpoints for file uploads and autocomplete,
but all real-time chat flows through WebSocket.

## Alternatives Considered

- **HTTP polling** — too much latency for real-time chat; wasteful when idle.
- **Server-Sent Events (SSE)** — unidirectional; would still need HTTP POST for
  user→server messages, adding complexity.
- **gRPC** — heavier dependency; browser support requires grpc-web proxy.

## Consequences

- Simple, low-latency message delivery in both directions.
- Event bus enables fan-out to multiple browser tabs with no extra code.
- WebSocket reconnection must be handled explicitly (see cursor-based sync ADR).
- All state lives in the event bus — no database required.
