# Bug: "Fork from here" is offered on progress bubbles

Date: 2026-07-01
Status: resolved (client-only; `agent_tool_name` already on the wire)
Kind: bug / UI gating
Related: 2026-06-27-per-bubble-fork-button.md

## Summary

The per-bubble overflow menu ("Speak aloud" + "Fork from here") is shown on
**every** agent bubble that has a server seq -- including bubbles produced by
`send_progress` / `send_verbal_progress`. Those are non-blocking mid-turn
status updates, not conversation turn boundaries, and must not be forkable.

## Why it matters

`send_progress` publishes `Event{Type:"agentMessage", AgentToolName:"send_progress"}`
(see `tools.go`) -- i.e. the same event `Type` as a real `send_message` reply;
only `AgentToolName` distinguishes them. Downstream, the swe-swe-server fork
resolver keys off `AgentToolName`, so a fork anchored on a progress bubble
resolves to the `send_progress` tool_use and cuts forkconvo **mid-turn** (the
agent kept working after that update). That is a silent wrong-cut, worse than a
visible error.

The swe-swe-server side now rejects progress anchors defensively
(`ErrProgressBubbleNotForkable`, in swe-swe repo `fork_resolve.go`), but the UI
should not *offer* the doomed action in the first place.

## Root cause (client)

`client-dist/app.js`:
- `addBubble(text, role, files, extraClass, timestamp, messageId, seq)`
  (~line 468) decides menu vs. plain TTS button purely on
  `role === 'agent' && forkSession && seq` (~lines 494-500). It never receives
  the bubble's `agent_tool_name`, so it cannot distinguish `send_progress` from
  `send_message`.
- `openBubbleMenuFor` (~line 391) unconditionally appends the "Fork from here"
  item (~line 406).

## Fix

1. Plumb `agent_tool_name` (and/or a boolean `forkable`) from the WS event
   payload into `addBubble` -> `createMenuButton` -> `openBubbleMenuFor`.
2. Only include the "Fork from here" item when the tool is a reply tool
   (`send_message` / `send_verbal_reply`). For progress bubbles either drop the
   fork item (keep "Speak aloud") or fall back to the plain TTS button.
3. Confirm the server already forwards `agent_tool_name` on the WS event; if
   not, add it to the outbound event JSON (it is already on the internal
   `Event` struct).

## Verification

- E2E (extend `e2e/fork-button.spec.cjs`): a `send_progress` agent bubble shows
  no "Fork from here" item (or no ⋯ menu); a `send_message` bubble still does.
- `make test`.

## Note

A `send_message` bubble whose tool_use has not yet been flushed to the agent
transcript (the agent is still blocked inside it) is a *separate* concern and is
already handled server-side by falling back to the last-persisted-reply anchor
(`ErrAnchorNotYetPersisted`). No client change needed for that case.
