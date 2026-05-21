# Pending user message lifecycle and unsend

**Date:** 2026-05-21
**Status:** Accepted

## Context

Agent Chat originally treated a user bubble as both displayed and delivered
as soon as the browser sent it. That was simple, but it hid an important
state boundary: the agent may be busy inside a blocking tool call and may not
have consumed the newly queued message yet.

That ambiguity produced several UX and correctness problems:

1. Users could not tell whether the model had actually seen a message typed
   while the agent was working.
2. A user could not retract a typo during the interval where the message was
   visible in the UI but still unread by the agent.
3. Quick-reply chips could drift visually when pending user bubbles and the
   typing/loading indicator were inserted around the same time.
4. Browser reconnect/history replay needed enough information to avoid
   resurrecting messages that had been withdrawn before delivery.

## Decision

Introduce an explicit pending-message lifecycle for user-authored messages.

### Message identity

Each queued user message gets a stable ID. The ID is carried on the
`UserMessage` stored in the queue and on the corresponding `userMessage`
event broadcast to browsers.

The ID lets later server events refer to the exact bubble they affect without
matching on message text or timestamp.

### Publish before queue

The WebSocket handler routes normal user input through `ReceiveUserMessage`.
That helper publishes the `userMessage` event before queueing the message for
the agent.

Publishing first preserves visual ordering: browsers see the user's bubble
before any immediate consumption signal that may race in from an agent tool
call.

### Consumed state

When `check_messages` drains the queue, or when a blocking receive path such
as `send_message` / `send_verbal_reply` consumes messages, the server emits a
`userMessagesConsumed` event containing the drained message IDs.

Browsers render user bubbles as pending until their IDs appear in
`userMessagesConsumed`:

- pending bubbles are dimmed and positioned below the loading indicator;
- the tooltip says the agent has not seen the message yet;
- once consumed, the bubble returns to normal styling and moves into the
  delivered portion of the transcript.

The Send button also turns orange while the loading indicator is present,
signalling that new text will queue behind in-progress agent work.

### Unsend before consumption

Pending bubbles expose an × control. Clicking it sends an `unsend` message
with the bubble ID.

The server attempts to remove that ID from the queued-but-unread messages. If
successful, it broadcasts `userMessageDeleted`; every connected browser drops
the bubble, and the agent's next `check_messages` will never see it.

Consumed bubbles do not expose ×. Once the model has read the text, deleting
only the UI bubble would misrepresent the conversation state.

### History replay

Replay builds a deleted-ID set from `userMessageDeleted` events and skips
matching historical `userMessage` events. This prevents withdrawn messages
from reappearing after reconnect or refresh.

### Quick-reply placement

Frozen/unused quick-reply chips are anchored to the originating agent bubble
rather than appended relative to the current end of the message list. This
keeps chips associated with the correct turn even when pending user bubbles
and loaders are inserted later.

## Alternatives considered

- **Treat displayed as delivered.** Rejected because it gives users false
  confidence that the agent has read queued text.
- **Allow deleting any user bubble.** Rejected because removing already-read
  text from the UI cannot remove it from the model context and would create a
  misleading transcript.
- **Use text/timestamp matching for unsend.** Rejected because duplicate
  messages and clock/timing races make it unreliable. Stable IDs are simpler
  and precise.
- **Keep quick replies at the end of the list.** Rejected because stale chips
  can become visually attached to a later pending user message rather than the
  agent turn that created them.

## Consequences

- The event protocol now has two additional lifecycle events:
  `userMessagesConsumed` and `userMessageDeleted`.
- Queue removal is best-effort. If the agent drains a message before the
  unsend request arrives, the server reports failure and the browser keeps the
  consumed bubble.
- Reconnect replay is more stateful: clients must account for deleted IDs
  when reconstructing visible history.
- The UI can accurately distinguish “visible in chat” from “read by agent,”
  which is the core semantic improvement.
