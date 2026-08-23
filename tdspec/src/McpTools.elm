module McpTools exposing
    ( McpTool(..)
    , MessageParams, VerbalReplyParams, ProgressParams
    , ToolResult(..)
    )

{-| MCP tools exposed by the agent-chat server.

Source: tools.go (tool registration)

The agent calls these tools over stdio MCP or StreamableHTTP.
Blocking tools (send\_message, send\_verbal\_reply) wait for user
interaction before returning. Non-blocking tools (send\_progress,
send\_verbal\_progress, check\_messages) return immediately.

Message flow:

    Agent -> MCP tool call -> event bus publish -> WebSocket -> browser
    Browser -> user reply -> WebSocket -> message queue -> MCP tool result

All blocking tool results may include a "Chat UI: {url}" suffix
when the HTTP server URL is known.

@docs McpTool
@docs MessageParams, VerbalReplyParams, ProgressParams
@docs ToolResult

-}

import Domain exposing (FileRef, QuickReplies, UserMessage)



-- -- Tools --------------------------------------------------------


{-| All MCP tools registered in registerTools().

Source: tools.go `registerTools` function.

Each variant carries the tool's input parameters.

-}
type McpTool
    = SendMessage MessageParams
      {- Blocking. Send text to chat UI, wait for user reply.
         Rejects with VoiceModeError if user is in voice mode
         (checked via EventBus.LastVoice; must use SendVerbalReply).
         Lazily starts HTTP server and opens browser on first call.
         Waits for at least one browser subscriber before publishing.
         If user has queued messages (HasQueuedMessages), returns them
         immediately with stale quick_replies dropped.
      -}
    | SendVerbalReply VerbalReplyParams
      {- Blocking. Send spoken text to user in voice mode, wait for reply.
         Browser uses text-to-speech to speak the text.
         After speaking, browser automatically listens for next voice input.
         Same blocking/queued-message behavior as SendMessage.
      -}
    | SendProgress ProgressParams
      {- Non-blocking. Publish agentMessage event and return immediately.
         Does not wait for subscriber. Does not block.
      -}
    | SendVerbalProgress ProgressParams
      {- Non-blocking. Publish verbalReply event and return immediately.
         Browser speaks the text via text-to-speech.
         Same params as SendProgress (Go: VerbalProgressParams is
         structurally identical to ProgressParams).
      -}
    | CheckMessages



{- Non-blocking. Drain message queue, return any queued messages.
   Returns NoNewMessages if queue is empty.
   Returns CheckedMessages if messages exist.
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


{-| Parameters for send\_progress and send\_verbal\_progress tools.

Source: tools.go `ProgressParams` and `VerbalProgressParams` structs
(structurally identical).

-}
type alias ProgressParams =
    { text : String
    , imageUrls : List String
    }


{-| What MCP tools return to the agent.

Source: tools.go -- the text content returned in CallToolResult.

All results from blocking tools may have a "\\nChat UI: {url}" suffix
appended when the HTTP server URL is known.

-}
type ToolResult
    = UserResponded
        { formatted : String -- "User responded: {FormatMessages(msgs)}\n\n{replyInstructions}"
        , messages : List UserMessage
        }
      {- Returned by send_message and send_verbal_reply.
         Prefix is "User responded: ".
      -}
    | CheckedMessages
        { formatted : String -- "User said: {FormatMessages(msgs)}\n\n{replyInstructions}"
        , messages : List UserMessage
        }
      {- Returned by check_messages when messages exist.
         Prefix is "User said: " (different from UserResponded).
      -}
    | ProgressSent {- "Progress sent." (send_progress) or "Verbal progress sent." (send_verbal_progress). -}
    | NoNewMessages {- "No new messages." -- check_messages found nothing queued. -}
    | VoiceModeError



{- "ERROR: The user is in voice mode. Use send_verbal_reply instead of send_message to respond."
   Returned by send_message when EventBus.LastVoice() is true.
-}
