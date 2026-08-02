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
   websocket frame. The server calls `CancelActiveWait()` — the agent that
   registered that wait is gone, and a parked waiter would swallow the
   instruction into a dead request — then, if the instruction is non-empty,
   queues it via `ReceiveUserMessage`. That publishes an ordinary `userMessage`,
   so `chatLogStream.HandleEvent` appends and `Sync`s it to the same `.md` file
   the session has been writing all along.
3. **Resume.** On `messageQueued` the browser fetches `GET /api/chatlog-path`
   and types `resume <path> - read the whole file for context, then
   check_messages for your instruction (if it returns nothing, the last USER
   entry in the file is your instruction)`.

### Exactly one carrier may be called the instruction

The instruction now exists in two places — the chat log and the queue — and only
one of them may be named as the instruction. The first shipped version named the
file, and the same question got answered twice:

1. The resumed agent read the file, found the question, and answered with
   `send_message`.
2. `send_message` deliberately skips `AckLimbo` (2026-08-01: the call may be a
   recap after a lost delivery), so the un-acked spare copy of the queued message
   survived.
3. The agent's next `check_messages` redelivered that copy behind the
   `---REDELIVERY---` sentinel. Its "ignore if you have already handled these"
   framing did not apply as intended: the agent had answered from the *file* and
   had never received this message through a tool, so it answered again.

The tell was that the two answers were written in different styles — the queued
copy carries the user's message-style template and `renderChatBubble` writes only
raw display text to the `.md`.

The queue wins as the carrier. It brings the style template and the standard
reply instructions, and those instructions are what make the agent post the
receipt-confirming `send_progress` — which acks the spare copy and closes the
loop. The file reverts to what it was always for: history. It is named in the
resume line only as a fallback for a genuinely empty queue, a branch that cannot
double-answer.

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

### The reset writes no bubble

The first version wrote a `⟪ context cleared ⟫` agent bubble at each reset, on
the theory that a future "read only since the last clear" mode would seek to it.
It was removed on first real use: it reads as the agent speaking when the agent
has just ceased to exist, and it costs a bubble in every log for a mode that does
not exist. Re-reading the whole file is the right default while chat logs are
roughly 50x smaller than agent transcripts, and if that mode is ever built the
boundary can be recorded then — as something the chat does not render.

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
- **Resume from the file alone, never touching the queue.** Removes the
  double-answer at the cost of the user's message-style template, which only the
  queued copy carries. Rejected: the first answer after a reset would silently
  ignore the user's stated preferences.
- **Resume with `check_messages` and no file at all.** Rejected — that is the
  path that goes silent when limbo has been acked, and the resumed agent would
  have no history either.
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
- A reset leaves no mark in the chat or in the exported `.md`. Reading the log
  back, a `/clear` is invisible: the conversation simply continues.
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
