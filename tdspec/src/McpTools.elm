module McpTools exposing
    ( McpTool(..), McpResource(..)
    , MessageParams, VerbalReplyParams, DrawParams, ProgressParams
    , ToolResult(..)
    , allResources
    )

{-| MCP tools and resources exposed by the agent-chat server.

Source: tools.go (tool registration), resources.go (resource registration)

The agent calls these tools over stdio MCP or StreamableHTTP.
Blocking tools (send\_message, send\_verbal\_reply, draw) wait for user
interaction before returning. Non-blocking tools (send\_progress,
send\_verbal\_progress, check\_messages) return immediately.

Message flow:

    Agent -> MCP tool call -> event bus publish -> WebSocket -> browser
    Browser -> user reply -> WebSocket -> message queue -> MCP tool result

@docs McpTool, McpResource
@docs MessageParams, VerbalReplyParams, DrawParams, ProgressParams
@docs ToolResult
@docs allResources

-}

import Domain exposing (FileRef, Json, QuickReplies, UserMessage)



-- -- Tools --------------------------------------------------------


{-| All MCP tools registered in registerTools().

Source: tools.go `registerTools` function.

Each variant carries the tool's input parameters.

-}
type McpTool
    = SendMessage MessageParams
      {- Blocking. Send text to chat UI, wait for user reply.
         Rejects with error if user is in voice mode (must use SendVerbalReply).
         Lazily starts HTTP server and opens browser on first call.
         Waits for at least one browser subscriber before publishing.
         If user has queued messages, returns them immediately (stale quick_replies dropped).
      -}
    | SendVerbalReply VerbalReplyParams
      {- Blocking. Send spoken text to user in voice mode, wait for reply.
         Browser uses text-to-speech to speak the text.
         After speaking, browser automatically listens for next voice input.
         Same blocking/queued-message behavior as SendMessage.
      -}
    | Draw DrawParams
      {- Blocking. Draw a diagram slide as inline canvas bubble in chat.
         Publishes text as agentMessage bubble first, then draw event.
         Blocks until viewer clicks ack button (resolves via ack protocol).
         If user has queued messages, shows draw without ack and returns immediately.
      -}
    | SendProgress ProgressParams
      {- Non-blocking. Publish agentMessage event and return immediately.
         Does not wait for subscriber. Does not block.
      -}
    | SendVerbalProgress ProgressParams
      {- Non-blocking. Publish verbalReply event and return immediately.
         Browser speaks the text via text-to-speech.
         Same params as SendProgress (Go: VerbalProgressParams is identical to ProgressParams).
      -}
    | CheckMessages



{- Non-blocking. Drain message queue, return any queued messages.
   Returns "No new messages." if queue is empty.
   Returns "User said: {formatted}" if messages exist.
   Takes no parameters (Go: EmptyParams struct).
-}


{-| Parameters for send\_message tool.

Source: tools.go `MessageParams` struct.

-}
type alias MessageParams =
    { text : String
    , quickReply : String
    , moreQuickReplies : List String
    , imageUrls : List String -- absolute paths to local image files
    }


{-| Parameters for send\_verbal\_reply tool.

Source: tools.go `VerbalReplyParams` struct.
Same shape as MessageParams.

-}
type alias VerbalReplyParams =
    { text : String
    , quickReply : String
    , moreQuickReplies : List String
    , imageUrls : List String
    }


{-| Parameters for draw tool.

Source: tools.go `DrawParams` struct (local to registerTools).

-}
type alias DrawParams =
    { text : String -- caption displayed as chat bubble before canvas
    , instructions : List Json -- drawing instruction objects
    , quickReply : String
    , moreQuickReplies : List String
    }


{-| Parameters for send\_progress and send\_verbal\_progress tools.

Source: tools.go `ProgressParams` and `VerbalProgressParams` structs.

-}
type alias ProgressParams =
    { text : String
    , imageUrls : List String
    }


{-| What blocking tools return to the agent.

Source: tools.go -- the text content returned in CallToolResult.

-}
type ToolResult
    = UserResponded
        { formatted : String -- "User responded: {FormatMessages(msgs)}\n\n{voiceSuffix}"
        , messages : List UserMessage
        }
    | DrawAcknowledged
    | DrawFeedback String -- viewer typed a response
    | ProgressSent -- "Progress sent." or "Verbal progress sent."
    | NoNewMessages -- "No new messages."
    | VoiceModeError -- "ERROR: The user is in voice mode. Use send_verbal_reply..."



-- -- Resources ----------------------------------------------------


{-| MCP resources exposed by the server.

Source: resources.go `registerResources` function.
URI scheme: `whiteboard://{name}`

-}
type McpResource
    = WhiteboardInstructions
      {- whiteboard://instructions (instruction-reference.md)
         Complete reference of all drawing instruction types.
      -}
    | WhiteboardDiagrammingGuide
      {- whiteboard://diagramming-guide (diagramming-guide.md)
         Layout rules, cognitive principles for diagrams.
      -}
    | WhiteboardQuickReference



{- whiteboard://quick-reference (quick-reference.md)
   Condensed cheat sheet for drawing.
-}


{-| All registered resources.

    allResources == [ WhiteboardInstructions, WhiteboardDiagrammingGuide, WhiteboardQuickReference ]

-}
allResources : List McpResource
allResources =
    [ WhiteboardInstructions, WhiteboardDiagrammingGuide, WhiteboardQuickReference ]
