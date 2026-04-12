# MCP Prompt Rewrite and first_quick_reply Schema Convention

**Date:** 2026-04-07
**Status:** Accepted

## Context

Three recurring failure modes were observed in agent sessions after the
0.1.x series shipped:

1. **Agents stalling or replying in the invisible TUI.**  The reply
   instructions told the agent to "summarise what you'll do" and "ensure
   mutual understanding" before every action, but the TUI-invisibility
   warning was conditional on message type.  Agents would sometimes emit
   their summary as plain TUI text instead of calling `send_message`,
   leaving the user staring at a silent chat.

2. **Agents ending their turn silently after the user responded.**  The
   `send_message` tool returned the user's reply as a tool result, but
   nothing reminded the agent to call `send_message` again when it was
   done.  The agent would finish its work and return without a final
   message — the user saw nothing.

3. **Agents JSON-encoding arrays into the `quick_reply` string field.**
   The schema had `quick_reply` (a string) alongside `more_quick_replies`
   (an array).  The identical naming pattern led LLMs to serialize
   `["Yes", "No"]` into the string field, producing a broken UI button
   labelled `["Yes", "No"]`.

## Decision

### Prompt rewrite

Replace the verbose confirmation-first instructions with a concise block:

- TUI invisibility is stated unconditionally for **all** message types.
- Confirmation is only required for "ambiguous, risky, or destructive"
  requests — routine work proceeds immediately.
- Every turn must end with a `send_message` (or `send_verbal_reply`) call.
- The per-voice-message "Confirm your understanding" boilerplate is
  removed; the reply-instructions block covers it.

### Closing reminder in tool result

The text returned by `send_message` and `send_verbal_reply` after the user
responds now appends: *"Address this response now.  When your work is done,
call send_message … again to deliver the result — never end your turn
without sending a user-visible message."*  This catches the stalling case
even if the agent ignores the system prompt.

### `quick_reply` → `first_quick_reply`

Rename the JSON field on `send_message`, `send_verbal_reply`, and `draw`
from `quick_reply` to `first_quick_reply`.  The `first_*` / `more_*`
naming pattern makes cardinality unambiguous:

| Old | New | Type |
|-----|-----|------|
| `quick_reply` | `first_quick_reply` | string (scalar) |
| `more_quick_replies` | `more_quick_replies` | string[] |

Each tool description now explicitly states: *"Do NOT pass a JSON-encoded
array as `first_quick_reply`; it must be a plain string."*

This is a **breaking change** for agents that hard-code the old field name.
The Go struct tag changes from `json:"quick_reply"` to
`json:"first_quick_reply"`.  Agents using the tool schema dynamically
(which all MCP-conformant agents should) will pick up the new name
automatically.

## Alternatives Considered

- **Keep `quick_reply` and add validation.**  Rejected: the ambiguous name
  was the root cause.  Validation would produce a runtime error instead of
  a broken button, but wouldn't prevent the mistake.
- **Accept arrays in `quick_reply` and merge with `more_quick_replies`.**
  Rejected: this hides the schema mismatch from the agent, encouraging
  continued misuse and making the contract harder to reason about.
- **Keep the verbose confirmation-first prompt.**  Rejected: it slowed
  every interaction and agents frequently ignored it anyway, producing the
  worst of both worlds — slow AND unreliable.

## Consequences

- Agents using the old `quick_reply` field name will silently drop the
  suggested reply (Go's JSON unmarshalling ignores unknown fields).  This
  degrades gracefully — the message still appears, just without a
  suggested reply button.
- The closing reminder in tool results is redundant with the system prompt.
  This is intentional: belt-and-suspenders for a failure mode that leaves
  users stuck.
- The prompt rewrite removes the blanket "confirm before acting" rule.
  Agents may now execute routine tasks without asking first, which is the
  desired behavior but changes the interaction style from 0.1.x.
