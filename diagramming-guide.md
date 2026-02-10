# Diagramming Guide

Read this before drawing. These principles help humans actually understand your diagrams.

## How drawing works in chat

Each `draw` call creates a **new canvas bubble** in the chat, like a message but with a diagram. Use `send_message` for text explanations before or after diagrams. Build complex explanations by interleaving text messages and diagram canvases.

## Cognitive principles

### 1. Gradual reveal (chunking)
Never dump an entire complex diagram at once. Build concepts layer by layer across **multiple draw calls**. Each draw call creates a new canvas bubble.

Example — explaining a client-server architecture:
- send_message: "Let me walk you through the architecture."
- draw call 1: Just the client box
- draw call 2: Client + server + request arrow
- draw call 3: Full picture with database

### 2. One concept per diagram
Each draw call should communicate exactly one idea. If you need multiple paragraphs to explain it, split into multiple draw calls with send_message in between.

### 3. Spatial consistency
Place elements in consistent locations across diagrams. If the "Client" box is at the top-left in diagram 1, keep it there in diagram 2. Humans build spatial memory.

## Choosing a diagram type

| Situation | Diagram type | When to use |
|-----------|-------------|-------------|
| How components connect | **Box-and-arrow** | Static structure — what exists and how parts relate |
| Interactions over time | **Sequence diagram** | Messages, requests, and responses between actors in order |
| Decision / branching | **Flowchart** | Logic and control flow — if/else paths, algorithms |
| Comparing two things | **Side-by-side** | Before/after, trade-offs. Use distinct fill colors to highlight differences |
| Explaining one concept | **Annotated shape** | Zooming into a single component with labeled parts |

## Layout rules

**Canvas:** 900 × 550 pixels. Leave 30px margins. Work within 840 × 490.

**Text:** 25px minimum vertical gap between lines. Font size 14-16 for body, 18+ for titles, 11-12 for annotations.

**Colors:** 2-4 max. One color per actor/component, stay consistent.
- Blue #2196F3 (primary) / fill #E3F2FD
- Green #4CAF50 (success) / fill #E8F5E9
- Orange #FF9800 (warnings) / fill #FFF3E0
- Red #F44336 (errors) / fill #FFEBEE
- Gray #666666 for annotations

## Drawing arrows

No arrow primitive — draw arrowheads as two short lines from the tip. Rightward arrow ending at (ex, ey):

```json
{"type": "moveTo", "x": sx, "y": sy},
{"type": "lineTo", "x": ex, "y": ey},
{"type": "moveTo", "x": ex, "y": ey},
{"type": "lineTo", "x": ex-10, "y": ey-6},
{"type": "moveTo", "x": ex, "y": ey},
{"type": "lineTo", "x": ex-10, "y": ey+6}
```

**Leftward**: `ex+10, ey-6` and `ex+10, ey+6`.
**Downward**: `ex-6, ey-10` and `ex+6, ey-10`.
**Upward**: `ex-6, ey+10` and `ex+6, ey+10`.

## Common mistakes
- **Too much at once**: split across multiple draw calls, one concept each
- **Overlapping text**: calculate positions; no two labels at the same Y
- **Missing arrowheads**: every directed line needs two short arrowhead lines
- **Tiny text**: never below fontSize 11; prefer 13+
- **No whitespace**: leave breathing room between elements
- **Inconsistent positions**: keep elements in the same place across diagrams
