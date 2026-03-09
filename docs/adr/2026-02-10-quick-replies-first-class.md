# Quick Replies as First-Class Chat Element

**Date:** 2026-02-10
**Status:** Accepted

## Context

The agent sends quick reply options (chip buttons) with messages so users can
respond with a single tap. These need to persist in the conversation history
so users can see what options were available at each point.

## Decision

Treat quick replies as a first-class part of the event model:

1. **Event bus stores `lastQuickReplies`** — tracks the most recent set of
   quick reply options, reset on user message.

2. **Freeze on selection** — when a user picks a quick reply (or types a custom
   response), the unchosen options are "frozen" as inert chips in the message
   flow, showing what alternatives existed.

3. **Strip if user already queued messages** — if the user sent a message while
   the agent was composing, quick replies are stripped from the response (the
   user already moved on).

4. **Defer on reconnect** — during history replay, quick replies are deferred
   until `historyEnd` to avoid prematurely freezing them.

## Alternatives Considered

- **Ephemeral chips** — disappear after selection; loses conversation context.
- **Always show all options** — clutters the chat with stale choices.
- **No quick replies** — forces users to type everything; poor mobile/voice UX.

## Consequences

- Users see a clear record of what options were offered and what was chosen.
- Frozen replies provide context when reviewing conversation history.
- Quick replies auto-hide when the user has already moved the conversation
  forward, preventing stale options.
