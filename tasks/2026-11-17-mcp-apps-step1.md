# MCP Apps, step 1: show tool pages in agent-chat via `agent-chat wrap`

Filed 2026-11-17. Status: **DONE** (all 5 phases, 2026-11-17). Branch: `mcp-apps`.
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

## Wrap safety (requested from the swe-swe session, 2026-11-18)

Why: if swe-swe starts every tool server it sets up through `agent-chat wrap`
(Later 3), one failure in `wrap` makes all of those servers disappear at once.
Causes: an older agent-chat without `wrap`, npm unreachable, a bug in `wrap`.
All 4 must hold before swe-swe wraps anything. Scope: only servers swe-swe
sets up; servers the user added are left alone. swe-swe-side notes:
`/workspace/tasks/2026-11-16-mcp-cli-read-resource.md` ("Phase 5", "Wrap safety").

1. **Check first, fall back** (agent-chat side: must ship in `wrap`'s first
   release, so before this branch merges).
   - Add `agent-chat wrap --version`: prints `agent-chat wrap <version>` and
     exits 0. It starts nothing, reads no env, and writes nothing.
   - **Trap for swe-swe:** an *older* agent-chat run as `agent-chat wrap
     --version` does not fail. Its flag parsing stops at the bare word `wrap`,
     so it starts a **full chat server** (stdio MCP, HTTP port, chat-log
     export if `AGENT_CHAT_*` env is inherited) and hangs until the timeout
     kills it. So the safe check is two-step:
     1. `agent-chat -h` (every version prints usage and exits; nothing starts).
        New binaries mention `agent-chat wrap` in that usage. No mention =
        no wrap, so fall back.
     2. Only then run `agent-chat wrap --version` (short timeout) to confirm.
   - Add the `wrap` line to the top-level `-h` usage.
   - Tests: `wrap --version` exit 0 plus output, with no child started; `-h`
     output contains `agent-chat wrap`.
2. **Look it up once**: swe-swe side. It reuses the agent-chat binary already
   downloaded for the chat server and runs no `swe-npx` per wrapped server.
   Nothing to do in agent-chat: `wrap` lives in the same binary.
3. **Keeps relaying even when its extras fail.** The only fatal error is the
   tool server failing to start (exit 127).
   - Already true and tested: agent-chat unreachable or POST fails, no
     password, page fetch fails or times out, AGENT_CHAT_PORT unset (plain
     relay), non-JSON lines, batches, lines over 16 MB.
   - Missing: a **crash (Go panic) inside the extras** (the capability edit,
     page tracking, delivery goroutine) would kill the whole wrapper. Fix:
     recover in each hook call and in the delivery goroutine. On a panic,
     forward the original line unchanged, log once to stderr, and switch the
     extras off for the rest of the session (pure relay from then on).
   - Tests: inject a panicking hook and a panicking delivery. Traffic still
     flows unchanged, the extras are off afterwards, and exit codes are
     unchanged.
4. **Off switch** `SWE_SWE_NO_WRAP=1` in `.swe-swe/env`: swe-swe side. agent-chat
   keeps `--no-advertise-ui` and the "no AGENT_CHAT_PORT = plain relay" rule
   as its own lower-level switches.

Estimate (agent-chat side, items 1 and 3): about 1.5 hours including tests.

## Logs

### Phase 1 (2026-11-17): done

- `wrap.go` + `wrap_test.go` (11 tests, fake child via `TestMain` re-exec);
  dispatched in `main()` before flag parsing.
- Beyond the plan: a client that already declares the ui extension is left
  alone; a JSON batch (array) is forwarded unedited; CRLF endings are kept;
  child killed by a signal → exit 128+signal; missing binary → exit 127.
- Bug caught by the over-long-line test: a line just over 16 MB whose final
  chunk contained the newline was still parsed. Fixed by checking the length
  before editing.
- Check: `printf <initialize as Claude Code 2.1.325 sends it> | agent-chat wrap -- mcp-apps-sample --ui=auto`
  → sample logs `ui=true`, and `tools/list` carries `_meta.ui.resourceUri`.
  Unwrapped → `ui=false`; `--no-advertise-ui` → `ui=false`.
- `make unit-test` green. `make e2e-test` NOT run: the CDP browser returned
  502 (`localhost:6045/json/version`) on two warm-up attempts. Phase 1 touches
  no client-dist file; E2E is due at phase 4/5 anyway.

### Phase 2 (2026-11-17): done

- `wrapapps.go` + `wrapapps_test.go` (8 tests; fake page server via the
  same `TestMain` re-exec, `httptest` stand-in for agent-chat). The relay
  now takes a hook that can replace or drop a line and run something after
  it is written; both directions write whole lines under a lock, so the
  wrapper's own `resources/read` never lands mid-line.
- Flags: `--name`, `--agent-chat-url` (default
  `http://127.0.0.1:$AGENT_CHAT_PORT/api/mcp-app`). Neither set → plain
  relay, no page tracking at all.
- Beyond the plan: tools whose answer has `isError: true` get no page;
  `tools/list` first page (no cursor) replaces the known tools, later pages
  add; only `ui://` uris count; page cache cleared on `tools/list_changed`
  and `resources/list_changed`; a failed fetch is retried next time; a
  timed-out `acw-N` reply that arrives late is still swallowed; when the
  server's output ends, waiting fetches fail at once and in-flight posts get
  up to 3s. POST body also carries `toolMeta` (the tool's `_meta`).
- Ordering test: the stand-in agent-chat blocks every POST, and the client
  still gets the tool's answer and later replies.
- Check with the real sample server and a capture server on a spare port:
  the client got the normal answers; the capture got one POST with
  server `mcp-apps-sample`, tool `show_calendar`, the 5428-byte page,
  `resourceMeta {"ui":{"prefersBorder":true}}`.
- `make unit-test` green. E2E not run (same reason as phase 1; no
  client-dist change).

### Phase 3 (2026-11-17): done

- **Decision (user, 2026-11-17): require a password, plus loopback only.**
  agent-chat makes a random 64-hex token per run and writes it, after the
  listener is up, to `<user cache dir>/agent-chat/mcp-app-tokens/<port>`
  (dir 0700, file 0600, temp file + rename). The wrapper reads the file for
  the port in its post URL on every post (so a restarted agent-chat is
  picked up) and sends it as `X-Agent-Chat-Token`. Blocks other machines,
  other containers and other user accounts; not programs running as the
  same user (they can read every file of the user's anyway; only a swe-swe
  handed-out secret could do better). No token file (older agent-chat) →
  the wrapper logs "no password" and skips the post; the tool is unaffected.
- `mcpapp.go` + `mcpapp_test.go` (5 tests), one more wrapper test (no
  password → no post). `POST /api/mcp-app`: loopback (403), token (401),
  4 MB (413), mimeType must be `text/html;profile=mcp-app` (415), tool +
  html required and `ui://` uri (400).
- Pages are stored by sha256 under `<user cache dir>/agent-chat/mcp-app-pages/`,
  not the per-run temp upload dir, so they survive restarts and identical
  pages are stored once. Not under `/uploads/`, which would serve them as
  `text/html` on agent-chat's own origin; instead
  `GET /api/mcp-app/page/<sha256>` serves `text/plain` + `nosniff` +
  `CSP: sandbox`. Nothing cleans the store yet (pages are a few KB).
- Event: `{"type":"mcpApp","app":{id, server, tool, arguments, result,
  resourceUri, pageId, resourceMeta, toolMeta}}` (new `Event.App`). The
  current UI ignores unknown event types, so a phase-3-only build shows
  nothing new and breaks nothing.
- Chat log: `**AGENT**` + `> 📱 \`server/tool\` page shown`, with the usual
  "took" line; the page itself isn't embedded.
- Check: demo agent-chat on port 4019 (`env -i`, `-no-stdio-mcp`) + wrapper +
  sample server: token file written 0600, page posted, page served back as
  `text/plain` with `CSP: sandbox`; a wrong password got 401.
- `make unit-test` green. E2E not run (CDP 502 earlier; no client-dist change).

### Phase 4 (2026-11-17): done

- New `client-dist/mcp-app.js` (loaded after `app.js`; uses its globals),
  hooked into both the live event switch and `replayHistory` as `mcpApp`.
  Bubble = `addBubble("📱 \`server/tool\` page")` + `<iframe sandbox="allow-scripts"
  referrerpolicy="no-referrer">`; html fetched from `api/mcp-app/page/<id>`
  and set as `srcdoc`, with a CSP `<meta>` put first in `<head>`.
- CSP: `default-src 'none'`, inline script/style, `data:`/`blob:` images and
  media, `connect-src`/`frame-src`/`base-uri` `'none'` unless the resource's
  `_meta.ui.csp` lists domains; only `http(s)://`/`ws(s)://` sources made of
  plain URL characters are copied in, so a page can't inject a directive.
- Beyond the plan:
  - `ui/message` is accepted only while that page's iframe has focus (the
    user clicked into it), so a page can't post chat messages on its own as
    it loads. Sent through `submitUserMessage` as a new `page` source (no
    interrupt word, no text-box lock), text "📱 From the \`server/tool\` page: …".
  - `ui/request-display-mode` → `inline`; `ping` → `{}`; page logs
    (`notifications/message`) go to the console; theme flips are sent as
    `ui/notifications/host-context-changed`.
  - Height: reported height + the iframe's own border (it is border-box
    here), capped 60–800px. Without the border the page got a scrollbar.
  - Bubble width `calc(100% - 40px)`: at 100% the "⋯" button (32px outside
    every agent bubble) caused a sideways page scroll.
  - Sample page now reports `scrollWidth/scrollHeight` (test-only file).
- Shared CDP browser still answered 502, so checks ran on a local headless
  chromium (`chromium --headless=new --remote-debugging-port=9333`) with
  `CDP_ENDPOINT=http://localhost:9333 E2E_SERVER_PORT=0`; the worktree's
  `node_modules` is a symlink to the main checkout's (gitignored).
- New E2E `e2e/mcp-app.spec.cjs` (2 tests, written now rather than in
  phase 5 because it was the only way to click inside the page): sample page
  handshakes and draws; click → page's `tools/call` refused → `ui/message`
  → user bubble; reload replays it; a hostile page's own `ui/message` and
  `tools/call` are refused and its `fetch('https://example.com')` is blocked.
- Screenshot after a click: the page in its bubble, and the user message
  below it.
- Tests: `make unit-test` green. Full E2E on the local chromium with
  ephemeral ports (parallel): 251 passed, 2 failed, 20 min. Neither failure
  is from this change:
  - `markdown-images.spec.cjs:190` passed on a rerun (flaky under parallel load).
  - `draft-keep.spec.cjs:147` ("another session does not pick up the draft")
    fails every time on ephemeral ports: its two sessions get two ports, so two
    origins and two separate localStorages. Pinned to one port
    (`E2E_SERVER_PORT=3005`), `draft-keep` + `mcp-app` specs: 7/7 passed.

### Phase 5 (2026-11-17): done

- `mcpapp_e2e_test.go`: builds the real sample server, runs it behind
  `agent-chat wrap` (in-process) with a Claude-Code-shaped initialize, posts
  to the real `/api/mcp-app` handler, and checks the `mcpApp` event and that
  `/api/mcp-app/page/<id>` serves `calendar.html` byte for byte. Wrapper test
  helper split into `startWrap(t, args)` for any child command.
- Live check, real Claude Code 2.1.326, no settings changed: instead of
  `claude mcp add`, a one-off `claude -p --mcp-config mcp.json
  --strict-mcp-config` run with two servers: `sample` =
  `agent-chat wrap --name mcp-apps-sample -- mcp-apps-sample --ui=auto`
  (env `AGENT_CHAT_PORT=3005`), and `agent-chat` = a dev-build agent-chat on
  3005 over HTTP MCP. Result, in order:
  1. Claude Code connected through the wrapper; the sample logged
     `initialize: mode=auto ui=true` (Claude Code alone gives `ui=false`).
  2. Claude called `show_calendar`; the page appeared in the chat.
  3. Claude's `send_message` waited; clicking "Bob, Wed 25" inside the page
     arrived as the reply: "📱 From the `mcp-apps-sample/show_calendar` page:
     Please book Bob on Wed 25 (book_slot)."
  4. Claude called `book_slot {Bob, Wed 25}` and reported it booked.
- Screenshot: [`2026-11-17-mcp-apps-step1-live.png`](2026-11-17-mcp-apps-step1-live.png)
  (after reload).
- Seen in the live check, for a later task: the page keeps showing the
  answer it was opened with. After `book_slot` it still shows Bob/Wed 25 as
  "free", because `book_slot` has no page of its own and nothing updates the
  existing one. Fix belongs with "page buttons call tools directly" (Later 1).
- `make unit-test` green; E2E as in phase 4.
