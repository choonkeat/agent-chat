# Agent-to-Agent Bridging via Shared Chat Surfaces

**Date:** 2026-04-08
**Status:** Proposed

## Context

Today, each agent-chat session is an isolated 1:1 conversation between a
human and a single agent. When a human wants two agents (in two separate
swe-swe sessions) to collaborate, the only available channel is for the
human to manually relay messages between the two chats — copy-pasting
context, questions, and answers across two browser tabs. This is the
"middle-man" pain that motivates this ADR.

The need is for two (eventually N) agents to converse directly while the
human remains fully in the loop — able to read everything, interrupt, and
redirect — without becoming a courier.

## Decision

Add agent-to-agent bridging to agent-chat. Bridged sessions can exchange
messages via new MCP tools, with every cross-session message mirrored into
both chat logs so the human sees the complete conversation in either UI.

### Naming and addressing

- Each session has a stable handle derived from its UUID:
  `Agent{first-6-chars-of-uuid}` (e.g. `Agent42e617`).
- Handles are derivable, require no bridge-time assignment, and are short
  enough for prose and bubble labels.
- The full UUID remains the canonical address at the protocol level. The
  handle is purely for display and agent-to-agent referencing.
- The DOM carries the full handle/UUID; CSS truncates with ellipsis and
  hover/title reveals the full string. Disambiguation in case of first-6
  collisions is a hover affair, not a protocol concern.

### Tools (added to agent MCP server)

- `list_peers()` — returns `[{handle, uuid, status}, ...]` for currently
  bridged peers. Agents call this to discover whom they may address.

- `send_to_peer(to: string | string[], text: string)` — **blocks** until a
  peer message arrives in this session OR a human interrupts from this
  session's chat, whichever comes first. Returns the unblocking message
  tagged with `from_kind: "peer" | "human"` and `from_handle`. Mirror image
  of `send_message`.

- `send_to_peer_async(to, text)` — fire-and-forget broadcast for FYI cases
  where the agent isn't expecting a reply. Returns immediately.

### Tool behavior changes

- `check_messages` and `send_message` both return peer messages in addition
  to human messages. Peer messages are tagged `from_kind: "peer"` so the
  agent can distinguish trust/authority. A single queue, two distinct kinds.
- `send_message` unblocks on **either** a human reply or a peer message —
  whichever arrives first. The agent's prompt rendering uses `from_kind` to
  frame the result correctly.
- `send_to_peer` unblocks on **either** a peer reply or a human
  interruption from the *same* chat. The human can always grab the wheel.

### Symmetry

| Tool            | Primary unblock | Also unblocks on    |
|-----------------|-----------------|---------------------|
| `send_message`  | human reply     | peer message        |
| `send_to_peer`  | peer reply      | human interruption  |

The agent picks the tool by who it's *expecting* to hear from. The runtime
delivers whichever shows up first with a `from_kind` tag.

### Human-to-agent stays strictly 1:1

When the human types into AgentA's chat, the message goes only to AgentA.
There is no broadcast affordance from the human side in v1. If the human
wants AgentB to know something, they tell AgentA, and AgentA relays via
`send_to_peer`. This eliminates a class of UX edge cases (cross-chat
provenance labels, broadcast toggles, accidental leaks of intended-private
messages) and matches the iMessage mental model: a 1:1 chat is private to
its participants.

### Mirroring rule (UI)

Every `send_to_peer` / `send_to_peer_async` call appears in **both** the
sender's and the recipient's chat logs. Each log thus contains the full
A↔B transcript interleaved with that agent's own work and its human-facing
messages.

### Bubble layout

Adopt the WhatsApp-style group convention, simplified:

- **Own agent**: left side, with a small label (the agent's handle) above
  the bubble.
- **Peer agents** (when bridged): also left side, with their own labels.
  Visually distinguished from own-agent bubbles by a subtle accent.
- **Human**: right side, distinct color, no label. Unchanged from today.

In 1:1 mode (no bridged peers), the left-side label is hidden via CSS
(e.g. `.chat--solo-agent .bubble-label { display: none }`). The DOM
always renders the label; presentation hides it when there's only one
distinct left-side speaker. This keeps the rendering path uniform and
avoids JS branching on bridge state.

### Three distinct "agent state" UI affordances

1. **Agent thinking** — existing spinner.
2. **Agent waiting on human** (`send_message`) — existing spinner +
   quick replies + input enabled.
3. **Agent waiting on peer** (`send_to_peer`) — *new* peer-spinner
   ("waiting for Agent2a6dca…"), no quick replies, input still enabled
   for human interruption.

A `send_to_peer_async` call produces no spinner state change — just a
new bubble in the log.

### Bridge setup

Bridging is a human-side action (UI button or CLI), linking N session
UUIDs into a bridge group. Agents do not join/leave themselves in v1.
A small bridge-status indicator in the chrome ("Bridged with: Agent2a6dca")
keeps the human aware of the topology at all times.

## Alternatives Considered

- **Shared rooms with `room_join` / `room_leave` / `room_send`** — more
  general, but overshoots the 2-agent case. Subscriptions, room names, and
  late-joiner replay are real concerns only when N>2 or sessions are
  long-lived. Defer until needed.
- **Single relay tool with session UUID arg, no handles** — UUIDs are too
  long for prose and labels, forcing the human to copy-paste them into
  prompts. The derived `Agent{first-6}` handle removes this friction at
  zero coordination cost.
- **Auto-broadcast human messages to all bridged peers** — convenient but
  removes the ability to talk privately to one agent. A footgun that
  inverts the iMessage mental model. Rejected for v1; the human always
  routes through their own agent.
- **Merge peer messages into `check_messages` without `from_kind`** —
  loses the trust/authority distinction. Agents must be able to tell
  "user told me X" from "peer told me X" because the implications differ.
- **`send_to_peer` as fire-and-forget only** — forces agents into busy
  polling on `check_messages` for the common synchronous Q&A case, which
  is exactly the kind of friction we're trying to eliminate. Hence the
  blocking variant is the default and the async variant is the
  specialization.
- **`send_to_peer` blocking with `send_message` semantics (quick replies
  for the human)** — confuses the addressee. The human is a spectator on
  agent-to-agent exchanges, not the party being asked to respond.

## Consequences

- Agents gain a first-class, low-friction channel to collaborate without
  the human having to courier messages between chat tabs.
- The human retains full visibility (every cross-agent message appears in
  both logs) and full interrupt capability (can break into any
  `send_to_peer` from their own chat).
- Two new MCP tools (`send_to_peer`, `send_to_peer_async`) plus
  `list_peers`. `send_message` and `check_messages` gain a `from_kind`
  tag on returned messages but otherwise unchanged.
- UI changes are additive: a label slot above left bubbles (CSS-hidden in
  1:1 mode), a peer-spinner state, a bridge-status chrome indicator, and
  per-session bubble accents for peer agents.
- The same bytes appear in two chat logs. This is intentional — single
  source of truth lives in the bridge event bus, each session's log is
  a projection.
- Generalizes naturally to N>2 agents: `send_to_peer` already takes a
  list, and the bubble layout has no special-case for "two agents" vs
  "many agents". Rooms can be added later as a thin layer over the same
  primitives if discovery becomes a real problem.
