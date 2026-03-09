# Agent Chat + Canvas Integration Plan

## Goal

Merge drawing/canvas capabilities from `agent-whiteboard` into `agent-chat`, so agents can interleave text messages and diagram canvases in a single chat history. Each drawing appears as an inline canvas bubble (not a single persistent canvas drawn over repeatedly).

## Architecture

### Current State

- **agent-chat**: Go MCP server with `send_message` and `check_messages` tools. Browser UI shows text bubbles over WebSocket.
- **agent-whiteboard**: Go MCP server with `draw`, `clear`, `message` tools. Browser UI has a single persistent canvas with Rough.js hand-drawn rendering, progressive animation, slide navigation, and session recording/export.

### Target State

A unified `agent-chat` server where:
- Text messages appear as chat bubbles (existing)
- Drawings appear as inline canvas bubbles in the chat flow
- Each `draw` call creates a **new** canvas element in the message history
- Canvases are rendered with the same Rough.js hand-drawn aesthetic
- Full chat history (text + canvases) can be exported as a single self-contained HTML file with canvas images embedded as base64 PNGs

## New MCP Tools

### `draw` tool
```
Name: "draw"
Params: {
  caption?: string         // Text displayed below the canvas
  instructions: []object   // Array of drawing instruction JSON objects
  quick_replies?: []string // Optional reply chips
}
```
- Creates a new canvas bubble in the chat (left-aligned, like agent messages)
- Caption displayed below the canvas as text
- Instructions rendered with Rough.js progressive animation
- After animation completes, input is enabled for user response
- Returns user's response (same as send_message)

### `clear` tool
Remove entirely. Each draw creates a fresh canvas, so there's no need to clear.

### Existing tools unchanged
- `send_message` — text messages (unchanged)
- `check_messages` — non-blocking poll (unchanged)

## MCP Resources

Port over from agent-whiteboard:
- `whiteboard://instructions` — instruction reference
- `whiteboard://diagramming-guide` — diagramming best practices
- `whiteboard://quick-reference` — condensed cheat sheet

## Drawing Instructions (from agent-whiteboard)

13 instruction types, all JSON objects with a `type` field:

| Type | Params | Description |
|------|--------|-------------|
| `moveTo` | x, y | Move cursor (no draw) |
| `lineTo` | x, y | Draw line to point |
| `setColor` | color | Set stroke color |
| `setStrokeWidth` | width | Set stroke width |
| `drawRect` | x, y, width, height, fill?, fillStyle? | Rectangle |
| `drawCircle` | x, y, radius, fill?, fillStyle? | Circle |
| `drawEllipse` | x, y, width, height, fill?, fillStyle? | Ellipse |
| `writeText` | text, x, y, fontSize?, font? | Text at position |
| `label` | text, offsetX?, offsetY?, fontSize? | Text near cursor |
| `clear` | (none) | Clear this canvas |
| `wait` | duration | Pause animation (ms) |

Fill styles: solid, hachure, zigzag, cross-hatch, dots, dashed, zigzag-line

## Event Types

Extend the WebSocket event protocol:

```json
// Existing
{"type": "agentMessage", "text": "...", "quick_replies": [...]}
{"type": "userMessage", "text": "..."}

// New
{"type": "draw", "instructions": [...], "caption": "...", "quick_replies": [...], "ack_id": "..."}
```

The `draw` event creates a new canvas bubble in the chat area.

## Client-Side Changes

### New Dependencies
- **Rough.js** — hand-drawn rendering (bundle or CDN)
- Port rendering code from agent-whiteboard:
  - `AgentWhiteboard` class (canvas facade)
  - `InstructionQueue` (animation sequencing)
  - `RoughRenderer` (two-canvas architecture)
  - `ProgressiveAnimator` (arc-length animation)
  - `TextRenderer` (typewriter effect)
  - `validate-instructions.ts` (instruction validation)

### Canvas Bubble Rendering

When a `draw` event arrives:
1. Create a `<div class="bubble agent canvas-bubble">`
2. Inside, create a `<canvas>` element (default 900x550, scaled to fit bubble max-width)
3. Optionally add caption text below canvas
4. Initialize `AgentWhiteboard` on the canvas
5. Feed instructions, animate progressively
6. When animation completes, enable input

### Canvas Sizing
- Default logical size: 900x550 (same as agent-whiteboard)
- CSS: scale to fit within bubble max-width (80% of chat area)
- Maintain aspect ratio
- Handle device pixel ratio for sharp rendering

### Chat History Replay
- On reconnect, replay `draw` events by rendering instructions with animation skipped (instant draw)
- Canvas bubbles appear in correct position in chat history

## Export / Download

### Single HTML File Export
Add a download button to the UI. On click:
1. Walk through all chat bubbles
2. For text bubbles: copy HTML content
3. For canvas bubbles: call `canvas.toDataURL('image/png')` to get base64 PNG
4. Generate a self-contained HTML file with:
   - Inline CSS (dark theme)
   - Text messages as styled divs
   - Canvas drawings as `<img src="data:image/png;base64,...">` tags
   - No external dependencies
5. Trigger download as `chat-export-{timestamp}.html`

## Go Server Changes

### New Tool Registration (tools.go)
- Add `draw` tool with `DrawParams` struct
- Publish `draw` event type through EventBus
- Block on user response (same pattern as send_message)

### Event Struct (eventbus.go)
Extend Event to carry drawing data:
```go
type Event struct {
    Type         string        `json:"type"`
    Text         string        `json:"text,omitempty"`
    AckID        string        `json:"ack_id,omitempty"`
    QuickReplies []string      `json:"quick_replies,omitempty"`
    Instructions []interface{} `json:"instructions,omitempty"` // draw instructions
    Caption      string        `json:"caption,omitempty"`
}
```

### MCP Resources (resources.go)
New file to register MCP resources:
- Read markdown files from embedded FS
- Serve as `whiteboard://instructions`, etc.

## Implementation Order

1. **Go: Add `draw` tool and extend Event struct** — server-side plumbing
2. **Go: Add MCP resources** — instruction reference, diagramming guide
3. **Client: Bundle Rough.js and rendering code** — port from agent-whiteboard, compile to plain JS
4. **Client: Canvas bubble rendering** — handle `draw` events, create inline canvases
5. **Client: History replay for canvases** — instant-draw on reconnect
6. **Client: Export/download** — single HTML with embedded PNGs
7. **Test and iterate**

## Build Considerations

The agent-whiteboard client code is TypeScript. For agent-chat (which uses plain JS with no build step), we have options:
- **Option A**: Add a build step (esbuild/rollup) to compile TypeScript rendering code → bundled JS
- **Option B**: Pre-build the rendering bundle in agent-whiteboard and copy the output
- **Option C**: Rewrite the essential rendering logic in plain JS (simpler but more work)

**Recommendation**: Option A — add a minimal esbuild step. The rendering code (Rough.js + animation) is complex enough that maintaining it in plain JS would be painful. A single `esbuild --bundle` command can produce a self-contained JS file.

## File Structure (after integration)

```
agent-chat/workspace/
├── main.go              # HTTP server, WebSocket, MCP (extended)
├── eventbus.go          # EventBus (extended with draw events)
├── tools.go             # MCP tools: send_message, check_messages, draw
├── resources.go         # MCP resources: instruction ref, guides
├── instruction-reference.md
├── diagramming-guide.md
├── quick-reference.md
├── client-dist/
│   ├── index.html       # Chat UI (add download button)
│   ├── style.css        # Styles (add canvas bubble styles)
│   ├── app.js           # Chat logic (add draw event handling, export)
│   └── canvas-bundle.js # Bundled Rough.js + rendering code
├── Makefile             # Build targets: build, bundle-client
├── go.mod / go.sum
└── dist/
    └── agent-chat       # Compiled binary
```
