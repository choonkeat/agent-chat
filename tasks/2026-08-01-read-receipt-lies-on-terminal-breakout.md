# Bug: a user bubble turns "read" (blue) when nothing actually received it

**Status: implemented 2026-08-01, with one deviation** — see
`docs/adr/2026-08-01-unread-until-proven-read.md`. The server design below
shipped as written (`ProveDelivery` / `userMessagesRead` in `eventbus.go`,
called at all ten tool entries in `tools.go`). The browser design did NOT: the
proposed third state `.delivered-agent` was dropped in favour of reusing the
existing unread bubble, which already says "the agent has not seen this" and
already carries the `⋯` interrupt row. A hand-over now sets only
`data-handed-over="1"`, which hides Delete; `userMessagesRead` is what clears
the pending state. Tests: 8 in `eventbus_test.go`, a structural guard in
`tools_test.go`, and `e2e/read-receipt-states.spec.cjs`. The "out of scope"
items below remain open.

Filed 2026-08-01. Reported against the live session (agent-chat inside a
swe-swe Agent Chat pane, host terminal = Agent Terminal running Claude Code).

## Symptom

1. The agent calls `send_message` and blocks waiting for a reply.
2. The user switches to Agent Terminal and breaks out of the pending tool call
   (Esc / Ctrl-C).
3. The user types a reply in Agent Chat and sends it.
4. The bubble **immediately loses its pending styling** — it reads as delivered
   and read.
5. The agent never sees it. It is idle at its prompt, unaware.
6. Agent Chat is now showing the loader (agent "working"), so the user is stuck
   on both sides. The only way out is manually typing `check_messages` into
   Agent Terminal.

Reproduced live during the session that filed this report.

## Root cause

Two independent facts combine.

### 1. Breaking out of the tool call does not cancel the server-side wait

`eventbus.go:462-492` — `BeginBlockingWait` / `CancelActiveWait`.

`send_message` registers itself as *the* active waiter and blocks in
`WaitForMessagesStamped` (`eventbus.go:409-429`). Claude Code aborting a tool
call client-side does not reliably send a `notifications/cancelled`, so the MCP
request context stays alive. The waiter keeps sitting on the queue with no
client behind it — a zombie.

This is already known and partially handled: `SetLimbo` / `Limbo` /
`AckLimbo` (`eventbus.go:432-455`) retain the last delivered batch precisely so
`check_messages` (`tools.go:673-704`) can redeliver it. That recovery path
works. What is missing is any *signal to the user* that recovery is needed.

### 2. The "read" broadcast fires at hand-over, not at receipt

`eventbus.go:338-360` — `publishConsumed`.

`WaitForMessagesStamped` publishes `userMessagesConsumed` the instant it takes
messages off the queue (`eventbus.go:424`). The browser
(`client-dist/app.js:3009-3013` → `markMessagesConsumed`, lines 1246-1261)
treats that event as "the agent has read this": it strips `.pending-agent`,
removes the `⋯` menu, and re-parents the bubble above the loader.

So a zombie waiter grabbing the message produces exactly the same broadcast as a
live agent reading it. `userMessagesConsumed` currently means *handed over*, but
the UI renders it as *read*.

### 3. The escape hatch is hidden at the exact moment it is needed

`client-dist/app.js:513-542` — `openPendingMenuFor` already offers **"Send as
interrupting"**, which posts `agent-chat-interrupt` to the parent frame
(`interruptWithPendingMessage`, lines 570-582). That does precisely what the
user ends up doing by hand: Esc-Esc plus `check_messages` into the terminal.

But the `⋯` button only exists on `.pending-agent` bubbles
(`client-dist/app.js:682-690`, `style.css:794-800`). Because the zombie
hand-over clears `.pending-agent`, the button disappears at the one moment it
would fix the problem.

## Design: three states, not two

Split the current two-state model into three. The wire event that exists today
keeps its name and its meaning; a second event is added for the promotion.

| State | Meaning | Bubble |
|---|---|---|
| 1. queued | in the agent's queue, nobody has taken it | dim (`.pending-agent`, as today) |
| 2. handed over | taken by a waiting request; **no proof it arrived** | new `.delivered-agent` style — outline / lighter than normal, distinct from dim |
| 3. read | the agent made a chat-tool call *after* the hand-over | normal (today's cleared style) |

State 2 is the disconnect indicator. In the failure above the bubble stops at
state 2 and stays there, and the `⋯` menu stays available on it.

### Why "the agent's next chat-tool call" is the right proof

The reply reaches the agent as the return value of `send_message`; the server
cannot observe whether that return value was consumed. But it *can* observe the
agent's next MCP call on this channel. After a break-out the agent is idle at
its prompt and makes no further agent-chat call, so the absence of that call is
the signal.

**Do not reuse `AckLimbo` for this.** `send_message` and `send_verbal_reply`
deliberately skip `AckLimbo` (`tools.go:340-344`: "this call might be a recap
after a lost delivery"). Reusing it would leave the happy path permanently
stuck at state 2, because a normal reply *is* a `send_message`. Limbo semantics
stay exactly as they are.

## Changes

### Server — `eventbus.go`

1. Add unproven-delivery tracking alongside limbo:

   ```go
   unprovenMu  sync.Mutex
   unprovenIDs []string
   ```

2. `publishConsumed` (line 343) additionally records the IDs it just published
   as unproven. No change to the event it emits — `userMessagesConsumed` keeps
   its name, its `IDs`, and its `AgentToolName` / `AgentToolSeq` stamping, so
   the chat-log exporter (`chatlogstream.go`) and swe-swe's `/api/fork`
   resolver are untouched.

3. New `ProveDelivery()`: if `unprovenIDs` is non-empty, publish
   `Event{Type: "userMessagesRead", IDs: ...}` and clear. No-op otherwise.

4. `PublishConsumedUserMessage` (line 331) — the permission-prompt and ack
   paths, where the server itself consumed the message — publishes
   `userMessagesRead` immediately after `userMessagesConsumed`. Those are
   genuine receipts; they must not sit at state 2.

5. Startup rehydration (`pendingUserMessages`, lines 190-215) is unchanged: the
   agent queue is still rebuilt from `userMessage` minus `userMessagesConsumed`
   minus `userMessageDeleted`. Additionally, after restoring the log, clear
   `unprovenIDs` — a restart is not evidence of a live disconnect, and leaving
   old hand-overs unproven would strand historic bubbles at state 2 forever.

### Server — `tools.go`

Call `bus.ProveDelivery()` as the **first statement** of every agent-chat tool
handler, before `CancelActiveWait()`. The call itself is the proof, so it must
be recorded even if the handler later returns early (voice-mode rejection at
line 346, etc.).

Ten entry points, currently identified by their `CancelActiveWait()` calls:

| Tool | line |
|---|---|
| `send_message` | 344 |
| `send_verbal_reply` | 433 |
| `draw` | 533 |
| `send_progress` | 624 |
| `send_verbal_progress` | 653 |
| `check_messages` | 680 |
| `set_chat_title` | 714 |
| `chatlog_close` | 754 |
| `chatlog_optout` | 787 |
| `export_chat_md` | 812 |

`check_messages` needs care about ordering: prove the *previous* delivery
first, then capture limbo and drain. The drain's own `publishConsumed` then
registers the fresh batch as unproven.

### Browser — `client-dist/app.js`

1. `markMessagesConsumed` (line 1246) becomes `markMessagesDelivered`: swap
   `.pending-agent` for `.delivered-agent`, re-parent above the loader (as
   today — the message has genuinely left the queue, so chronological order is
   correct), keep the `⋯` button, update the tooltip to *"Sent, but the agent
   hasn't acknowledged it"*.

2. New handler for `userMessagesRead` in the event switch (near line 3009):
   remove `.delivered-agent`, remove the tooltip, remove the `⋯` button. This
   is today's `markMessagesConsumed` body.

3. `openPendingMenuFor` (line 513) — gate the two items by state:
   - **Delete** (`sendUnsend`, line 697) only in state 1. In state 2 the
     message has already left the queue, so the server's unsend would fail
     (`app.js:2998-3004` already logs that race).
   - **Send as interrupting** in both states 1 and 2.

4. `isLastPendingBubble` (line 508) must match both `.pending-agent` and
   `.delivered-agent`, so the interrupt item appears on the bottom-most
   unacknowledged bubble regardless of state.

5. `replayHistory` (lines 2777-2830) — add a `readIds` map beside the existing
   `deletedIds` / `consumedIds` (lines 2777-2785) and render each restored user
   bubble into the correct one of the three states.

### Browser — `client-dist/style.css`

Add `.bubble.user.delivered-agent` next to `.pending-agent` (line 794).
Visually between dim and normal, and clearly *not* the same as dim — e.g. full
opacity with a dashed/soft border, so "queued" and "unacknowledged" are
distinguishable at a glance. Extend the `⋯` reveal selectors (line 826) to the
new class.

## Edge cases

1. **User breaks out, then drives the agent from the terminal on some other
   task.** That agent's next chat-tool call proves the delivery and the bubble
   goes blue even though the agent never read the text. Narrower than today's
   failure — the user was in the terminal and knows — and the limbo copy still
   holds the message for `check_messages`.
2. **Server restart while a bubble is at state 2.** Cleared to state 3 on
   rehydration (see server change 5). Accepted: the live-disconnect signal does
   not survive a restart.
3. **Multiple browser tabs.** `userMessagesRead` is a normal broadcast; all
   tabs converge. A tab that connects late rebuilds from the log via
   `replayHistory`.
4. **File-only messages** (no text) already carry an ID and a pending bubble;
   nothing state-specific changes for them.
5. **`send_message` returning normally then the agent ending its turn without
   another chat call.** Bubble stays at state 2 until the agent's next chat
   call. That is correct — the turn ended, so the reply genuinely produced no
   response yet.

## Verification

Unit (`make unit-test`):

- `eventbus_test.go` — a drain publishes `userMessagesConsumed` and **no**
  `userMessagesRead`; a subsequent `ProveDelivery()` publishes
  `userMessagesRead` with the same IDs; a second `ProveDelivery()` is a no-op.
- `eventbus_test.go` — `PublishConsumedUserMessage` emits both events.
- `stamp_test.go` — `userMessagesConsumed` stamping (`AgentToolName` /
  `AgentToolSeq`) is unchanged.
- restart test — a log containing `userMessage` + `userMessagesConsumed` with
  no `userMessagesRead` rehydrates with an empty unproven set.

E2E (`make e2e-test`; warm CDP first with an MCP `browser_navigate`):

- send a message while the agent is blocked → bubble reaches state 2, `⋯` menu
  present, **Delete** absent, **Send as interrupting** present.
- simulate the agent's next tool call → bubble reaches state 3, `⋯` gone.
- reload mid-state-2 → bubble replays at state 2.

Manual reproduction of the original bug:

1. Agent blocks on `send_message`.
2. Esc out in Agent Terminal.
3. Reply in Agent Chat.
4. Expect: bubble stops at state 2, does not go blue, `⋯` → "Send as
   interrupting" recovers without typing anything into the terminal.

## Out of scope

- Auto-firing the interrupt without a click. That needs the host terminal to
  report when the agent returns to its prompt — a swe-swe-side change
  (`/workspace/tasks/`), and it degrades to nothing when agent-chat runs
  outside swe-swe. File separately once state 2 exists.
- A timeout that escalates state 2 into a visible warning after N seconds.
- Making Claude Code emit `notifications/cancelled` on break-out. Not ours.
