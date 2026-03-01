module Domain exposing
    ( FileRef, UserMessage
    , Seq(..), AckId(..), Timestamp(..), Version(..)
    , Event(..), ChatMessageData, UserMessageData, DrawEventData
    , QuickReplies
    , Json(..)
    )

{-| Shared types for the agent-chat bridge.

The Go server bridges a browser chat UI and a coding agent.
Communication: Agent <--stdio MCP--> Go Bridge <--WebSocket--> Browser(s)

Source of truth: eventbus.go (types), tools.go (params), main.go (protocol)

@docs FileRef, UserMessage
@docs Seq, AckId, Timestamp, Version
@docs Event, ChatMessageData, UserMessageData, DrawEventData
@docs QuickReplies
@docs Json

-}


{-| Uploaded file metadata.

Source: eventbus.go `FileRef` struct.

    type FileRef struct {
        Name string `json:"name"`
        Path string `json:"path"`
        URL  string `json:"url"`
        Size int64  `json:"size"`
        Type string `json:"type,omitempty"`
    }

-}
type alias FileRef =
    { name : String
    , path : String -- absolute server path
    , url : String -- relative URL for browser (e.g., "/uploads/abc-photo.png")
    , size : Int -- bytes
    , contentType : Maybe String -- MIME type, e.g., "image/png". JSON wire name: "type" (omitempty)
    }


{-| A text message with optional file attachments from the browser.

Source: eventbus.go `UserMessage` struct.

-}
type alias UserMessage =
    { text : String
    , files : List FileRef
    }


{-| Monotonic sequence number assigned to each published event.
Used by browser for reconnect cursor (resume from last seen seq).
-}
type Seq
    = Seq Int


{-| UUID identifying a pending acknowledgment.
Created by the event bus, sent to browser, returned in ack messages.
-}
type AckId
    = AckId String


{-| Unix milliseconds timestamp. Set on publish if not already present.
-}
type Timestamp
    = Timestamp Int


{-| Server version string, e.g., "0.1.6 (bcaedff)".
Set at build time via ldflags: `-X main.version=... -X main.commit=...`
-}
type Version
    = Version String


{-| Suggested reply buttons shown to the user.
First element is the primary reply; rest are alternatives.
-}
type alias QuickReplies =
    List String


{-| A chat event published through the event bus.

Source: eventbus.go `Event` struct. The Go struct is flat (all fields
optional), but each event type uses a specific subset of fields.
The ADT makes these constraints explicit.

    type Event struct {
        Type         string    `json:"type"`
        Seq          int64     `json:"seq"`
        Text         string    `json:"text,omitempty"`
        AckID        string    `json:"ack_id,omitempty"`
        QuickReplies []string  `json:"quick_replies,omitempty"`
        Instructions []any     `json:"instructions,omitempty"`
        Files        []FileRef `json:"files,omitempty"`
        Timestamp    int64     `json:"ts,omitempty"`
    }

-}
type Event
    = AgentMessage ChatMessageData
      {- "agentMessage" -- text from the agent, displayed as chat bubble.
         May include quick_replies and ack_id (for blocking tools).
         May include files (images from agent).
      -}
    | VerbalReply ChatMessageData
      {- "verbalReply" -- spoken text from the agent (voice mode).
         Browser uses text-to-speech. Same payload as AgentMessage.
      -}
    | UserMessageEvent UserMessageData
      {- "userMessage" -- broadcast of user's reply to all browsers.
         Also appended to event log for reconnect replay.
      -}
    | DrawEvent DrawEventData



{- "draw" -- canvas drawing instructions.
   Rendered as inline canvas bubble in chat history.
   May include quick_replies and ack_id.
   Note: draw events do NOT carry text -- the text goes in a
   separate AgentMessage event published immediately before the draw.
-}


{-| Payload for AgentMessage and VerbalReply events.
-}
type alias ChatMessageData =
    { seq : Seq
    , timestamp : Timestamp
    , text : String
    , ackId : Maybe AckId
    , quickReplies : QuickReplies
    , files : List FileRef
    }


{-| Payload for UserMessageEvent.
-}
type alias UserMessageData =
    { seq : Seq
    , timestamp : Timestamp
    , text : String
    , files : List FileRef
    }


{-| Payload for DrawEvent.

Note: does NOT include a text field. The caption text is published
as a separate AgentMessage event before the draw (see tools.go draw handler).

-}
type alias DrawEventData =
    { seq : Seq
    , timestamp : Timestamp
    , instructions : List Json
    , ackId : Maybe AckId
    , quickReplies : QuickReplies
    }


{-| Opaque JSON value -- draw instructions are untyped JSON objects.
-}
type Json
    = Json
