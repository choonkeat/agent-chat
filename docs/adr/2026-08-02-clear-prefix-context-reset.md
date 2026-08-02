# `/clear <instruction>` resets the agent and hands the instruction over via the chat log

**Date:** 2026-08-02
**Status:** Accepted
**Builds on:** `docs/adr/2026-08-01-unread-until-proven-read.md`

## Context

A long session fills the agent's context with tool output the user never sees —
files read, commands run, search results. The streaming chat log
(`AGENT_CHAT_EXPORT_DIR`) holds only what was *said*, and is roughly 50× smaller:
across this repo's own logs, the median agent transcript is ~400 KB and the
median chat log ~8 KB. So `/clear` in the host terminal followed by "read
`agent-chats/….md`" is a cheap context reset that keeps the conversation.

Half of it already existed: typing `clear context` and confirming posts
`agent-chat-interrupt` with `/clear` to the parent frame, which types it into the
terminal. What was missing is everything after the wipe — the user had to type
the filename back by hand, and had to know it.

The obvious implementation — intercept a `/clear ` prefix, strip it, post the
rest as a normal chat message, then wipe — loses the message. Two independent
ways:

1. The message is handed to the agent *before* the wipe lands. The agent's first
   `send_progress` calls `AckLimbo`, discarding the spare copy that exists for
   exactly this case, and then the wipe destroys the original. Whether this
   happens depends on whether the agent's first tool call beats a 600 ms
   keystroke sequence.
2. Even wiping first does not release agent-chat's blocking wait: Claude Code
   does not reliably send `notifications/cancelled` on a terminal break-out
   (2026-08-01), so `WaitForMessagesStamped` stays parked on `msgQueue` and
   swallows the next message into a dead request. Limbo recovers it — but only
   if the resumed agent's *first* call is `check_messages`, and the standard
   reply instructions tell it to call `send_progress` first.

Either way the failure is silent: `check_messages` returns
`emptyQueueGuidance`, which explicitly tells the agent to stay quiet and wait.
The chat looks alive and answers nothing.

## Decision

`/clear [instruction]` typed in the chat runs three steps in this order, and the
order is the design:

1. **Wipe.** `agent-chat-interrupt` with `/clear`, exactly as the existing
   confirmed flow does. Sent first so an agent still mid-turn cannot consume the
   instruction and then be erased holding it.
2. **Record.** After `clearWipeSettleMs` (2 s), the browser sends a `clear`
   websocket frame. The server publishes the boundary marker
   (`clearMarkerText`) as an `agentMessage` and, if the instruction is
   non-empty, queues it via `ReceiveUserMessage`. Both are ordinary chat events,
   so `chatLogStream.HandleEvent` appends and `Sync`s them to the same `.md`
   file the session has been writing all along.
3. **Resume.** On `messageQueued` the browser fetches `GET /api/chatlog-path`
   and types `resume <path> - read the whole file; the last USER entry in it is
   your instruction; reply with send_message`.

Step 3 points at the **file**, not at the message queue. That is what removes
both failure modes above: the file cannot be drained by a dead waiter and cannot
be acked away by an eager `send_progress`. Whether the queued copy also survives
becomes a bonus rather than the mechanism.

### The filename is fetched at clear time, not cached at connect

`set_chat_title` renames the `.md` mid-session. A name captured in the connect
handshake would send the resumed agent to a file that no longer exists.
`/api/chatlog-path` answers from `chatStream.MDPath()` on each call, relative to
the working directory so the agent can open it verbatim, and `""` when the
export is disabled — in which case the browser falls back to a plain
`check_messages` nudge and says so in the chat.

### No `@` in the resume line

The line is typed into the agent CLI as keystrokes. A leading `@` on a path
opens its file picker, and the trailing Enter would pick an entry instead of
submitting. Plain paths only.

### The marker is written even though nothing reads it yet

`⟪ context cleared ⟫` is what a future "read only since the last clear" mode
would seek to. Re-reading the whole file is the right default while chat logs
are three orders of magnitude smaller than agent transcripts; writing the marker
from day one means switching later needs no migration of existing logs.

### A bare `/clear` is a reset with no instruction

Nothing is queued, but the marker and the resume line still go out —
`messageQueued` is sent either way, because a browser waiting for it would
otherwise hang. `/` is a live autocomplete trigger and the trigger only dies at
the first space, so a bare `/clear` still has its dropdown open when Enter is
pressed; `isClearCommand` in the keydown handler hides the dropdown and sends
rather than letting a status-only dropdown ("No results") swallow the Enter
with nothing to select.

## Alternatives considered

- **A button in the message bar.** Rejected in favour of the typed prefix: it
  composes with the instruction the user is already typing, and needs no chrome.
- **Resume with `check_messages` instead of a file path.** Rejected — that is
  precisely the path that goes silent when limbo has been acked.
- **Record the instruction before the wipe.** Rejected — a live agent consumes
  it and is then erased mid-thought.
- **Hold the instruction server-side until the resumed agent asks.** A third
  mechanism to keep correct, for a message the file already carries. Deferred.
- **Ask swe-swe for a single "clear then resume" message type.** Would let the
  terminal owner serialise its own keystroke timers instead of the browser
  guessing 2 s. Deferred: it is a cross-repo change, and the existing
  `agent-chat-interrupt` route is not gated by the once-only
  `_chatBootstrapped` switch, so agent-chat can drive both halves alone.

## Consequences

- One new websocket frame type (`clear`) and one new endpoint
  (`/api/chatlog-path`).
- The `/clear ` prefix is now reserved in the chat input. The older
  `clear context` → `yes` flow is untouched and still works.
- The wipe→resume handoff rides on fixed keystroke delays (300 ms inside the
  parent frame, 2 s for the wipe to settle). If an agent CLI takes longer to
  reset, the resume line lands in a screen that is still tearing down and is
  lost. `clearWipeSettleMs` is the single knob.
- With the export disabled there is no file, so `/clear` degrades to a plain
  reset with no history — stated in the chat rather than failing quietly.
- `firstMessageSent` is reset as before, but the parent's `_chatBootstrapped`
  switch is never reset, so the ordinary first-message nudge stays dead after a
  wipe. This flow does not depend on it (it uses the interrupt route), but any
  future feature that does should know.
