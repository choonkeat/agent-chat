# MCP Apps step 2: page buttons run tools directly

Status: planned 2026-11-18, not started. Builds on
`tasks/2026-11-17-mcp-apps-step1.md` (done; branch `mcp-apps`, not merged).

## Goal

A click in a tool page (e.g. "Bob, Wed 25" in the sample calendar) runs the
tool straight away: page → chat window → agent-chat → `agent-chat wrap` →
tool program → back the same way. The page updates itself (the cell turns
"pending"), and no chat message or Claude turn is needed. Claude gets a short
note with the user's next message. Pages also pick up agent-chat's colours.

The sample calendar already tries `tools/call` first and falls back to
`ui/message` (step 1's path), so the sample itself needs no logic change.

## Decisions (made in chat 2026-11-18)

- **Route:** the wrapper keeps a waiting request (long poll) open to agent-chat.
  That means no new port and the same password file as step 1.
- **Which tools:** only tools whose `_meta.ui.visibility` includes `"app"`.
  No visibility marking means `["model","app"]`, per spec. Only tools from
  the page's own tool program.
- **Permission (option A + "Always allow"):** the first tool run from a page
  asks "Let the `<server>` page run its tools? Yes / No". A Yes covers that
  page. After a Yes in 2 different sessions for the same tool program (by
  name), the question also offers **Always allow**, remembered on this machine
  (server side, not localStorage: each session's chat uses its own origin).
  A No resets the count. Settings has "Forget allowed tool programs".
  Known limit: a different program given the same name is also allowed.
- **Claude is told** with the user's next message ("Meanwhile, from tool
  pages: …"). It does not wake Claude by itself.
- **Spec background** (checked in `@modelcontextprotocol/ext-apps` types):
  - The reference `AppBridge` forwards `tools/call` with no prompt.
  - Prompts are "should" for downloads, "may" for links, and "should" for
    sampling.
  - Asking before tool runs is left to the host.
- **Compatibility** (memory `agent-chat-swe-swe-version-compat`):
  - An old wrapper sends no wrapper id, so its pages keep step 1's behaviour
    (`tools/call` refused, falls back to messages).
  - An old agent-chat (404 on the polling address) makes the wrapper stop
    polling.
  - agent-chat being down never affects the relay (wrap safety rule 3).

## Before this: wrap safety (step 1 plan, "Wrap safety", about 1.5 hours)

1. `agent-chat wrap --version`, plus a `wrap` line in the top-level `-h`.
2. Recover from crashes in the extras: forward the line unchanged and switch the
   extras off. The panic recovery also covers the new polling and call code
   below.

## Phases

### Phase 1: wrapper can run a tool when asked (about 1.5 hours)

Achieves: `wrapApps.callTool(name, args) (result json.RawMessage, err)`. Not
reachable from outside yet.

Steps:
1. Learn *every* tool from `tools/list`: name plus visibility. Keep the
   existing page-tool tracking. First page replaces the list, later pages add
   to it, and `tools/list_changed` clears it.
2. `callTool`:
   - refuses unknown tools and tools without `"app"` before contacting the
     server
   - otherwise sends `tools/call` with an `acw-N` id
   - the reply is swallowed (never reaches the client), as with page fetches
3. Time limit of 2 minutes, then the error "tool took too long". A late reply
   is still swallowed (`abandon` keeps the id).
4. A JSON-RPC error reply comes back as an error. A result with
   `isError:true` comes back as a normal result, and the page shows it.

Verify (TDD, in `wrapapps_test.go` with `fakeAppsServer`, adding a
`book`-style tool and a model-only tool):
1. An app-visible tool returns the server's answer, and the client side gets
   no extra line.
2. Model-only and unknown tools are refused, and the server never sees a
   request.
3. A server that never answers gives the timeout, and the late answer doesn't
   leak to the client.
4. A JSON-RPC error is passed back.
5. After `list_changed` plus a new `tools/list`, the new visibility applies.

Regression: all existing wrap/wrapapps tests, `make unit-test`.

### Phase 2: line between agent-chat and the wrapper (about 2 hours)

Achieves: agent-chat can hand a tool request to the wrapper that owns a page
and get the answer back.

Steps:
1. Each wrapper process makes a random id (32 hex) at start and sends it as
   `wrapper` in the page POST. `McpApp` gains `Wrapper string` (stored with
   the event, so a replayed page still knows its wrapper).
2. After the first successful page delivery, the wrapper loops:
   - `GET /api/mcp-app/calls?wrapper=<id>`, with the token header and
     loopback check
   - agent-chat holds it open for up to 25 seconds, then returns
     `[{callId, tool, arguments}]` or `[]`
3. For each call, the wrapper runs `callTool` in a goroutine, then
   `POST /api/mcp-app/calls/<callId>` with
   `{result}` or `{error:{code,message}}`.
4. Browser-facing `POST /api/mcp-app/call` `{appId, tool, arguments}`:
   - look up the app, then its wrapper
   - if that wrapper hasn't polled in 35 seconds → 503 "tool program not
     running"
   - otherwise queue the call and wait up to 2m10s for the result, and return
     it
5. Never break anything:
   - errors back off from 1 s to 30 s, and the relay is untouched
   - a 404 on `/calls` (old agent-chat) stops polling for good
   - `shutdown` stops polling
6. State is in memory only. When agent-chat restarts, wrappers reconnect on
   their next poll.

Verify (TDD):
1. Two wrappers: a call reaches only the owner, and its answer comes back.
2. A wrapper that isn't polling gives 503 in under 1 second.
3. Polling without the token → 401, and from a non-loopback address → 403.
4. A result for an unknown callId → 404.
5. Wrapper side, against `fakeAgentChat`:
   - it picks up a call, runs it on the fake server, and posts the result
   - with agent-chat down, the relay still flows and it retries
   - a 404 makes it stop polling

Regression: step 1 tests (`mcpapp_test.go`, `wrapapps_test.go`, E2E Go test),
`make unit-test`.

### Phase 3: chat window passes page tool requests, with permission (about 3 hours)

Achieves: clicking a free cell books it in place (after the permission
question), and no chat message is sent.

Steps:
1. `ui/initialize` advertises `hostCapabilities.serverTools: {}` only when
   `app.wrapper` is set.
2. Page `tools/call` handling:
   - refuse unless `document.activeElement === iframe` (step 1 rule)
   - permission check (below)
   - `fetch('api/mcp-app/call')` and reply with the result or the error
3. One call at a time per page. A second one gets the error "busy".
4. No wrapper: the same refusal as step 1, so the page falls back.
   503: the error "tool program not running", so the sample falls back to
   `ui/message`.
5. Permission:
   - an in-bubble prompt (not `window.confirm`) with Yes / No, plus
     **Always allow** when that server's yes-count ≥ 1 from an earlier session
   - a Yes is remembered for this page (in memory) and recorded server-side
     once per session
   - server store: `<cache>/agent-chat/mcp-app-allow.json` (0600) with
     `{server: {yesSessions: [sessionIds], always: bool}}`
   - endpoints `GET/POST /api/mcp-app/allow` (same-origin only, like other
     UI endpoints) and `DELETE` to forget all
6. Settings: a "Forget allowed tool programs" button.

Verify (TDD; E2E in `e2e/mcp-app.spec.cjs`, where the spec plays the wrapper by
polling `/api/mcp-app/calls` with the token; Go tests for the allow store):
1. Click free, then Yes: the cell turns pending, the status reads "Booked (via
   tool call)", and there's no user bubble.
2. Click, then No: nothing runs, and the page falls back to a chat message.
3. A hostile page calling a tool without focus is refused (extend the existing
   test).
4. A page without a wrapper behaves exactly like step 1 (the existing test
   stays green).
5. Always allow:
   - not offered the first time
   - offered after a Yes recorded in another session (seeded store)
   - once chosen, no prompt
   - after Forget, the prompt is back
   - a No resets the count
   - Go: file mode 0600, and the yes-count goes up once per session

Regression: the full E2E run, except the 2 known ephemeral-port failures
(`draft-keep:147`, flaky `markdown-images:190`).

### Phase 4: Claude is told (about 1.5 hours)

Achieves: a chat line after each page tool run, and Claude gets it with the
next user message.

Steps:
1. After `/api/mcp-app/call` succeeds or fails, publish an `mcpAppCall` event:
   server, tool, arguments, and a short result (the first text content, cut to
   about 300 characters). Shown as a small line:
   "📱 `server` page ran `tool` (args): text".
2. Add it to a bus "pending notes" list. When messages are drained for the
   agent, notes are put first as one block: "Meanwhile, from tool pages: …".
   Then they're cleared, so each is delivered exactly once.
3. Doesn't wake a waiting `send_message` or `check_messages`, and doesn't
   count as a queued message.
4. Chat log export plus history replay (`renderChatBubble`,
   `chatlogstream` type lists, `app.js` replay and live switch).

Verify (TDD, Go):
1. A note, then a user message: the agent text has the note before the
   message.
2. Two notes, one message: both appear once, and the next message has none.
3. A blocking wait isn't ended by a note.
4. Chat-log line text, and the cut on long answers.
5. E2E: the line appears after the click and survives a reload.

Regression: existing delivery tests (limbo, redelivery, guidance rotation:
`resetGuidance(t)`), `make unit-test`.

### Phase 5: pages use agent-chat's colours (about 1.5 hours)

Achieves: pages that use the standard style names blend into the chat in both
themes.

Steps:
1. Build `hostContext.styles.variables` (spec `McpUiStyleVariableKey`) from
   agent-chat's computed CSS variables:
   - `--color-background-*` from `--bg-*`
   - `--color-text-*` from `--text-*`
   - `--color-border-*` from `--border-*`
   - plus fonts and radius
   - only keys we have a real value for
2. Send them in `ui/initialize`, and again in `host-context-changed` on a theme
   flip.
3. Sample `calendar.html`: use `var(--color-background-primary, Canvas)` etc.
   (with the current fallbacks), and apply the variables on init and on
   change.

Verify (TDD, E2E):
1. In dark and in light, the page body background equals the chat's
   `--bg-primary` value.
2. A flip updates it without a reload.
3. A page that ignores the variables still renders.

Regression: step 1's E2E theme behaviour, and the full E2E run.

### Phase 6: full check (about 1.5 hours)

Steps:
1. Extend `mcpapp_e2e_test.go`: the real sample, plus a browser-style
   `POST /api/mcp-app/call` running `book_slot`. Checks:
   - the right answer comes back
   - the client side gets nothing extra
   - the note is pending
2. Live run, like step 1: a one-off `claude -p --mcp-config` with no settings
   change.
   1. Show the calendar.
   2. Click Bob/Wed 25, then Yes.
   3. The cell turns pending, with no chat message.
   4. Ask "what changed?", and Claude cites the note.
   5. Flip the theme, and the page follows.
3. Screenshots go in this file. The log goes in this file, with remaining
   gaps.

Done when: `make test` is green (warm CDP first; known ephemeral-port failures
aside) and the live run has been seen working.

## Total

About 11 hours (phases 1–6), plus 1.5 hours of wrap safety beforehand.

## Later

1. A booking made in the page vanishes from view after a reload: the page
   replays the original result. Store the latest page result per app.
2. Settings list of allowed tool programs with per-program remove.
3. `ui/update-model-context` support (spec) as another way to tell Claude.
4. (Noted 2026-11-18; user: "note the website issue for later") The chat's
   live connection (`/ws`, `CheckOrigin` returns true in main.go) and other
   browser endpoints accept requests from any website open in the browser. A
   hostile site could read the chat or send messages as the user. This predates
   MCP Apps. Fix idea: same-origin check on `/ws` upgrades and on state-changing
   POSTs, mindful that agent-chat runs inside swe-swe's cross-origin iframe.

## Logs

### Phase 1 (2026-11-18): done

- `wrapApps.callTool(name, args)` in wrapapps.go. It checks `appTools`
  (learned from every tool in `tools/list` via `wrapAppVisible`: no visibility
  means allowed, otherwise it must include `"app"`; cleared on
  `tools/list_changed`), then sends `tools/call` under an `acw-N` id.
- The wrapper's own requests now share `request(method, params, timeout)`:
  - `pending` carries `wrapReply{result, err, closed}`
  - `fetch` uses it too
  - errors are `*wrapCallError{Code, Message}`: -32602 refused, -32001
    "tool took too long" (`wrapCallTimeout`, 2 min), -32000 "tool program not
    running" (after shutdown), or the server's own JSON-RPC error
- An `isError` result is returned as a normal result.
- Tests (`wrapcall_test.go`, harness feeding replies through `serverHook`):
  - app-visible tools run and the reply is dropped
  - model-only and unknown tools are refused and the server sees nothing
  - a timeout swallows the late reply
  - errors are passed back
  - list changes are followed
  - shutdown frees a waiting call
- `make unit-test` green.

### Phase 2 (2026-11-18): done

- `mcpappcall.go` (agent-chat side): an in-memory `mcpAppRelay`.
  - `GET /api/mcp-app/calls?wrapper=<id>`: the wrapper waits up to 25 s
    (`mcpAppPollWait`) and gets a list of queued calls, or `[]`.
  - `POST /api/mcp-app/calls/<callId>`: the wrapper's `{result}` or `{error}`.
  - `POST /api/mcp-app/call`: from the browser. It takes
    `{wrapper, server, tool, arguments}` and answers 200 with
    `{result}`/`{error}`, or 503 "tool program not running" when the wrapper
    hasn't polled within 35 s (`mcpAppPollStale`) or no wrapper is given (older
    wrap).
  - Wrapper endpoints share `mcpAppWrapperAuth` (loopback + token) with page
    posts.
  - The browser endpoint accepts only `application/json` (cross-site needs a
    CORS preflight, which is never granted) and rejects `Sec-Fetch-Site`
    other than same-origin.
  - `McpApp.Wrapper` is stored with the page event.
- Wrapper side (wrapapps.go):
  - a random 32-hex `id`, sent as `wrapper` in page posts
  - after the first 2xx page post, `pollLoop` starts (once)
  - each call goes through `runPageCall` → `callTool` → POST the answer
  - errors back off 1 s → 30 s
  - 404/405 means an older agent-chat: log once and stop
  - `shutdown` closes `stop` and cancels the poll
  - all goroutines use `guard.catch()`
- Tests (`mcpappcall_test.go`):
  - routing to the owning wrapper, plus error answers
  - not running / stale / no wrapper → 503 immediately
  - password, loopback, missing id, unknown call
  - the browser endpoint refuses cross-site and text/plain
  - the real wrap + fake server + real handlers run a tool, and the client sees
    nothing
  - an old agent-chat stops polling after 1 try
  - a failing agent-chat retries and the relay is untouched
- `make unit-test` green.

### Phase 3 (2026-11-19): done

- `mcpappallow.go`: `GET/POST/DELETE /api/mcp-app/allow`.
  - The store is `<cache>/agent-chat/mcp-app-allow.json` (0600, atomic) with
    `{servers: {name: {yesSessions, always}}}`.
  - Sessions are told apart by `SESSION_UUID`, or a random id per run.
  - `offerAlways` = a Yes from another session. "no" deletes the entry, and
    DELETE forgets everything. The last 5 sessions are kept.
  - `mcpAppFromChatPage` (Sec-Fetch-Site same-origin, JSON bodies) is now
    shared with `/api/mcp-app/call`.
- `mcp-app.js`:
  - `serverTools` is advertised only when `app.wrapper` is set
  - `tools/call` order: no wrapper → refusal (step 1 fallback), then the focus
    rule, then busy (one at a time), then `mcpAppAllowed` (the in-bubble
    question `.mcp-app-ask`: Yes / Always allow / No; a Yes is remembered for
    the page), then `mcpAppRunTool` → `api/mcp-app/call`
  - Settings: "Tool pages" row with `#btn-mcp-app-forget`
- Change from plan: after **No**, the sample's fallback chat message is
  refused too. The user is on the No button, not in the page, so nothing is
  sent at all. That's better than plan step 2's "falls back to a chat message".
- E2E (`e2e/mcp-app.spec.cjs`, the spec plays the wrapper):
  - Yes books in place with no user bubble, and a second click doesn't ask
  - No runs nothing and sends nothing
  - Always allow is offered after a seeded earlier-session Yes, survives a
    reload, and Settings Forget brings the question back without Always
  - the hostile page (now with a wrapper) is still refused and no question
    appears
  - 5/5 pass (local chromium on 9333, `E2E_SERVER_PORT=0`)
- Go: `mcpappallow_test.go`. `make unit-test` green.
