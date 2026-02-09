# SPEC v3: Agent Chat Bridge

## Overview

A Go server that bridges a browser chat UI and a coding agent. The server communicates with the agent over **stdio MCP** and with the browser over **WebSocket**. It is forked from the [agent-whiteboard](../agent-whiteboard/workspace) codebase with all drawing/canvas functionality removed.

## Architecture

```
Browser(s) <--WebSocket--> Go Bridge <--stdio MCP--> Coding Agent
```

| Interface | Protocol | Purpose |
| :--- | :--- | :--- |
| Agent-facing | MCP over stdio | Expose `send_message` tool to the coding agent |
| Browser-facing | WebSocket + HTTP (static assets) | Real-time chat messages |

The coding agent spawns the Go server as a subprocess. The server starts an HTTP listener on `PORT` (or `:0`) and prints `http://localhost:<port>` to **stderr** (stdout is reserved for MCP stdio). Multiple browser clients may connect simultaneously (fan-out).

## Lifecycle

1. Coding agent spawns the Go server as a child process (stdio MCP).
2. Server starts HTTP listener on `PORT` or OS-assigned port.
3. Server prints `http://localhost:<port>` to **stderr**.
4. Browser(s) connect via WebSocket at `/ws`.
5. Agent calls `send_message` tool to communicate with the user.
6. Server publishes message to browser(s), blocks until user replies.
7. User's reply is returned as the tool result.

## MCP Tool

### `send_message`

The **only** tool exposed to the agent. Sends a text message to the browser chat UI and blocks until the user replies.

**Parameters:**

| Name | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `text` | `string` | Yes | The message to display to the user. |

**Returns:**

```json
{ "reply": "the user's response text" }
```

**Behavior:**

1. Server creates an ack channel (UUID).
2. Server publishes an `agentMessage` event (with `ack_id`) to all connected browsers via WebSocket.
3. Server **blocks** waiting on the ack channel.
4. Browser displays the message and enables the chat input.
5. User types a reply and sends it.
6. Browser sends an `ack` message over WebSocket with the `ack_id` and the user's text.
7. Server resolves the ack channel, tool unblocks.
8. Tool returns `{ "reply": "<user's text>" }` to the agent.

## WebSocket Protocol

### Server → Browser

```jsonc
// On initial connection (includes history for reconnect replay)
{ "type": "connected", "history": [...], "pendingAckId": "..." }

// Agent message (displayed as chat bubble, enables user input)
{ "type": "agentMessage", "text": "...", "ack_id": "uuid" }
```

### Browser → Server

```jsonc
// User reply (sent when user types and submits)
{ "type": "ack", "id": "uuid", "message": "user's reply text" }
```

## Event Bus

Forked from `agent-whiteboard/mcp-server-go/eventbus.go`. Responsibilities:

- **Pub/sub**: Fan-out events to N WebSocket subscribers.
- **Blocking ack**: `CreateAck()` returns a channel; `ResolveAck(id, result)` unblocks it.
- **Event log**: Stores all published events in-memory. On browser reconnect, the full log is replayed so the client can reconstruct chat history.

## Reconnect Behavior

When a browser disconnects and reconnects:

1. Server sends `{ "type": "connected", "history": [...], "pendingAckId": "..." }`.
2. Browser replays all past `agentMessage` events to rebuild the chat history.
3. If there is a `pendingAckId`, the chat input is enabled so the user can still reply.

## Browser Client

A minimal chat UI served as static HTML/JS/CSS from the Go binary (embedded or from a dist directory).

### UI Elements

- **Chat message area**: Scrollable list of agent and user message bubbles.
- **Text input**: Enabled only when an `agentMessage` with `ack_id` is pending. Disabled otherwise.
- **Send button**: Submits the user's reply as an `ack`.

### Behavior

- Auto-scrolls to latest message.
- Shows a typing/thinking indicator while waiting for the agent.
- Auto-reconnects on WebSocket disconnect (exponential backoff).
- On reconnect, replays history to restore chat state.

## Configuration

Via environment variables:

| Parameter | Default | Description |
| :--- | :--- | :--- |
| `PORT` | _(OS-assigned)_ | TCP port for the HTTP/WebSocket listener. |

## Forked from Agent Whiteboard

This server reuses the following from `agent-whiteboard/mcp-server-go/`:

| Keep | Remove |
| :--- | :--- |
| `eventbus.go` (pub/sub, ack, event log) | `draw` tool |
| `main.go` (HTTP server, WebSocket handler, static serving, stdio MCP) | `clear` tool |
| Blocking ack protocol | Canvas, InstructionQueue, animation |
| Reconnect + history replay | Slide history, snapshots |
| Fan-out to N browser clients | Canvas dimension validation |
| | Turtle state, viewport reporting |

## `.mcp.json` Entry

```json
{
  "mcpServers": {
    "agent-chat": {
      "command": "go",
      "args": ["run", "."],
      "cwd": "/repos/agent-chat/workspace",
      "env": {
        "PORT": "3003"
      }
    }
  }
}
```
