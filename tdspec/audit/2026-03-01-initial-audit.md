# Tdspec Audit: 2026-03-01

Comparison of tdspec Elm modules against Go source code in the agent-chat project.

Go source files audited:
- `eventbus.go`
- `tools.go`
- `main.go`
- `resources.go`
- `prompts.go` (discovered during audit)

---

## Module: Domain.elm

Spec file: `tdspec/src/Domain.elm`
Go source: `eventbus.go` (types), `main.go` (protocol)

### FileRef (Domain.elm:38-44 vs eventbus.go:17-23)

- [x] `name : String` -- matches Go `Name string json:"name"` (line 18)
- [x] `path : String` -- matches Go `Path string json:"path"` (line 19)
- [x] `url : String` -- matches Go `URL string json:"url"` (line 20)
- [x] `size : Int` -- matches Go `Size int64 json:"size"` (line 21)
- **DISCREPANCY: Field name mismatch.** Spec calls the field `mimeType` (Domain.elm:43) but Go struct field is `Type` with JSON tag `"type,omitempty"` (eventbus.go:22). The Elm field should be named `type_` or the comment should note the JSON wire name is `"type"`, not `"mimeType"`.
- **DISCREPANCY: Optionality mismatch.** Go field `Type` has `json:"type,omitempty"` meaning it can be absent on the wire (eventbus.go:22). The Elm spec declares `mimeType : String` as non-optional (Domain.elm:43). Should be `Maybe String` to reflect the Go `omitempty` behavior.

### UserMessage (Domain.elm:52-55 vs eventbus.go:26-29)

- [x] `text : String` -- matches Go `Text string json:"text"` (line 27)
- [x] `files : List FileRef` -- matches Go `Files []FileRef json:"files,omitempty"` (line 28)
- Note: Go has `omitempty` on Files but the Elm spec models it as `List FileRef` (empty list). This is acceptable -- an absent JSON array and an empty array are semantically equivalent here.

### Seq, AckId, Timestamp, Version (Domain.elm:61-82)

- [x] `Seq Int` -- matches Go `Seq int64` on Event (eventbus.go:34)
- [x] `AckId String` -- matches Go `AckID string` on Event (eventbus.go:36) and AckHandle (eventbus.go:45)
- [x] `Timestamp Int` -- matches Go `Timestamp int64` on Event (eventbus.go:40)
- [x] `Version String` -- matches Go `version` and `commit` vars (main.go:35-36), composed as `version + " (" + commit + ")"` (main.go:314)

### Event ADT (Domain.elm:110-131 vs eventbus.go:32-41)

- [x] `AgentMessage ChatMessageData` -- corresponds to Go `Type: "agentMessage"` events
- [x] `VerbalReply ChatMessageData` -- corresponds to Go `Type: "verbalReply"` events
- [x] `UserMessageEvent UserMessageData` -- corresponds to Go `Type: "userMessage"` events
- [x] `DrawEvent DrawEventData` -- corresponds to Go `Type: "draw"` events
- **DISCREPANCY: Missing event type.** The Go Event struct `Type` field (eventbus.go:33) can carry any string. In practice, the WebSocket handler on line main.go:406 creates events with `Type: "userMessage"` that include a `Text` and `Files` but no `Seq` (it gets assigned during Publish). The spec correctly models this. However, `LogUserMessage` (eventbus.go:332-338) creates events that are appended to the event log **without** a `Seq` number (they are not published through `Publish`). The spec does not account for this -- these are events in the log with `Seq == 0`.

### ChatMessageData (Domain.elm:136-143)

- [x] `seq : Seq` -- matches Event.Seq
- [x] `timestamp : Timestamp` -- matches Event.Timestamp
- [x] `text : String` -- matches Event.Text
- [x] `ackId : Maybe AckId` -- matches Event.AckID (omitempty)
- [x] `quickReplies : QuickReplies` -- matches Event.QuickReplies (omitempty, empty list when absent)
- [x] `files : List FileRef` -- matches Event.Files (omitempty)

### UserMessageData (Domain.elm:148-153)

- [x] `seq : Seq` -- matches
- [x] `timestamp : Timestamp` -- matches
- [x] `text : String` -- matches
- [x] `files : List FileRef` -- matches

### DrawEventData (Domain.elm:158-165)

- [x] `seq : Seq` -- matches
- [x] `timestamp : Timestamp` -- matches
- [x] `text : String` -- matches
- **DISCREPANCY: DrawEvent has `text` in spec but Go draw events may not have text.** Looking at tools.go:289, the draw event is published as `Event{Type: "agentMessage", Text: params.Text}` for the text bubble, but the actual draw event at tools.go:294-297 and 311-316 does NOT include a `Text` field. The `text` field on DrawEventData is misleading -- the text goes in a separate agentMessage event, not on the draw event itself.
- [x] `instructions : List Json` -- matches Event.Instructions (omitempty)
- [x] `ackId : Maybe AckId` -- matches Event.AckID
- [x] `quickReplies : QuickReplies` -- matches Event.QuickReplies

### QuickReplies (Domain.elm:88-89)

- [x] `List String` -- matches Go `[]string` in Event.QuickReplies (eventbus.go:37)

### Json (Domain.elm:170-171)

- [x] Opaque type -- matches Go `[]any` for Instructions (eventbus.go:38)

### Types in Go NOT in Domain.elm

- **MISSING: EventBus struct** (eventbus.go:51-66). Not modeled in Domain.elm. It is modeled in EventBus.elm (appropriate).
- **MISSING: `lastVoice` field on EventBus** (eventbus.go:62). The voice mode tracking (`SetLastVoice`, `LastVoice`) is not mentioned anywhere in the spec. This is important behavior: it determines whether `send_message` rejects with `VoiceModeError`.
- **MISSING: formatMessagesData, messageData, fileData, replyInstructionsData** (prompts.go:16-35). These are internal template data types. Arguably internal implementation detail, but `FormatMessages` and `voiceSuffix` are referenced in tool results. Not critical to spec.

---

## Module: EventBus.elm

Spec file: `tdspec/src/EventBus.elm`
Go source: `eventbus.go`

### AckHandle (EventBus.elm:47-49 vs eventbus.go:44-47)

- [x] `id : AckId` -- matches Go `ID string` (eventbus.go:45)
- [x] `channel : AckResult` -- matches Go `Ch chan string` (eventbus.go:46)
- Note: Spec models the channel as producing `AckResult`, Go chan produces raw strings ("ack" or "ack:message"). The spec correctly abstracts this.

### AckResult (EventBus.elm:59-61)

- [x] `Ack` -- corresponds to Go `"ack"` string result
- [x] `AckWithMessage String` -- corresponds to Go `"ack:{message}"` string result
- Note: The parsing logic is in main.go:416-419, not in eventbus.go. Spec correctly abstracts this.

### publish (EventBus.elm:77-83 vs eventbus.go:304-329)

- [x] Signature and behavior description match.
- [x] Correctly notes: sets timestamp if zero, assigns monotonic seq.
- [x] Correctly notes: tracks lastQuickReplies.

### createAck (EventBus.elm:92-96 vs eventbus.go:386-395)

- [x] Signature matches. Go returns `AckHandle`, spec returns `AckHandle`.

### resolveAck (EventBus.elm:105-111 vs eventbus.go:399-415)

- **DISCREPANCY: Signature mismatch.** Spec takes `{ id : AckId, result : AckResult }` (EventBus.elm:105), but Go `ResolveAck` takes `(id string, result string)` (eventbus.go:399). The Go function takes a raw result string, not a structured `AckResult`. The spec models the input as already-parsed, but the Go function receives raw wire format ("ack" or "ack:message").

### subscribe (EventBus.elm:125-127 vs eventbus.go:260-266)

- **DISCREPANCY: Return type mismatch.** Spec returns `()` (EventBus.elm:125), but Go returns `chan Event` (eventbus.go:260). The subscriber channel is the key return value -- without it you cannot receive events. The spec should model this.

### waitForSubscriber (EventBus.elm:139-141 vs eventbus.go:270-287)

- [x] Signature conceptually matches. Go takes `context.Context` and returns `error`; spec returns `Result String ()`.
- [x] Timeout/polling behavior accurately described.

### pushMessage (EventBus.elm:154-159 vs eventbus.go:161-173)

- [x] Signature matches conceptually.
- [x] Buffer overflow behavior (drop oldest) correctly described.

### drainMessages (EventBus.elm:171-173 vs eventbus.go:177-187)

- [x] Matches.

### waitForMessages (EventBus.elm:183-185 vs eventbus.go:191-208)

- [x] Matches conceptually. Go takes `context.Context`, spec returns `Result String`.

### eventsSince (EventBus.elm:198-204 vs eventbus.go:341-362)

- [x] Matches. Spec takes `Seq`, Go takes `int64`.

### pendingAckId (EventBus.elm:214-216 vs eventbus.go:365-372)

- [x] Matches.

### lastQuickReplies (EventBus.elm:226-228 vs eventbus.go:226-230)

- [x] Matches.

### Methods in Go NOT in EventBus.elm

- **MISSING: `Unsubscribe(ch chan Event)`** (eventbus.go:290-294). Not in spec. This is the counterpart to `Subscribe` and is called in the WebSocket handler (main.go:325).
- **MISSING: `ResetLog()`** (eventbus.go:297-301). Not in spec. Clears the in-memory event log.
- **MISSING: `LogUserMessage(text string, files []FileRef)`** (eventbus.go:332-338). Not in spec. Appends a userMessage event to the log without publishing (no seq assigned, no fan-out). Used differently from `Publish`.
- **MISSING: `History() ([]Event, string)`** (eventbus.go:375-382). Not in spec. Returns full event log copy and pending ack ID. Not currently used in the main code paths but is a public method.
- **MISSING: `HasQueuedMessages() bool`** (eventbus.go:233-235). Not in spec. Used by blocking tools to check if user has already sent messages before publishing with quick_replies.
- **MISSING: `SetLastVoice(voice bool)`** (eventbus.go:211-215). Not in spec. Tracks voice mode state.
- **MISSING: `LastVoice() bool`** (eventbus.go:218-222). Not in spec. Returns voice mode state. Used by `send_message` to reject calls when user is in voice mode.
- **MISSING: `Close()`** (eventbus.go:150-158). Not in spec. Flushes and closes the JSONL log file.
- **MISSING: `writeToLog(event Event)`** (eventbus.go:134-147). Private method, arguably internal detail.
- **MISSING: `NewEventBus()` and `NewEventBusWithLog(path string)`** (eventbus.go:69-97). Constructors not in spec. Relevant for understanding initialization.
- **MISSING: `FormatMessages(msgs []UserMessage) string`** (eventbus.go:238-256). Free function, not a method on EventBus, but defined in eventbus.go. Used by tools to format user messages for agent responses.

### Functions in Spec NOT in Go

- None. All spec functions correspond to real Go methods.

---

## Module: McpTools.elm

Spec file: `tdspec/src/McpTools.elm`
Go source: `tools.go`, `resources.go`

### McpTool ADT (McpTools.elm:43-80)

- [x] `SendMessage MessageParams` -- matches `send_message` tool (tools.go:101-175)
- [x] `SendVerbalReply VerbalReplyParams` -- matches `send_verbal_reply` tool (tools.go:177-238)
- [x] `Draw DrawParams` -- matches `draw` tool (tools.go:248-340)
- [x] `SendProgress ProgressParams` -- matches `send_progress` tool (tools.go:348-364)
- [x] `SendVerbalProgress ProgressParams` -- matches `send_verbal_progress` tool (tools.go:372-388)
- [x] `CheckMessages` -- matches `check_messages` tool (tools.go:392-409)

### MessageParams (McpTools.elm:88-93 vs tools.go:32-37)

- [x] `text : String` -- matches `Text string json:"text"` (tools.go:33)
- [x] `quickReply : String` -- matches `QuickReply string json:"quick_reply"` (tools.go:34)
- [x] `moreQuickReplies : List String` -- matches `MoreQuickReplies []string json:"more_quick_replies,omitempty"` (tools.go:35)
- [x] `imageUrls : List String` -- matches `ImageURLs []string json:"image_urls,omitempty"` (tools.go:36)

### VerbalReplyParams (McpTools.elm:102-107 vs tools.go:40-45)

- [x] All fields match (same structure as MessageParams).

### DrawParams (McpTools.elm:115-120 vs tools.go:241-246)

- [x] `text : String` -- matches `Text string json:"text"` (tools.go:242)
- [x] `instructions : List Json` -- matches `Instructions []any json:"instructions"` (tools.go:243)
- [x] `quickReply : String` -- matches `QuickReply string json:"quick_reply"` (tools.go:244)
- [x] `moreQuickReplies : List String` -- matches `MoreQuickReplies []string json:"more_quick_replies,omitempty"` (tools.go:245)
- **DISCREPANCY: Missing field.** Go `DrawParams` does NOT have `imageUrls` (tools.go:241-246), and the spec correctly omits it. However, the Draw tool description in McpTools.elm does not mention that draw does NOT support image_urls, while SendMessage and SendVerbalReply do. This is a minor documentation gap.

### ProgressParams (McpTools.elm:128-131 vs tools.go:343-346)

- [x] `text : String` -- matches `Text string json:"text"` (tools.go:344)
- [x] `imageUrls : List String` -- matches `ImageURLs []string json:"image_urls,omitempty"` (tools.go:345)

### VerbalProgressParams

- **DISCREPANCY: Spec uses shared `ProgressParams` type** (McpTools.elm:128) for both `send_progress` and `send_verbal_progress`. Go defines a separate `VerbalProgressParams` struct (tools.go:367-369) that is structurally identical to `ProgressParams`. The spec notes this ("Go: VerbalProgressParams is identical to ProgressParams" at McpTools.elm:70), which is acceptable, but the Go source does have a distinct type.

### ToolResult ADT (McpTools.elm:139-148)

- [x] `UserResponded { formatted, messages }` -- matches behavior in tools.go:146,165 ("User responded: ...") and tools.go:402 ("User said: ...")
- **DISCREPANCY: `UserResponded` is used for both blocking tool responses and `check_messages` responses, but the Go code uses different prefix text.** Blocking tools return `"User responded: "` (tools.go:146,165) while `check_messages` returns `"User said: "` (tools.go:402). The spec only documents the "User responded:" format (McpTools.elm:141). Should note both formats or have separate variants.
- **DISCREPANCY: Missing `uiURL` in result text.** Go blocking tools append `"\nChat UI: " + uiURL` to the result when `uiURL != ""` (tools.go:147-149, 166-168, 229-231, 299-301, 331-333). The spec `formatted` field description (McpTools.elm:141) does not mention this suffix.
- [x] `DrawAcknowledged` -- matches "Viewer acknowledged." (tools.go:325)
- [x] `DrawFeedback String` -- matches "Viewer responded: " + msg (tools.go:328)
- **DISCREPANCY: Missing draw result variant.** When the user has queued messages during a draw, Go returns `"Draw displayed. User has pending messages -- call check_messages."` (tools.go:298). This is not captured by any `ToolResult` variant.
- [x] `ProgressSent` -- matches "Progress sent." (tools.go:361) and "Verbal progress sent." (tools.go:386)
- [x] `NoNewMessages` -- matches "No new messages." (tools.go:399)
- [x] `VoiceModeError` -- matches "ERROR: The user is in voice mode..." (tools.go:109)

### McpResource (McpTools.elm:161-176)

- [x] `WhiteboardInstructions` -- matches `whiteboard://instructions` (resources.go:21)
- [x] `WhiteboardDiagrammingGuide` -- matches `whiteboard://diagramming-guide` (resources.go:38)
- [x] `WhiteboardQuickReference` -- matches `whiteboard://quick-reference` (resources.go:54)

### allResources (McpTools.elm:184-186)

- [x] Matches the three resources registered in registerResources (resources.go:19-70).

### Items in Go NOT in McpTools.elm

- **MISSING: `resolveImageFiles` function** (tools.go:48-98). Helper that copies local image files to upload directory and returns FileRefs. Important implementation detail that explains how `image_urls` parameters work.
- **MISSING: `isVoiceMessage` function** (tools.go:17-24). Determines if a message is voice by checking for microphone emoji prefix.
- **MISSING: `voiceSuffix` function** (tools.go:27-29). Returns reply instruction text appended to tool results.
- **MISSING: `EmptyParams` struct** (tools.go:390). The spec notes CheckMessages takes no parameters (McpTools.elm:79) but does not explicitly model the empty params type.
- **MISSING: `ensureHTTPServer` lazy startup behavior** (main.go:64-82). Spec mentions it in SendMessage comments (McpTools.elm:47) but does not note crash-recovery restart behavior.

---

## Module: WebSocketProtocol.elm

Spec file: `tdspec/src/WebSocketProtocol.elm`
Go source: `main.go`

### ServerToClient (WebSocketProtocol.elm:32-52)

- [x] `Connected ConnectedHandshake` -- matches Go connected message (main.go:314-321)
- [x] `StreamedEvent Event` -- matches Go event streaming (main.go:328-337 for missed, main.go:354-368 for live)
- [x] `MessageQueued` -- matches Go `{"type": "messageQueued"}` (main.go:410)
- [x] Duplicate skipping behavior documented (WebSocketProtocol.elm:41) matches Go `event.Seq <= highSeq` check (main.go:359)

### ConnectedHandshake (WebSocketProtocol.elm:66-70 vs main.go:314-321)

- [x] `version : Version` -- matches `"version": version + " (" + commit + ")"` (main.go:314)
- [x] `pendingAckId : Maybe AckId` -- matches conditional inclusion (main.go:315-317)
- [x] `quickReplies : QuickReplies` -- matches conditional inclusion (main.go:318-320)
- **DISCREPANCY: Line number reference outdated.** Spec cites "main.go lines 313-321" (WebSocketProtocol.elm:57) but the actual code is at main.go:314-321. Minor, but line references can drift.

### ClientToServer (WebSocketProtocol.elm:82-101)

- [x] `Message { text : String, files : List FileRef }` -- matches Go `case "message"` (main.go:401-413)
- [x] `AckReply { id : AckId, message : String }` -- matches Go `case "ack"` (main.go:414-423)
- **DISCREPANCY: Wire format field name.** The Go WebSocket read struct uses `ID string json:"id"` (main.go:394) but the spec says `id : AckId` (WebSocketProtocol.elm:90). The AckId wrapper is fine, but the JSON tag is `"id"`, not `"ack_id"` as might be expected from the Event struct's `AckID` field. The spec correctly models this.
- **DISCREPANCY: Ack behavior detail.** The spec says the ack result is `"ack"` or `"ack:{message}"` (WebSocketProtocol.elm:98-99). The Go code does this at main.go:416-419, but it also **publishes a userMessage event** with the ack reply text (main.go:422). The spec mentions this in the comment (WebSocketProtocol.elm:94-95) but only briefly. Worth noting: when `m.Message` is empty, the published userMessage has empty Text.

### HttpEndpoint (WebSocketProtocol.elm:111-143)

- [x] `WebSocket` -- matches `/ws` handler (main.go:184)
- [x] `McpStreamableHttp` -- matches `/mcp` handler (main.go:183)
- [x] `Upload` -- matches `/upload` handler (main.go:185)
- [x] `UploadedFiles` -- matches `/uploads/` handler (main.go:186)
- [x] `ConfigJs` -- matches `/config.js` handler (main.go:187-191)
- [x] `StaticAssets` -- matches `/` handler (main.go:192)
- **DISCREPANCY: Line number reference slightly off.** Spec says "lines 182-192" (WebSocketProtocol.elm:108) which is close but the mux setup starts at line 182 (mux creation) through line 192.
- **DISCREPANCY: Missing `THEME_COOKIE_NAME` detail.** Spec correctly shows `THEME_COOKIE_NAME` in config.js (WebSocketProtocol.elm:133) but does not mention the `--theme-cookie` CLI flag (main.go:87) or the `themeCookieName` variable (main.go:39).

### Items in Go NOT in WebSocketProtocol.elm

- **MISSING: Upload size limit.** The spec says "Max 50MB" (WebSocketProtocol.elm:123), which matches Go `50<<20` (main.go:241). This is correct.
- **MISSING: Port configuration.** Go supports `AGENT_CHAT_PORT` and `PORT` env vars (main.go:195-199) and `0.0.0.0:0` default binding. Not documented in spec.
- **MISSING: CLI flags.** Go has `-v` (version), `-no-stdio-mcp`, `-theme-cookie`, `-upload-dir` flags (main.go:85-88). Not documented in spec.
- **MISSING: `AGENT_CHAT_DISABLE` env var** (main.go:138). Disables tool and resource registration.
- **MISSING: `AGENT_CHAT_EVENT_LOG` env var** (main.go:114). Enables JSONL event log persistence.
- **MISSING: `openBrowser` function** (main.go:221-232). Platform-specific browser opening.
- **MISSING: `saveUploadedFile` function** (main.go:267-295). Upload file saving implementation.
- **MISSING: `handleUpload` function details** (main.go:234-265). The spec mentions the endpoint but not the implementation details (multipart form field name is `"files"`, returns JSON array of FileRef).
- **MISSING: WebSocket read loop struct.** The Go anonymous struct at main.go:390-396 deserializes incoming messages. It has `Type`, `Text`, `Files`, `ID`, and `Message` fields. The spec models the client-to-server messages as an ADT but does not show the wire struct.

---

## Cross-cutting Issues

### 1. Voice mode tracking not in spec

The `lastVoice` field (eventbus.go:62), `SetLastVoice` (eventbus.go:211), and `LastVoice` (eventbus.go:218) methods are not documented in any spec module. This is core behavioral logic: it causes `send_message` to reject with `VoiceModeError`. The EventBus.elm module should document this state, and McpTools.elm should reference it.

### 2. LogUserMessage vs Publish distinction

`LogUserMessage` (eventbus.go:332-338) appends events to the log WITHOUT assigning a sequence number or fanning out to subscribers. This creates events with `Seq == 0` in the event log. The spec does not distinguish between published events (with seq) and logged-only events (without seq). This matters for reconnect replay -- browsers receiving these events would see `seq: 0`.

**Update:** On closer inspection, `LogUserMessage` is defined but never called from main.go. The WebSocket handler uses `bus.Publish(Event{Type: "userMessage", ...})` (main.go:406) instead. `LogUserMessage` may be dead code or legacy. Still, it is a public method that should either be spec'd or noted as unused.

### 3. HasQueuedMessages not in spec

`HasQueuedMessages` (eventbus.go:233-235) is used by all three blocking tools (send_message, send_verbal_reply, draw) to short-circuit when user has already sent messages. This is important behavioral logic described in the McpTools.elm comments but the underlying EventBus method is not in EventBus.elm.

### 4. FormatMessages not in spec

`FormatMessages` (eventbus.go:238-256) is a free function (not a method) in eventbus.go that formats user messages for tool results. It is referenced in the ToolResult description but not modeled anywhere.

### 5. History method not in spec

`History() ([]Event, string)` (eventbus.go:375-382) returns the full event log and pending ack ID. This is a public method not documented in EventBus.elm.

### 6. Spec has no module for prompts/templates

The `prompts.go` file and its template types (`formatMessagesData`, `messageData`, `fileData`, `replyInstructionsData`) are not covered. The `execTemplate` and `formatSize` functions are also absent. These are internal but affect the tool result text format.

---

## Summary of Discrepancies

| # | Module | Severity | Description |
|---|--------|----------|-------------|
| 1 | Domain.elm | Medium | FileRef field `mimeType` should be `type_` (or annotated) -- Go JSON tag is `"type"` not `"mimeType"` |
| 2 | Domain.elm | Low | FileRef `mimeType`/`type` should be `Maybe String` (Go uses `omitempty`) |
| 3 | Domain.elm | Low | DrawEventData.text is misleading -- Go draw events do not carry text (text goes in separate agentMessage) |
| 4 | EventBus.elm | Medium | `subscribe` return type is `()` but Go returns `chan Event` |
| 5 | EventBus.elm | Low | `resolveAck` takes `AckResult` but Go takes raw string |
| 6 | EventBus.elm | Medium | Missing 8 public methods: `Unsubscribe`, `ResetLog`, `LogUserMessage`, `History`, `HasQueuedMessages`, `SetLastVoice`, `LastVoice`, `Close` |
| 7 | EventBus.elm | Medium | Missing `FormatMessages` free function |
| 8 | McpTools.elm | Low | `UserResponded` does not distinguish "User responded:" (blocking) vs "User said:" (check_messages) |
| 9 | McpTools.elm | Low | Missing `uiURL` suffix in result text format |
| 10 | McpTools.elm | Medium | Missing draw result variant for "User has pending messages" case |
| 11 | McpTools.elm | Low | `VerbalProgressParams` is a separate Go type (structurally identical to `ProgressParams`) |
| 12 | WebSocketProtocol.elm | Low | Line number references may drift |
| 13 | WebSocketProtocol.elm | Low | Missing documentation of CLI flags, env vars (`AGENT_CHAT_PORT`, `AGENT_CHAT_DISABLE`, `AGENT_CHAT_EVENT_LOG`) |
| 14 | Cross-cutting | Medium | Voice mode tracking (`lastVoice`, `SetLastVoice`, `LastVoice`) not documented in any spec module |
| 15 | Cross-cutting | Low | `LogUserMessage` is a public method but may be dead code -- not called from main.go |
| 16 | Cross-cutting | Low | No spec module for prompts.go template types and functions |
