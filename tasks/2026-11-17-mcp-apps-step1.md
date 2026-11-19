# MCP Apps, step 1: show tool pages in agent-chat via `agent-chat wrap`

Filed 2026-11-17. Status: **PLANNED**. Branch: `mcp-apps`.
Repo: agent-chat (this worktree: `/repos/agent-chat/worktrees/mcp-apps`).

## Goal

When the agent calls an MCP tool that comes with an interactive page
(MCP Apps, SEP-1865, spec `2026-01-26`), that page appears inside the chat, filled in
with the tool's answer. Clicking a button in the page sends a chat message
to the agent. The agent still decides what to do and calls the tools itself.

## Why this shape (decided in chat 2026-11-16)

- Claude Code in the terminal does NOT tell tool servers "I can show pages"
  (measured: Claude Code 2.1.325 `initialize` capabilities = `roots` +
  `elicitation` only; `testdata/mcp-apps-sample --ui=auto` logged
  `ui=false`). No setting exists; feature request anthropics/claude-code#95149
  has had no reply and is marked stale. Official client list:
  modelcontextprotocol.io/extensions/client-matrix (Claude Code: not supported).
- Spec says servers SHOULD hide pages from such clients. The official
  ext-apps SDK doesn't do this by default (its example servers attach
  `_meta.ui` for everyone), but some servers do.
- Existing MCP proxies/gateways (about 10 surveyed) open their own connection
  to the server or merge servers, and lose client capabilities and
  server-to-client requests (e.g. aws/mcp-proxy-for-aws#450, fixed
  2026-11-02). swe-swe's `mcp-cli-proxy` drops server-initiated messages too
  (`routeResponse`), and can't be used in front of Claude Code.
- So: a **dumb pass-through wrapper**, `agent-chat wrap -- <server command>`.
  Claude Code starts it in place of the server, and it passes every message
  through unchanged except for two edits. Design reference: mcpsnoop
  (github.com/kerlenton/mcpsnoop). It ships inside the agent-chat binary, so
  it updates as fast as agent-chat does.

## Compatibility rule (core, see memory `agent-chat-swe-swe-version-compat`)

- agent-chat works with any released swe-swe. If nothing is wrapped, no pages
  appear and nothing else changes.
- The wrapper must NEVER break a tool. If agent-chat isn't reachable, if a
  page fetch fails, or if a message can't be parsed, it forwards the bytes as
  they are and logs to stderr.
- swe-swe may later wrap the servers it configures, but only after checking
  that the installed agent-chat has `wrap` (a separate swe-swe task, not this
  one).

## Pieces

```
Claude Code ──stdio──> agent-chat wrap ──stdio──> tool server
                          │  (1) adds "I can show pages" to initialize / per-request _meta
                          │  (2) on tools/call result for a tool with _meta.ui.resourceUri:
                          │      resources/read with its own ids ("acw-N"), then
                          └──HTTP POST──> agent-chat server (AGENT_CHAT_PORT) /api/mcp-app
                                              └─ event "mcpApp" ─> chat UI renders sandboxed iframe
```

## Phases

### Phase 1: `agent-chat wrap` pass-through, plus the capability edit (about 1 day)

- New file `wrap.go`. Dispatch it in `main()` next to `migrate-chatlogs`
  (`main.go` ~line 265): `agent-chat wrap [--no-advertise-ui] -- <cmd> [args...]`.
- Start the child with the same env, stdin, stdout and stderr. Relay
  newline-delimited JSON-RPC in both directions, one line at a time, with a
  16 MB line cap (matches mcp-cli-proxy).
- Edit only these, and re-encode only the lines you edit:
  1. Client→server `initialize`: add
     `params.capabilities.extensions["io.modelcontextprotocol/ui"] = {"mimeTypes":["text/html;profile=mcp-app"]}`,
     keeping any existing extensions.
  2. Client→server requests carrying
     `params._meta["io.modelcontextprotocol/clientCapabilities"]` (2026-07-28
     protocol, also `server/discover`): add the same extension there.
- When the child exits, the wrapper exits with the child's exit code. When
  the wrapper's stdin closes, close the child's stdin.
- Tests (`wrap_test.go`, using a fake child written in Go and run with the
  `os.Args` re-exec trick):
  - Lines that aren't edited are forwarded byte-for-byte in both directions.
  - `initialize` gets the extension, and an existing `extensions.io.modelcontextprotocol/tasks` is kept.
  - Per-request `_meta` capabilities get the extension.
  - Server→client requests (`elicitation/create`, `roots/list`) and the
    client's responses pass through untouched. So do notifications
    (progress, list_changed) and cancellation in both directions.
  - A malformed line passes through unchanged.
  - Exit codes propagate.
- **Done when** `make test` is green and
  `go run . wrap -- go run ./testdata/mcp-apps-sample --ui=auto` logs
  `ui=true` when it gets a plain initialize.

### Phase 2: wrapper notices pages and sends them to agent-chat (about 1 day)

- Remember the response to `tools/list` from server to client: for each tool
  with `_meta.ui.resourceUri` (or the deprecated `_meta["ui/resourceUri"]`),
  store name → uri. Re-learn it on `notifications/tools/list_changed`
  (the next `tools/list` refreshes it).
- Track pending client `tools/call` ids → (tool name, arguments).
- When the response for a page tool arrives: forward it to the client FIRST,
  with no delay. Then, off the main path:
  1. Send `resources/read {uri}` to the child using id `"acw-<n>"` (a string,
     so it can't collide with the client's numbers). Swallow its response, so
     it never reaches the client. Cache the page per uri for the life of the
     process.
  2. POST to `http://127.0.0.1:$AGENT_CHAT_PORT/api/mcp-app`:
     `{server, tool, arguments, result, resourceUri, html, resourceMeta}`.
     `server` is taken from `--name` if given, else from the command's base name.
- Send our own requests only after the client's `notifications/initialized`
  has passed through.
- If `AGENT_CHAT_PORT` is unset, or the POST fails or times out (2s): log
  one stderr line, carry on.
- Tests: page fetched once per uri, response order kept, `acw-` responses
  never reach the client, failed POST doesn't block or change the tool reply.
  Use an `httptest` server for agent-chat and the sample server as the child.

### Phase 3: agent-chat server receives pages (about half a day)

- `mux.HandleFunc("/api/mcp-app", handleMcpApp)` in `main.go` (~line 473).
  Loopback only. 4 MB body cap. Reject unless the `mimeType` is
  `text/html;profile=mcp-app`.
- Publish a new bus event `mcpApp` (`eventbus.go`) carrying an id, server,
  tool, arguments, result and resourceMeta. Store the html once under the
  upload dir and reference it by URL, so the history and the websocket don't
  carry 100s of KB on every replay.
- Chat-log export: a text line "📱 <server>/<tool> page shown". Don't embed
  the page.
- Open question to settle in this phase: any local process could POST here.
  The existing `/upload` is equally open. Decide whether to require a
  per-session token (e.g. an env var both processes inherit). Record the
  decision here.

### Phase 4: chat UI shows the page and acts as its host (about 1–1.5 days)

- `client-dist/app.js`: render `mcpApp` as an agent-side bubble that holds an
  `<iframe sandbox="allow-scripts">`. Do NOT add `allow-same-origin`, so the
  page can't read agent-chat's cookies or storage. Content goes in via
  `srcdoc`, with a CSP `<meta>` added in front: built from
  `resourceMeta.ui.csp`, otherwise the spec's restrictive default.
  Honour `prefersBorder`.
- The host side of the protocol (`postMessage` JSON-RPC, check `ev.source`):
  - `ui/initialize` → reply with `protocolVersion "2026-01-26"`,
    `hostInfo {name:"agent-chat"}`, `hostCapabilities` (no tool calls yet),
    `hostContext {theme}`.
  - After `ui/notifications/initialized`: send `ui/notifications/tool-input`,
    then `ui/notifications/tool-result`.
  - `ui/notifications/size-changed` → set the iframe height (with a cap).
  - `ui/message {role:"user", content:{type:"text"}}` → post as a user chat
    message marked "from <tool> page", using the same path as typing it.
  - `tools/call` → error `-32000 "agent-chat can't call tools yet; use ui/message"`.
    The sample already falls back on this.
  - `ui/open-link` → open in a new tab, but only `http(s)`.
  - Anything else → `-32601`.
- On reload or history replay, re-render from the stored event, so the page
  gets the same tool-input and tool-result again.
- Rebuild the binary after client-dist edits (see CLAUDE.md / memory).

### Phase 5: tests and live check (about 1 day)

- E2E `e2e/mcp-app.spec.cjs` (binds `$PORT` via `serverPort()`, uses
  `gotoRetry`): POST the sample page and result to `/api/mcp-app` → the
  iframe appears with the grid; clicking a free cell → a user message
  "Please book … (book_slot)" appears; reload → the page is still there.
- Go test: wrap + sample server + httptest agent-chat, end to end.
- Live check in a real Claude Code session:
  `claude mcp add mcp-apps-sample -- <agent-chat bin> wrap --name mcp-apps-sample -- go run ./testdata/mcp-apps-sample --ui=auto`,
  ask the agent to show the calendar, see the page in chat, click a cell,
  and the agent books it.
- **Done when** `make test` is green (warm CDP first) and the live check has
  been seen working, with a screenshot in this file.

## Total

About 5 working days.

## Later (separate tasks, not this one)

1. Page buttons call tools directly through the wrapper. It already sits in
   the line, so state is shared. Ask the user before each call.
2. Fallback for servers that aren't wrapped: an agent-chat tool `show_app`
   that the agent calls, and agent-chat fetches the page itself.
3. swe-swe: start the servers it configures through `agent-chat wrap` when the
   installed agent-chat has it. Also a helper to wrap servers the user added
   with `claude mcp add`.
4. Remote (HTTP) servers: document `agent-chat wrap -- npx mcp-remote <url>`.
5. swe-swe `--without-mcp` mode: `mcp-cli-proxy --advertise-ui` plus
   `mcp --read-resource` (`/workspace/tasks/2026-11-16-mcp-cli-read-resource.md`).

## Logs

(append phase logs here)
