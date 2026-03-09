# Dual MCP Servers: Agent-Facing vs Orchestrator-Facing

**Date:** 2026-02-28
**Status:** Accepted

## Context

The agent interacts with agent-chat via MCP tools like `send_message` (which
blocks until the user replies) and `check_messages`. External systems (e.g.
swe-swe) also need to inject messages into the chat and read history, but must
not interfere with the agent's blocking tool calls or message queue.

## Decision

Run two separate MCP server instances on the same HTTP process:

1. **Agent server** (`/mcp`) — tools: `send_message`, `send_verbal_reply`,
   `draw`, `send_progress`, `send_verbal_progress`, `check_messages`. These
   tools interact with the message queue (blocking on user replies).

2. **Orchestrator server** (`/mcp/orchestrator`) — tools: `send_chat_message`,
   `get_chat_history`. These bypass the message queue, writing directly to the
   event bus. Non-blocking.

## Alternatives Considered

- **Single MCP server with role parameter** — muddies tool semantics; risk of
  orchestrator accidentally calling blocking tools.
- **Separate HTTP endpoints without MCP** — loses MCP protocol benefits
  (tool discovery, schema validation).

## Consequences

- Clean separation of concerns: agent tools block, orchestrator tools don't.
- External systems can inject messages without disrupting agent flow.
- Two MCP server instances share the same event bus and HTTP listener.
