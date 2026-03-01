module Domain exposing
    ( FileRef, UserMessage
    , Seq(..), AckId(..), Timestamp(..), Version(..)
    , EventType(..), Event
    , QuickReplies
    , Json(..)
    )

{-| Shared types for the agent-chat bridge.

The Go server bridges a browser chat UI and a coding agent.
Communication: Agent <--stdio MCP--> Go Bridge <--WebSocket--> Browser(s)

Source of truth: eventbus.go (types), tools.go (params), main.go (protocol)

@docs FileRef, UserMessage
@docs Seq, AckId, Timestamp, Version
@docs EventType, Event
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
    , mimeType : String -- e.g., "image/png"; defaults to "application/octet-stream"
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


{-| The type field of an Event. Each variant maps to a JSON `type` string.

Source: eventbus.go `Event.Type` field, tools.go `Publish` calls.

-}
type EventType
    = AgentMessage
      {- "agentMessage" -- text from the agent, displayed as chat bubble.
         May include quick_replies and ack_id (for blocking tools).
         May include files (images from agent).
      -}
    | VerbalReply
      {- "verbalReply" -- spoken text from the agent (voice mode).
         Browser uses text-to-speech. Same fields as AgentMessage.
      -}
    | UserMessageEvent
      {- "userMessage" -- broadcast of user's reply to all browsers.
         Also appended to event log for reconnect replay.
      -}
    | DrawEvent



{- "draw" -- canvas drawing instructions.
   Rendered as inline canvas bubble in chat history.
   May include quick_replies and ack_id.
-}


{-| A chat event published through the event bus.

Source: eventbus.go `Event` struct.

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
type alias Event =
    { eventType : EventType
    , seq : Seq
    , text : String
    , ackId : Maybe AckId
    , quickReplies : QuickReplies
    , instructions : List Json
    , files : List FileRef
    , timestamp : Timestamp
    }


{-| Opaque JSON value -- draw instructions are untyped JSON objects.
-}
type Json
    = Json
