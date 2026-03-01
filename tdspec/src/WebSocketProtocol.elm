module WebSocketProtocol exposing
    ( ServerToClient(..), ClientToServer(..)
    , ConnectedHandshake, HttpEndpoint(..)
    , CliFlag(..), EnvVar(..)
    , allHttpEndpoints
    )

{-| WebSocket and HTTP protocol between Go server and browser.

Source: main.go `handleWebSocket` and `startHTTPServer` functions.

Connection: browser connects to /ws?cursor={lastSeq}
Server streams missed events (seq > cursor) then fans out live events.

@docs ServerToClient, ClientToServer
@docs ConnectedHandshake, HttpEndpoint
@docs CliFlag, EnvVar
@docs allHttpEndpoints

-}

import Domain exposing (AckId(..), Event(..), FileRef, QuickReplies, Seq(..), Version(..))



-- -- Server to Client ---------------------------------------------


{-| Messages sent from server to browser over WebSocket.

Source: main.go `handleWebSocket` function.

-}
type ServerToClient
    = Connected ConnectedHandshake
      {- Sent immediately after WebSocket upgrade.
         Browser uses this to restore state on reconnect.
      -}
    | StreamedEvent Event
      {- Events streamed after connected handshake.
         First: missed events (seq > cursor) from event log.
         Then: live events from event bus subscription.
         Duplicates (seq <= highSeq) are skipped server-side.
      -}
    | MessageQueued



{- Sent only to the browser that submitted a message.
   Confirms the message was queued in the event bus.
   Browser uses this to notify parent frame (for check_messages).

   Wire format: { "type": "messageQueued" }
-}


{-| The connected handshake sent on WebSocket open.

Source: main.go `handleWebSocket` function.

    { "type": "connected",
      "version": "0.1.6 (bcaedff)",
      "pendingAckId": "uuid",    -- optional
      "quickReplies": ["Yes", "No"]  -- optional
    }

-}
type alias ConnectedHandshake =
    { version : Version
    , pendingAckId : Maybe AckId -- non-empty if a blocking tool is waiting for user input
    , quickReplies : QuickReplies -- last sent quick_replies (empty if agent is working)
    }



-- -- Client to Server ---------------------------------------------


{-| Messages sent from browser to server over WebSocket.

Source: main.go `handleWebSocket` read loop.

The Go read struct is:

    struct {
        Type    string    `json:"type"`
        Text    string    `json:"text"`
        Files   []FileRef `json:"files"`
        ID      string    `json:"id"`
        Message string    `json:"message"`
    }

-}
type ClientToServer
    = Message { text : String, files : List FileRef }
      {- User typed a message and hit send (or spoke via voice).
         Server pushes to message queue, publishes userMessage event
         to all browsers, and sends MessageQueued back to sender.

         Wire: { "type": "message", "text": "...", "files": [...] }
      -}
    | AckReply { id : AckId, message : String }



{- User clicked a quick-reply button or typed into ack input.
   Server resolves the pending ack (unblocking the MCP tool),
   then publishes userMessage event with the reply text.

   If message is empty, ack result is "ack".
   If message is non-empty, ack result is "ack:{message}".

   Note: the wire field for ack ID is "id" (not "ack_id" as in Event).

   Wire: { "type": "ack", "id": "uuid", "message": "..." }
-}
-- -- HTTP endpoints -----------------------------------------------


{-| HTTP endpoints served by the Go server.

Source: main.go `startHTTPServer` function.

-}
type HttpEndpoint
    = WebSocket
      {- GET /ws?cursor={seq}
         Upgrades to WebSocket. Cursor param enables reconnect replay.
      -}
    | McpStreamableHttp
      {- POST /mcp
         StreamableHTTP MCP endpoint (stateless mode).
         Same tools/resources as stdio MCP transport.
      -}
    | Upload
      {- POST /upload
         Multipart file upload. Max 50MB. Form field name: "files".
         Returns JSON array of FileRef.
      -}
    | UploadedFiles
      {- GET /uploads/{filename}
         Serves uploaded files from the upload directory.
      -}
    | ConfigJs
      {- GET /config.js
         Dynamic JavaScript with server-side config:
           var THEME_COOKIE_NAME = "agent-chat-theme";
           var SERVER_VERSION = "0.1.6 (bcaedff)";
      -}
    | StaticAssets



{- GET /
   Serves embedded client-dist/ files (index.html, app.js, style.css, etc.).
-}


{-| All HTTP endpoints.

    allHttpEndpoints == [ WebSocket, McpStreamableHttp, Upload, UploadedFiles, ConfigJs, StaticAssets ]

-}
allHttpEndpoints : List HttpEndpoint
allHttpEndpoints =
    [ WebSocket, McpStreamableHttp, Upload, UploadedFiles, ConfigJs, StaticAssets ]



-- -- CLI flags and environment variables --------------------------


{-| Command-line flags accepted by the server.

Source: main.go `main` function, flag.Parse().

-}
type CliFlag
    = VersionFlag {- -v: print version and exit. -}
    | NoStdioMcp
      {- -no-stdio-mcp: disable stdio MCP transport.
         HTTP MCP (POST /mcp) is always available.
      -}
    | ThemeCookie String
      {- -theme-cookie: cookie name for light/dark theme toggle.
         Default: "agent-chat-theme".
      -}
    | UploadDirFlag String



{- -upload-dir: directory for uploaded files.
   Default: temp directory (os.MkdirTemp).
-}


{-| Environment variables read by the server.

Source: main.go `main` and `startHTTPServer` functions.

-}
type EnvVar
    = AgentChatPort
      {- AGENT_CHAT_PORT: TCP port to listen on.
         Falls back to PORT if AGENT_CHAT_PORT is not set.
         Default: 0 (OS-assigned random port).
      -}
    | AgentChatDisable
      {- AGENT_CHAT_DISABLE: when non-empty, disables tool and
         resource registration. Server starts but does nothing.
      -}
    | AgentChatEventLog



{- AGENT_CHAT_EVENT_LOG: path for JSONL event log file.
   Enables persistence across server restarts.
   Events are loaded on startup and appended during operation.
-}
