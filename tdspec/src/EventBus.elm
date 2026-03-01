module EventBus exposing
    ( AckHandle, AckResult(..)
    , Subscription
    , publish, createAck, resolveAck
    , subscribe, unsubscribe, waitForSubscriber
    , pushMessage, drainMessages, waitForMessages, hasQueuedMessages
    , eventsSince, pendingAckId, lastQuickReplies, history
    , setLastVoice, lastVoice
    , resetLog, logUserMessage, close
    , formatMessages
    )

{-| Event bus -- pub/sub with blocking ack and message queue.

Source: eventbus.go

The event bus is the central hub connecting MCP tools to browser clients:

    MCP tool -> publish -> fan-out to N WebSocket subscribers
    Browser  -> pushMessage -> message queue -> MCP tool reads

Responsibilities:

  - Fan-out: publish events to all connected browser subscribers
  - Blocking ack: createAck/resolveAck for tools that wait for user response
  - Event log: in-memory log for browser reconnect replay
  - Message queue: buffered channel (256) for browser -> agent messages
  - JSONL persistence: optional disk log for history across server restarts
  - Voice mode tracking: lastVoice state determines send\_message rejection

@docs AckHandle, AckResult
@docs Subscription
@docs publish, createAck, resolveAck
@docs subscribe, unsubscribe, waitForSubscriber
@docs pushMessage, drainMessages, waitForMessages, hasQueuedMessages
@docs eventsSince, pendingAckId, lastQuickReplies, history
@docs setLastVoice, lastVoice
@docs resetLog, logUserMessage, close
@docs formatMessages

-}

import Domain exposing (AckId(..), Event(..), FileRef, QuickReplies, Seq(..), UserMessage)



-- -- Ack protocol ------------------------------------------------


{-| Handle returned by createAck. Caller blocks on the channel until
the browser resolves it.

Source: eventbus.go `AckHandle` struct.

-}
type alias AckHandle =
    { id : AckId
    , channel : AckResult -- blocks until resolved
    }


{-| Result received when an ack is resolved.

The browser sends either a bare "ack" (primary button clicked)
or "ack:{message}" (user typed a response or clicked secondary button).

-}
type AckResult
    = Ack
    | AckWithMessage String



-- -- Subscriptions ------------------------------------------------


{-| An event bus subscription. Wraps a buffered channel (capacity 64)
that receives all published events.

Source: eventbus.go `Subscribe()` returns `chan Event`.

-}
type Subscription
    = Subscription



-- -- Publishing ---------------------------------------------------


{-| Publish an event to all subscribers and append to event log.

Sets timestamp to now if zero. Assigns next monotonic seq number.
Tracks lastQuickReplies: set when event has quick\_replies,
cleared when event is userMessage.
Fan-out is non-blocking: if a subscriber channel is full, the event
is dropped for that subscriber.
Also writes to JSONL log file if configured.

Source: eventbus.go `Publish` method.

-}
publish : Event -> ()
publish event =
    let
        _ =
            event
    in
    ()


{-| Create a pending ack. Returns a handle whose channel blocks
until resolveAck is called with the matching id.

Source: eventbus.go `CreateAck` method.

-}
createAck : () -> AckHandle
createAck () =
    { id = AckId "uuid"
    , channel = Ack
    }


{-| Resolve a pending ack by id. Sends the result string through
the channel, unblocking the waiting tool. Returns True if the ack
existed and was resolved.

Source: eventbus.go `ResolveAck` method.
Note: Go takes raw strings ("ack" or "ack:message"), not AckResult.
The AckResult ADT models the parsed form.

-}
resolveAck : { id : AckId, result : AckResult } -> Bool
resolveAck args =
    let
        _ =
            args.id
    in
    True


{-| Subscribe to receive all published events.
Returns a Subscription (buffered channel, capacity 64). Non-blocking
fan-out -- if channel is full, event is dropped for that subscriber.
Call unsubscribe when done.

Source: eventbus.go `Subscribe` method.

-}
subscribe : () -> Subscription
subscribe () =
    Subscription


{-| Remove a subscriber channel, stopping event delivery.

Source: eventbus.go `Unsubscribe` method.

-}
unsubscribe : Subscription -> ()
unsubscribe sub =
    let
        _ =
            sub
    in
    ()


{-| Poll until at least one subscriber is connected.
Timeout: 30 seconds. Poll interval: 100ms.

Used by blocking MCP tools (send\_message, send\_verbal\_reply, draw)
to ensure at least one browser is watching before publishing.

Source: eventbus.go `WaitForSubscriber` method.

-}
waitForSubscriber : () -> Result String ()
waitForSubscriber () =
    Ok ()



-- -- Message queue ------------------------------------------------


{-| Queue a user message from the browser.
Buffered channel capacity: 256. On overflow, drops oldest message.

Source: eventbus.go `PushMessage` method.

-}
pushMessage : { text : String, files : List FileRef } -> ()
pushMessage msg =
    let
        _ =
            msg.text
    in
    ()


{-| Non-blocking drain of all queued user messages.
Returns empty list if queue is empty.

Used by check\_messages tool.

Source: eventbus.go `DrainMessages` method.

-}
drainMessages : () -> List UserMessage
drainMessages () =
    []


{-| Block until at least one message is queued, then drain all.

Used by send\_message and send\_verbal\_reply tools.

Source: eventbus.go `WaitForMessages` method.

-}
waitForMessages : () -> Result String (List UserMessage)
waitForMessages () =
    Ok []


{-| Returns True if there are user messages waiting in the queue.

Used by blocking tools (send\_message, send\_verbal\_reply, draw)
to short-circuit: if messages are already queued, skip quick\_replies
and return immediately since the replies would be stale.

Source: eventbus.go `HasQueuedMessages` method.

-}
hasQueuedMessages : () -> Bool
hasQueuedMessages () =
    False



-- -- Voice mode tracking -----------------------------------------


{-| Record whether the last consumed user messages contained voice input.

Voice messages are identified by a microphone emoji prefix on the text.
This state is checked by send\_message to reject calls when user is in
voice mode (must use send\_verbal\_reply instead).

Source: eventbus.go `SetLastVoice` method.

-}
setLastVoice : Bool -> ()
setLastVoice voice =
    let
        _ =
            voice
    in
    ()


{-| Returns True if the last consumed user messages contained voice input.

Used by send\_message tool to reject with VoiceModeError.

Source: eventbus.go `LastVoice` method.

-}
lastVoice : () -> Bool
lastVoice () =
    False



-- -- History and state --------------------------------------------


{-| Return all events with seq > cursor.
Used by WebSocket handler to stream missed events on reconnect.

Source: eventbus.go `EventsSince` method.

-}
eventsSince : Seq -> List Event
eventsSince cursor =
    let
        _ =
            cursor
    in
    []


{-| Return the first pending ack id, or Nothing.
Sent in the WebSocket connected handshake so browser can re-enable
input for an in-progress blocking tool.

Source: eventbus.go `PendingAckID` method.

-}
pendingAckId : () -> Maybe AckId
pendingAckId () =
    Nothing


{-| Return the last quick\_replies sent to browser, or empty.
Tracks whether the agent is waiting for input (has replies)
or working (no replies). Sent in connected handshake.

Source: eventbus.go `LastQuickReplies` method.

-}
lastQuickReplies : () -> QuickReplies
lastQuickReplies () =
    []


{-| Return a copy of the full event log and the pending ack ID (if any).

Source: eventbus.go `History` method.

-}
history : () -> { events : List Event, pendingAckId : Maybe AckId }
history () =
    { events = [], pendingAckId = Nothing }


{-| Clear the in-memory event log.

Source: eventbus.go `ResetLog` method.

-}
resetLog : () -> ()
resetLog () =
    ()


{-| Append a userMessage event to the log for reconnect replay WITHOUT
assigning a sequence number or fanning out to subscribers.

These log-only events have Seq == 0 and are not broadcast.

Note: this method is defined in eventbus.go but is not currently called
from main.go -- the WebSocket handler uses Publish instead. May be
dead code or reserved for future use.

Source: eventbus.go `LogUserMessage` method.

-}
logUserMessage : { text : String, files : List FileRef } -> ()
logUserMessage msg =
    let
        _ =
            msg.text
    in
    ()


{-| Flush and close the JSONL log file.

Source: eventbus.go `Close` method.

-}
close : () -> ()
close () =
    ()



-- -- Message formatting -------------------------------------------


{-| Format user messages into a single string for tool results.

Joins multiple messages, strips voice emoji prefix, includes file
attachment info (path, MIME type, size). Uses the "format-messages"
Go template.

Free function (not a method on EventBus), defined in eventbus.go.

Source: eventbus.go `FormatMessages` function.

-}
formatMessages : List UserMessage -> String
formatMessages msgs =
    let
        _ =
            msgs
    in
    ""
