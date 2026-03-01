module EventBus exposing
    ( AckHandle, AckResult(..)
    , publish, createAck, resolveAck
    , subscribe, waitForSubscriber
    , pushMessage, drainMessages, waitForMessages
    , eventsSince, pendingAckId, lastQuickReplies
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

@docs AckHandle, AckResult
@docs publish, createAck, resolveAck
@docs subscribe, waitForSubscriber
@docs pushMessage, drainMessages, waitForMessages
@docs eventsSince, pendingAckId, lastQuickReplies

-}

import Domain exposing (AckId(..), Event, FileRef, QuickReplies, Seq(..), UserMessage)



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



-- -- Publishing ---------------------------------------------------


{-| Publish an event to all subscribers and append to event log.

Sets timestamp to now if zero. Assigns next monotonic seq number.
Tracks lastQuickReplies: set when event has quick\_replies,
cleared when event is userMessage.

Source: eventbus.go `Publish` method.

-}
publish : Event -> ()
publish event =
    let
        _ =
            event.seq
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
the channel, unblocking the waiting tool.

Source: eventbus.go `ResolveAck` method.

-}
resolveAck : { id : AckId, result : AckResult } -> Bool
resolveAck args =
    let
        _ =
            args.id
    in
    True



-- -- Subscriptions ------------------------------------------------


{-| Subscribe to receive all published events.
Returns a buffered channel (capacity 64). Non-blocking fan-out --
if channel is full, event is dropped for that subscriber.

Source: eventbus.go `Subscribe` method.

-}
subscribe : () -> ()
subscribe () =
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
