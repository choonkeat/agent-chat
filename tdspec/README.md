# agent-chat

Elm type-checked specification of the agent-chat bridge.

**Go server bridging Browser chat UI and Coding Agent** via stdio MCP + WebSocket.

See `HOW-TO.md` in the parent tdspec for the methodology behind this approach.

## Quick start

Type-check:

    make build

Browse docs with clickable type navigation:

    PORT=8000 make preview

Generate static HTML docs:

    make docs

## Spec modules

  - **Domain** -- shared types: `FileRef`, `Event`, `Seq`, `AckId`, `Timestamp`, `Version`
  - **EventBus** -- pub/sub, blocking ack, message queue, reconnect replay
  - **McpTools** -- 6 MCP tools, 3 resources, param/result types
  - **WebSocketProtocol** -- wire protocol, HTTP endpoints
