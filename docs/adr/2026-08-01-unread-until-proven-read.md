# A user bubble stays unread until the agent proves receipt

**Date:** 2026-08-01
**Status:** Accepted
**Amends:** `docs/adr/2026-05-21-pending-message-lifecycle.md`

## Context

The pending-message lifecycle (2026-05-21) made `userMessagesConsumed` the
boundary between pending and read. That event fires the moment
`WaitForMessagesStamped` / `DrainMessagesStamped` take messages off the queue,
and the browser rendered it as *the agent has read this*.

Those are not the same thing. A blocking `send_message` registers a server-side
waiter; the user can break out of that tool call in the host terminal (Esc /
Ctrl-C), and Claude Code does not reliably send `notifications/cancelled`, so
the MCP request context stays alive. The waiter lives on with nothing behind it.

The observed failure:

1. The agent blocks on `send_message`.
2. The user breaks out in the terminal.
3. The user replies in Agent Chat.
4. The dead waiter drains the reply and publishes `userMessagesConsumed`.
5. The bubble immediately renders as read — and loses its `⋯` menu, because
   that button only exists on `.pending-agent` bubbles.
6. The agent is idle at its prompt and never sees the message. The chat still
   shows the loader. The user is stuck on both sides and has to type
   `check_messages` into the terminal by hand.

Recovery already existed on both sides — `SetLimbo`/`Limbo`/`AckLimbo`
redelivers the lost batch on the next `check_messages`, and the `⋯` menu's
**Send as interrupting** does exactly what the user ends up doing manually. What
was missing was any signal that recovery was needed, and the escape hatch was
hidden at precisely the moment it applied.

## Decision

Keep the existing two-state UI — unread (`.pending-agent`) and read — and move
the boundary. A bubble is unread until the agent **proves** it received the
message; a hand-over off the queue is not proof.

An earlier draft of this change introduced a third bubble state
(`.delivered-agent`, "handed over but unproven") with its own styling. It was
dropped: from the user's side "queued" and "handed to something that may be
dead" are the same fact — *the agent has not seen this* — and the existing
unread treatment already communicates it, with the `⋯` menu and its interrupt
row attached. Collapsing the two reuses the whole concept, its CSS, its menu,
its below-the-loader placement, and its history replay.

### The agent's next chat-tool call is the proof

The reply reaches the agent as the return value of `send_message`, and the
server cannot observe whether that return value was consumed. It *can* observe
the agent's next MCP call on this channel. After a break-out the agent is idle
at its prompt and makes no further agent-chat call, so the absence of that call
is the signal.

`publishConsumed` records the IDs it publishes in `unprovenIDs`.
`bus.ProveDelivery()` — called at the top of all ten agent-chat tool handlers,
before `CancelActiveWait()`, so an early return still counts — publishes
`userMessagesRead` with those IDs and clears the set. The browser clears the
pending state on `userMessagesRead` (previously on `userMessagesConsumed`).

`userMessagesConsumed` keeps its name, its `IDs`, and its
`AgentToolName`/`AgentToolSeq` stamping, so the chat-log exporter and swe-swe's
`/api/fork` resolver are untouched.

### The one thing a hand-over does change

`userMessagesConsumed` sets `data-handed-over="1"` on the bubble, which hides
**Delete**. Unsend removes the message from the agent's queue; once handed over
there is nothing there to remove, so the server would reject it. **Send as
interrupting** stays — after a break-out it is the one-click recovery.

### Limbo is left alone

`send_message` and `send_verbal_reply` deliberately skip `AckLimbo` (the call
might be a recap after a lost delivery). Reusing limbo for read receipts would
strand the happy path as permanently unread, because a normal reply *is* a
`send_message`. The unproven set is tracked separately.

### Server-side consumes are genuine receipts

`PublishConsumedUserMessage` (permission-prompt and ack paths) publishes
`userMessagesRead` immediately after `userMessagesConsumed`. The server itself
read the message; no later call will ever prove it.

### History replay

`replayHistory` builds a `readIds` set beside `deletedIds`/`consumedIds`: a
`userMessage` with no matching `userMessagesRead` replays as pending, and one
that was consumed without being read also gets `data-handed-over`.

## Alternatives considered

- **A distinct third bubble state for "delivered, unproven".** Rejected as
  above: more CSS, more replay branches, more menu gating, for a distinction the
  user cannot act on differently.
- **Auto-fire the interrupt without a click.** Needs the host terminal to
  report when the agent returns to its prompt — a swe-swe-side change that
  degrades to nothing when agent-chat runs outside swe-swe. Deferred.
- **Escalate a long-unread bubble into a visible warning after N seconds.**
  Deferred.
- **Make Claude Code emit `notifications/cancelled` on break-out.** Not ours.
- **Reuse `AckLimbo` as the proof.** Rejected — see above; it would leave the
  happy path permanently unread.

## Consequences

- One new event type: `userMessagesRead`. Consumers that ignore unknown types
  (markdown exporter, older browser tabs) are unaffected.
- A bubble now stays dim for the whole of the agent's turn and clears when the
  agent next speaks, rather than clearing mid-turn at the drain. That is the
  honest reading of what is known.
- A restart clears the unproven set: hand-overs restored from the log are not
  carried forward, so historic bubbles never emit late receipts. The live
  disconnect signal does not survive a restart.
- If the user breaks out and then drives the agent from the terminal on some
  other task, that agent's next chat call proves the delivery even though the
  agent never read the text. Narrower than the original failure — the user is
  in the terminal and can see it — and limbo still holds the message for
  `check_messages`.
- A `send_message` that returns and then ends the turn leaves its bubble unread
  until the next chat call. That is correct: the turn ended, so the reply has
  produced no response yet.
