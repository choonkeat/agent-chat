# Changelog

All notable changes to agent-chat are documented in this file.

## [Unreleased]

### Features
- `/clear <instruction>` in the chat input resets the agent's context and hands
  it the rest of the message as its next instruction. The `/clear ` prefix is
  stripped; what remains is recorded in the chat and the streaming chat log, and
  the agent comes back pointed at that log file — so the conversation survives
  the reset even though the agent's own memory does not. A bare `/clear` resets
  with no follow-up.
  **The order is the design: wipe, then record, then point at the file.**
  Recording first lets a still-running agent consume the instruction and then be
  erased holding it. Pointing the resumed agent at the message queue instead of
  the file goes silent in two ways — a `/clear` does not release agent-chat's
  blocking wait (Claude Code sends no `notifications/cancelled` on a terminal
  break-out), so the parked waiter swallows the instruction into a dead request,
  and the spare copy that exists for exactly that case is discarded by the first
  `send_progress` the fresh agent makes. `check_messages` then returns empty and
  tells the agent to stay quiet, leaving a chat that looks alive and answers
  nothing. The chat log has neither failure mode.
  The filename is read from the new `GET /api/chatlog-path` at clear time, not
  cached at connect, because `set_chat_title` renames the file mid-session. A
  `⟪ context cleared ⟫` marker is written at each reset for a future
  read-only-since-the-last-clear mode. Requires `AGENT_CHAT_EXPORT_DIR`; without
  it the reset still happens, says so in the chat, and starts blank. See
  `docs/adr/2026-08-02-clear-prefix-context-reset.md`.

### Fixes
- A message consisting of exactly a slash-triggered word (e.g. a bare `/clear`)
  could not be sent: the autocomplete trigger only dies at the first space, so
  the dropdown was still open and swallowed the Enter — with nothing selectable
  in a "No results" dropdown, the message simply never sent.

## [0.8.22] — 2026-08-01

### Features
- Bare workspace paths in chat text are now clickable. A bubble that merely
  mentions `client-dist/app.js` renders it as `@client-dist/app.js`, linked
  into the embedder's Files pane — the same link the explicit
  `[app.js](client-dist/app.js)` form already got, without anyone writing
  markdown. A directory shows its trailing slash (`@docs/adr/`), and a path
  already written with `@` is not double-prefixed.
  **Existence on disk is the whole filter.** Of 188 unique path-shaped tokens
  in this repo's own chat archive only 31 exist, and the rejects — `Ctrl/Cmd`,
  `origin/main`, `7/7`, `and/or`, `bump/publish`, `x64/arm64`,
  `@choonkeat/agent-chat` — are indistinguishable from real paths by shape
  alone. The server is the only side that can stat, so it attaches the
  confirmed paths to each bubble as `file_paths` and the browser links exactly
  those. A single-backtick span whose entire content is such a path links too;
  fenced ``` blocks stay literal and remain the way to show a path without
  linking it.
  Off unless the embedder passes `files_url` — with no Files pane a link would
  lead nowhere, so the extractor never runs. Annotation happens once as each
  bubble is written, never at read time, and is never stored in the JSONL
  archive. Consequences of that, stated plainly: a history restored after a
  restart shows as plain text until those bubbles are re-published, and a file
  deleted after its bubble was annotated keeps its link until restart. See
  `tasks/2026-08-01-autolink-workspace-paths.md`.
- Markdown links to workspace files now open in the embedder's Files pane. A
  bare workspace path — `[main.go](cmd/main.go)` — used to resolve against the
  parent app, which never served those paths and 404'd. When the embedder
  passes `files_url` (its per-session file-browser origin), such links get an
  absolute Files-pane href: cmd-click opens a real tab, a plain click posts
  `agent-chat-open-files` to the parent. Absolute links are covered too — the
  server inlines its symlink-resolved working directory as `WORKSPACE_ROOT`,
  and a `/`-prefixed link underneath it (what `@/` autocomplete produces) is
  rewritten the same way. Root-anchored links outside it (`/api/fork/…`,
  `/.swe-swe/uploads/…`) and images still resolve against the parent. No
  `files_url` means the feature stays off, so a new client inside an older
  embedder behaves exactly as before.
- Agents are now instructed to open every turn with a one-line `send_progress`
  call. A user's bubble stays dim until an agent-chat tool call reaches the
  server, so an agent that only replies at the end of a long turn left the
  bubble looking unread for minutes. The expected prompt text is pinned in
  `tools_test.go`.

### Fixes
- A user bubble no longer claims to be read when nothing received it. Breaking
  out of the agent's blocking `send_message` in the host terminal leaves a dead
  server-side waiter that still drains the queue, so the reply flipped straight
  to "read" — and lost its `⋯` menu, hiding **Send as interrupting** at the one
  moment it would have fixed things. A bubble now stays in the existing unread
  state (dim, tooltip, `⋯` menu, below the loader) until the agent *proves* it
  received the message: new `userMessagesRead` event, published by
  `bus.ProveDelivery()` at the top of every agent-chat tool handler. Draining
  the queue only sets `data-handed-over`, which hides **Delete** (an unsend can
  no longer pull the message back) and leaves the interrupt recovery in place.
  See `docs/adr/2026-08-01-unread-until-proven-read.md`.
- The on-screen keyboard no longer hides the newest messages. The sticky
  `#chat-footer` paints over flow content whenever the document is not
  scrolled to the very end, and opening the keyboard shortens the visible area
  without moving the scroll offset — parking the input bar on top of about 40%
  of an iPhone screen. The view now re-pins on both `window` resize (iframe
  embedding, where the host resizes the frame) and `visualViewport` resize
  (standalone iOS Safari, where the keyboard is an overlay and the layout
  viewport never changes). `isUserScrolledUp` is honoured, so a reader who
  scrolled back through history is not yanked forward.
- Chat text no longer grows on its own on iPhone. Mobile Safari re-runs text
  autosizing whenever the layout shifts — keyboard opening, iframe resize,
  rotation — and inflated the type until the next reload. `html`/`body` now
  set `text-size-adjust: 100%` (plus the `-webkit-` prefix); the sizes here
  are already chosen for phone widths.

### Tests
- All 53 E2E navigations go through `gotoRetry()`. Each spec spawns its own
  server on a random port, and the CDP browser reaches it through a forwarder
  that takes a few seconds to notice a freshly bound port — 29 of 93 specs
  failed at connect that way in a full run, with zero assertion failures among
  them. `gotoRetry()` retries connection-level failures (including the
  `chrome-error://` interrupted-navigation shape) for up to 20s and costs
  nothing on a warm port. Full suite now 94/94, twice in a row.

## [0.8.21] — 2026-07-31

### Features
- Pasting 30 or more lines of plain text into the composer now stages it as a
  `pasted-<n>-lines.txt` attachment instead of inserting it. A log dump or whole
  file is unreadable in the textarea and more useful to the agent as a file it
  can open. Shorter pastes are unaffected, and pastes that carry an actual
  file/image keep their existing upload behavior. The threshold is
  `PASTE_AS_FILE_MIN_LINES` in `client-dist/app.js`.
- A paste that carries nothing usable now stages a red "nothing arrived" chip.
  iOS **Share > Copy** of a PDF hands the page a file reference it cannot open,
  so the paste event carries no `File` and no text and the composer did nothing
  at all — the paste simply looked broken. The chip is born failed, reuses the
  failed-upload red, names itself from whatever the clipboard still advertises
  (file URL → real filename, bare MIME type → `clipboard-empty.pdf`, nothing →
  `clipboard-empty`), never blocks **Send**, and clears when the message goes.

## [0.8.20] — 2026-07-25

### Features
- New **message style** settings panel: the browser stores a template that
  wraps every outgoing user message, so the agent sees the wrapped text while
  the chat bubble always shows the raw text typed. The template rides on the
  queued `UserMessage` (never on the broadcast `userMessage` event), so
  browsers render the un-wrapped bubble before any consumption signal. The
  orchestrator send-message path passes an empty template.
- Styles combine, as checkboxes. Picking "Non-technical" used to throw away
  "ADHD"; each pill now adds or drops only its own words, and which pills are
  filled is read from the text alone (by containment, so a hand-edited template
  still lights up every style inside it). The `{{message}}` marker is gone —
  the message always lands at the bottom — though the server still honours it
  for templates saved by an older client. "Off" is gone too: unticking every
  pill empties the box. A browser that never opened the panel starts on
  **Non-technical + ADHD**; an emptied box is a real answer and outlasts that
  default.
- **ADHD** preset: lead with the next action, number multi-step work, restate
  progress, suppress tangents, cut preamble/recap/closers. **Non-technical**
  preset also now says "don't use analogies".
- **Save as…** names an edited template and gives it its own pill, stored in a
  cookie beside the active style so it survives the next session. It is
  enabled only for the dashed **Custom** pill, since naming Custom is its whole
  job. The naming row hides **Save as…**/**Done** so only **Save**/**Cancel**
  compete for the tap, the "Active: …" line is gone (the pills already say what
  is on, and it squeezed the buttons on a phone), and the settings icon is a
  speech bubble instead of a sun.

## [0.8.19] — 2026-07-23

### Fixes
- The chat-archive `index.html` no longer goes dirty on every reply. It was
  regenerated after each quiet turn, adding manifest entries for `.md` files
  that were still untracked and still renameable by `set_chat_title` — so
  every session left `M agent-chats/index.html` in the working tree, and
  committing it published links to files that did not exist. `index.html` is
  now rewritten only at commit moments (`chatlog_close`, `chatlog_optout`,
  `export_chat_md`, and a `set_chat_title` that renames an export already in
  the manifest), and still-`untitled` exports are never listed. See
  `docs/adr/2026-07-24-index-html-only-on-commit-moments.md`.

### Build
- `make build` now refreshes the swe-swe npx cache. swe-swe launches each
  session's binary from
  `$SWE_SWE_HOME/npx-cache/@choonkeat/agent-chat-<platform>@<version>/bin/agent-chat`
  — not from `npm-platforms/` and not through `bin/agent-chat.js` — and
  `npm link` does not touch that cache, so a rebuilt fix silently never
  reached a newly started session. `scripts/refresh-npx-cache.sh` copies the
  freshly built host-platform binary over the cached copies, renaming the old
  inode aside first (a live session is executing that exact file, so a plain
  `cp` fails with `ETXTBSY`). Never fatal: a machine without the cache skips.

## [0.8.18] — 2026-07-22

### Fixes
- `send_message` is now documented as **terminal** and `send_progress` as
  **non-terminal**, in the reply-instructions template and in the
  `send_message` / `send_verbal_reply` / `send_progress` /
  `send_verbal_progress` tool descriptions. Agents (notably codex) would call
  `send_message` mid-task and stall: the blocking reply ends the turn, there is
  no background worker, and any remaining work silently paused until the user
  spoke again. The existing confirm-first carve-out for risky steps is kept.
- Pasting into an autocomplete trigger no longer kills the dropdown. iOS smart
  paste prepends a space when pasting after existing text, so typing `@` then
  pasting `docs/adr` yielded `@ docs/adr`. When the dropdown is open and the
  cursor still sits inside the trigger token, leading spaces/tabs are stripped
  and the text inserted via `execCommand`, so native undo and the input event
  (which re-queries autocomplete) still work.

## [0.8.17] — 2026-07-22

### Features
- `chatlog_status` and `chatlog_optout` are exposed on the orchestrator
  server, so the swe-swe server can offer "discard or commit this chat log?"
  when a session ends instead of routing every chat-log action through the
  agent. Identifying the file from outside does not work — `set_chat_title`
  drops the `SESSION_UUID` from the filename, and the `session:` header is a
  sha256 of the event-log path — so only the stream knows where its file is.
  `chatLogStream.Status()` reports enabled, path, dir, slug, titled, stopped,
  optedOut and a stat-backed exists. Unlike the agent-facing copies, these do
  **not** call `CancelActiveWait` or `AckLimbo`: an orchestrator asking about
  the log must never cancel a `send_message` the agent is blocked on.
  `optout` is idempotent, so a double-click cannot error.

## [0.8.16] — 2026-07-19

### Features
- New `chatlog_close` — the finalize twin of `chatlog_optout`. Committing the
  streaming archive always went dirty one turn later, because the "committed"
  reply is itself a chat event that re-appends to the just-committed `.md`.
  `chatlog_close` freezes this session's `.md` (keeps it, unlike optout), stops
  the index debounce, regenerates `index.html` once, and returns the exact
  repo-relative paths to `git add`. Renames only ever fill a blank: a still-
  untitled export must be named in the close call, an already-titled one is
  never renamed here. Idempotent, and freezing loses nothing — the JSONL event
  log keeps recording, and `set_chat_title` re-opens the export with a
  full-history rewrite.

### Fixes
- The provisional export is now `{date}-{NN}-untitled-{SESSION_UUID}.md` when
  the `SESSION_UUID` env var is non-blank, so a dangling untitled file is
  attributable to the session that wrote it. The display title stays
  "Untitled" and `set_chat_title` drops the suffix.
- A failed retitle no longer goes dark. `SetTitle` used to close and remove the
  old file before building the new one, so a failed rewrite or reopen left
  `s.f == nil` and every subsequent event silently dropped. It now builds the
  new file and opens its append handle first, committing the swap only after
  both succeed — any failure leaves the stream appending to the old filename
  and a later retry works.

## [0.8.15] — 2026-07-19

### Fixes
- The chat never grabs focus from an unfocused document. 102b934 stopped the
  `connected` reconnect path from stealing focus, but reconnects replay missed
  events through the normal live handlers, and `agentMessage` with quick
  replies, `historyEnd` deferred replies, and `draw` all call `enableInput()`
  without the `focusInput` flag — so in the common case (user in the host's
  Terminal tab, iframe websocket drops, agent replies while disconnected) the
  replayed message yanked focus into the chat textarea. The focus call is now
  gated on `document.hasFocus()`.

## [0.8.14] — 2026-07-18

### Features
- Streaming chat-log auto-export: set `AGENT_CHAT_EXPORT_DIR` (relative to
  cwd; cannot escape it) and every chat bubble is appended to
  `{dir}/{date}-{NN}-untitled.md` the moment it happens — attachments are
  copied into `assets/` at that same moment, while the upload files still
  exist. New `set_chat_title` tool renames the file (full header rewrite;
  callable again to rename), and `chatlog_optout` stops the export for the
  session and deletes its `.md` (`set_chat_title` re-enables). The archive's
  `index.html` is now **regenerated from the `.md` files on disk** (after a
  quiet-turn debounce, and on exit) instead of being patched incrementally —
  idempotent, so a git merge conflict in `index.html` heals on the next
  export: accept either side. A `session:` header line lets a restarted
  process (same `AGENT_CHAT_EVENT_LOG`) resume appending to its own file.
  `export_chat_md` remains as the manual escape hatch. Nothing is ever
  auto-committed.
- "Send as interrupting" on a pending user bubble now aborts the agent's
  current tool and submits `check_messages`, so the agent reads ALL queued
  messages through the normal agent-chat channel (full redelivery / ordering /
  file-attachment semantics) instead of having the message text typed
  out-of-band. Bubbles stay pending until the server's real
  `userMessagesConsumed` broadcast flips them, and the interrupt row shows
  only on the bottom-most pending bubble (it drains the whole queue).

### Fixes
- Pending user messages survive a server restart. The event log survived but
  the in-memory queue did not, leaving "ghost" pending bubbles the agent could
  never drain and Delete could not remove. The queue is now rehydrated at
  startup from every `userMessage` with no matching consumed/deleted event.

## [0.8.13] — 2026-07-13

### Fixes
- Pending user-bubble (⋯) menu button unified with the agent bubble's
  menu-button style.

## [0.8.12] — 2026-07-13

### Features
- Pending user bubbles get a (⋯) overflow menu with "Delete" and "Send as
  interrupting".

### Fixes
- The pending-bubble (⋯) menu stays faintly visible at rest so it is
  discoverable on touch screens.

## [0.8.11] — 2026-07-13

### Features
- When embedded in a host that passes `parent_url` (e.g. swe-swe), clicking a
  link to a local address (`localhost`, `127.0.0.1`, or a `*.lvh.me` vhost)
  inside a chat bubble now loads it in the host's App Preview pane instead of a
  new browser tab. The click is posted to the parent window as
  `agent-chat-open-preview`; the host routes it into Preview. Modified clicks
  (cmd/ctrl/shift/alt or non-left button) and non-local links keep the default
  new-tab behaviour, and standalone agent-chat is unaffected.

### Fixes
- No longer steals focus on reconnect.
- The local-preview host check matches `*.lvh.me` subdomains.

## [0.8.10] — 2026-07-12

### Fixes
- The MCP `send_message` call now survives the harness's stdio idle abort
  (Claude Code's ~30-min `CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT`, which fires with
  no `cancelled` notification) without losing the user's reply. A 60-second
  progress keepalive resets the idle window so a blocked `send_message` can
  wait on a human indefinitely, a single-active-waiter guard prevents duplicate
  waits, and any reply whose delivery dies in transit is redelivered on the
  next `check_messages` behind a `---REDELIVERY---` sentinel.

## [0.8.9] — 2026-07-11

### Features
- Paste images or files directly into the message textarea to upload them.
  A pure file/image paste (no accompanying text) is intercepted and staged
  for upload; a rich-text paste that carries an image snapshot alongside real
  text is inserted as plain text instead, leaving the image out.

### Fixes
- Wide markdown tables now scroll horizontally within their chat bubble
  instead of widening the whole page.

## [0.8.8] — 2026-07-08

### Features
- WebSocket keepalive ping/pong to cut reconnect churn on idle connections.

### Fixes
- The chat no longer force-scrolls to the bottom on reconnect or when a new
  prompt arrives, so your scroll position is preserved.

## [0.8.7] — 2026-07-08

### Fixes
- The loading tick now anchors to the previous bubble's timestamp, keeping the
  elapsed-time counter accurate.
- Session export warns and skips missing attachments instead of failing the
  whole export.

## [0.8.6] — 2026-07-02

### Fixes
- "Fork from here" is hidden on non-forkable progress bubbles.

## [0.8.5] — 2026-06-28

### Features
- Ticking elapsed-time counter on the loading indicator.

## [0.8.4] — 2026-06-28

### Features
- Exported asset filenames are content-addressed with a sha12 suffix, so
  identical assets deduplicate and names are stable across exports.

## [0.8.3] — 2026-06-28

### Features
- Per-bubble "Fork from here" action on agent bubbles, consolidated (along with
  the other bubble actions) into a ⋯ overflow menu.
- Filepath autocomplete now does a BFS directory walk including dotfiles, and
  supports absolute `@/…` queries against a configurable roots allowlist via
  the new `--filepath-roots` flag.

### Fixes
- Wider gap between the fork and play buttons to guard against fat-finger taps.

## [0.8.2] — 2026-06-27

### Features
- Relative-path markdown links now render. The link rule previously matched
  only `http(s)://` URLs, so `[text](/relative/path)` fell through as plain
  text. It now accepts relative URLs too (still blocking `javascript:`),
  mirroring the image rule.
- Relative links and images resolve against the parent window URL when
  embedded. When agent-chat runs inside a host iframe (e.g. swe-swe), reading
  the parent's location is blocked cross-origin, so the embedder passes its
  top-level URL via a `parent_url` query-string parameter. That value is used
  as the base for resolving relative `[text](url)` link hrefs and
  `![alt](url)` image srcs via `new URL()`. Absolute and protocol-relative
  URLs pass through unchanged; with no `parent_url` present it is a no-op,
  preserving prior own-origin behaviour.

### Fixes
- Autocomplete now re-fetches from the provider when client-side cache
  filtering empties the result set, instead of short-circuiting to a bare
  "No results". The provider's informative status ("No emoji matching X" /
  "No files matching X in DIR") is shown consistently, and a race that made
  the no-match state nondeterministic is removed.

### Tests
- `e2e/markdown-images.spec.cjs`: relative links (leading-slash and no-slash),
  `javascript:` rejection, and `parent_url` resolution — origin/path
  resolution, image src, absolute pass-through, no-base fallback, and the
  actual `?parent_url=` load wiring.

## [0.8.1] — 2026-06-20

### Features
- Ctrl/Cmd+Enter always submits. The submit/newline keydown handler now
  treats Ctrl/Cmd+Enter as submit on every platform — including hardware
  keyboards on mobile, where the `isMobile` bail previously swallowed the
  keystroke before any modifier check. Desktop plain Enter still submits;
  Shift/Alt+Enter still insert a newline.
- Welcome quick replies on an empty chat. A genuinely empty chat (zero
  events) now seeds hardcoded "welcome" quick-reply chips so the opening
  state signals "your turn" instead of reading as frozen. They are
  suppressed the moment any history exists (including a `send_progress`-only
  opening), and the agent's first `send_message` replaces them with its own
  context-aware replies. Overridable via the new `-welcome-replies` flag
  (comma-separated; `''` disables).

### Fixes
- Dropped the window `focus` + `visibilitychange` auto-focus
  (`focusChatInput`) that grabbed the textarea on every tab/window
  refocus. The four intentional focus points remain.

### Tests
- `e2e/chat-submit.spec.cjs`: 6 specs over desktop + mobile-UA contexts
  covering Enter / Shift+Enter / Ctrl+Enter / Cmd+Enter, asserting submit
  via `#loading-bubble` with a fresh server per test.
- `eventbus_test.go` (`HasHistory`), `main_test.go` (`parseWelcomeReplies`),
  and `e2e/welcome-replies.spec.cjs` cover the welcome-reply seeding and
  history-suppression behavior.

## [0.8.0] — 2026-05-24

### Features
- Export viewer markdown styling. The chatlog viewer now styles every
  element `marked.parse` emits — blockquotes (including nested and inside
  user bubbles), GFM tables, horizontal rules, `h4`–`h6`, and list items —
  which previously fell back to unstyled browser defaults, so quoted text
  and tables rendered broken in exported archives.
- Viewer assets are agent-chat-owned. `ensureViewerAssets` now overwrites
  `viewer.css`/`viewer.js` on every export instead of skipping existing
  copies, so bundled fixes reach every archive without manual deletion.
  `index.html` keeps its create-if-missing behavior (it is mutated in
  place with manifest entries). See ADR
  `2026-06-11-viewer-assets-agent-owned.md`.
- Exported markdown embeds agent-attached images, not just user uploads.
  Images posted via `send_message`/`send_progress` `image_urls` are copied
  to `assets/` and rendered inline within the agent turn's blockquote,
  matching how user uploads are archived. See ADR
  `2026-05-30-export-embeds-agent-images.md`.
- Agent steered off the built-in AskUserQuestion tool. Its MCQ renders
  only in the chat-invisible TUI and, unlike permission prompts, exposes
  no channel to intercept, so the reply-instructions prompt now directs
  the agent to route choices through `send_message` quick replies instead.

### Fixes
- Restart-safe tool ordinals. The per-tool `agent_tool_seq` counter ticks
  on handler entry to stay aligned with the agent's `.jsonl`, but two
  routine early-returns published no event — `send_message` rejected in
  voice mode, and `check_messages` draining an empty queue. Because
  `SeedToolCounters` reseeds from the on-disk event log at startup, those
  ticks were invisible after a restart, so the next stamp could collide
  with an ordinal the agent's rollout had already used. Both branches now
  emit a hidden `toolMarker` event carrying only the stamp; the UI event
  switches and the markdown exporter ignore unknown event types, so it
  renders nothing, while `SeedToolCounters` recovers the true count. This
  makes the early-return alignment promised in 0.7.1 hold across restarts.

### Tests
- `stamp_test.go`: marker-stamp emission and seed recovery from a
  `toolMarker` phantom. `tools_test.go`: render-guard asserting a
  `toolMarker` produces no markdown and never perturbs elapsed-time deltas.
- `chatlogexport_assets_test.go`: viewer assets are written from the
  embedded source when missing and unconditionally overwritten when
  present (agent-owned). `tools_test.go`: pinned expected-text consts
  updated for the AskUserQuestion steering directive.

## [0.7.1] — 2026-05-23

### Features
- Chat events now carry an `agent_tool_name` + `agent_tool_seq` stamp
  identifying the per-tool ordinal of the MCP call that produced
  them (`send_message`, `send_progress`, `send_verbal_reply`,
  `send_verbal_progress`, `check_messages`). Downstream consumers
  (e.g. a fork resolver) can locate the matching `tool_use_id` /
  `call_id` in the agent's own `.jsonl` rollout without resorting to
  text correlation against bubble content. Counters tick on handler
  entry so even early-return calls (e.g. voice-mode rejection) stay
  aligned with the agent-side `.jsonl` count, and are seeded from the
  on-disk event log at startup so post-restart events keep counting
  from where they left off.

### Tests
- New `stamp_test.go` covering counter increment, restart seeding,
  and stamped vs. unstamped drain paths.

## [0.7.0] — 2026-05-21

### Features
- Pending-receipt state for user bubbles: messages render dim and
  below the typing loader with an "Agent hasn't seen this yet"
  tooltip until the agent actually drains them (via `check_messages`
  or `send_message`), then flip above the loader and revert to normal
  styling. The Send button takes yolo-orange whenever the loading
  indicator is visible, signalling that the next message will queue
  behind in-progress work. `eventbus.go` carries IDs on
  `UserMessage`/`Event`; new `ReceiveUserMessage` /
  `PublishConsumedUserMessage` helpers guarantee
  publish-before-queue ordering, and `DrainMessages` /
  `WaitForMessages` emit `userMessagesConsumed` with the drained IDs.
- Unsend × control on every pending user bubble. Click withdraws the
  message before the agent reads it: the server atomically
  drain-filter-requeues the queue and broadcasts
  `userMessageDeleted` so every tab removes the bubble; the agent's
  next `check_messages` never sees it. Consumed bubbles do not
  expose × — once the model has read the text, "unsend" would be
  misleading. History replay builds a deleted-IDs set and skips
  withdrawn user messages.

### Fixes
- `check_messages` empty-queue results now return the machine-readable
  `{"queue":"empty"}` prefix plus explicit guidance that agents must
  not send a user-visible reply merely to report an empty queue. The
  shared tool-result framing was refactored so barge-in messages are
  appended consistently, including on progress tools, reducing the
  need for defensive polling between steps.
- The iframe bootstrap nudge (`check_messages; reply me with a
  send_message`) is persisted in `sessionStorage`, so reloads do not
  re-type the nudge after a real user message has already been
  delivered. `/clear` resets the persisted flag for a fresh session.
- Frozen quick-reply chips now anchor immediately after the agent
  bubble that created them, even when pending user bubbles or the
  loading indicator are present. This keeps stale/unused reply chips
  visually associated with the correct agent turn.

### Tests
- New `e2e/markdown-images.spec.cjs` drives client-side
  `renderMarkdown()` via Playwright to cover `![alt](url)`, empty
  alt, relative paths, `javascript:` URL rejection, plain-link
  regression, and mixed image+link. Companion visual side-by-side
  bubble screenshot spec (`markdown-images-visual.spec.cjs`) is
  skipped from the default suite; run manually.
- Each Playwright test now runs in its own isolated
  `browser.newContext().newPage()` instead of reusing `pages[0]`,
  eliminating cross-test state bleed (stale autocomplete dropdowns,
  leftover navigations, `ERR_ABORTED` first-of-describe failures)
  that produced 0–4 intermittent failures per run. Trade-off: tests
  no longer ride in the pre-existing Agent View tab.
- `@xyz` autocomplete debounce race fixed by switching the
  no-result-response lookup from `.find()` to `.findLast()`, so the
  final query's response is asserted against even when an
  intermediate query (e.g. just `x`) slips through debounce
  coalescing under CPU pressure.

### Docs
- ADR documenting the pending-message lifecycle: queued, consumed,
  deleted, replayed, and how quick replies relate to pending user
  bubbles.
- Exported chat session capturing the TDD-driven implementation of
  the pending-receipt UX and the unsend × control.
- Exported chat session covering the empty-queue guidance and frozen
  quick-reply placement fixes.

## [0.6.0] — 2026-05-03

### Features
- New MCP tool `export_chat_md(title, target_dir?)` replaces
  `export_chat_html`: server writes a script-style markdown file
  (`**USER**` / `**AGENT**` markers with `> `-blockquoted bodies) to
  `./agent-chats/YYYY-MM-DD-NN-{title}.md`, copies user-attached
  images to `./agent-chats/assets/`, and upserts an `index.html`
  archive landing page that re-renders each chat as speech bubbles
  matching the `[download chat]` HTML look. NN is a per-day index.
- Each agent turn carries an elapsed-time prefix
  (`<small>took 26.5s</small><br>`) computed against the previous
  bubble's timestamp, plus a trailing `[Quick replies]` bullet block
  when the original `send_message` supplied one.
- User-attached image attachments live inside the user blockquote,
  wrapped in a flex `<div>` so md-serve / our viewer tile them
  three-up; each thumbnail is `<a href>`-wrapped for click-to-open
  and middle-click-to-new-tab. GitHub's HTML sanitiser strips the
  inline `style=...`, gracefully degrading to one-image-per-row.
- Bundled chat-archive viewer in `chatlog-viewer/` (embedded via
  `//go:embed`, written on first export). The viewer auto-detects
  legacy table-format exports and the new heading-marker format,
  and renders both as bubbles. Markdown fetches use
  `Accept: text/markdown, text/plain` so md-serve 0.4.0+ returns
  raw bytes via content negotiation.

### Fixes
- Parser hardened with `(?!>)` lookaheads on the turn-marker and
  elapsed-time regexes — turn detection is now blockquote-aware by
  construction, so literal `**USER**` / `<small>took …</small>`
  strings inside chat content can never false-trigger a split.
- Image flex layout now applies to the `<a>` link wrapping each
  thumbnail (the actual flex item) instead of the inner `<img>`,
  fixing the regression where attachments stacked one per row.

### Docs
- `agent-chats/index.html` is the live, browseable archive of every
  chat exported through `export_chat_md`, with a sidebar filter and
  a raw `.md ↗` link per chat.

## [0.5.0] — 2026-04-25

### Features
- New MCP tool `export_chat_html(title, target_path?, image_mode?)`:
  ask a connected browser to render the current chat as a
  self-contained HTML file (uploaded images inlined as base64 data
  URIs) and have the server write it to disk. Default location:
  `./agent-chats/YYYY-MM-DD-{title}.html`, auto-suffixed `-2`/`-3`
  on same-day collision.
- `image_mode` controls image fidelity: `fullsize` (default) keeps
  the original bytes and makes thumbnails clickable in the export
  to open in an in-page lightbox overlay (data: URIs can't be
  navigated to as a top-frame in modern browsers); `thumbnail`
  downsamples each image to a small JPEG via canvas for a compact
  archive.
- Non-image attachments render as plain filename text in exports
  (their `/uploads/*` href is dropped because it won't resolve
  outside the server).
- Top-right download button now also inlines `/uploads/*` images so
  the saved HTML is portable outside the agent-chat server.

### Docs
- ADR for the export feature, including the transient-broadcast bus
  channel that delivers exportRequest without polluting the event log.

## [0.1.15] — 2026-03-14

### Features
- Built-in emoji autocomplete via `:` trigger (1,560 emojis with multi-keyword fuzzy search)
- `replace_trigger` response field: providers can control whether the trigger character is kept or removed on selection
- Auto-detect Chrome CDP endpoint from `BROWSER_CDP_PORT` env var for E2E tests

### Fixes
- Handle object results `{v, h}` in E2E autocomplete response assertion

### Docs
- ADR for `replace_trigger` and built-in emoji autocomplete
- Document `replace_trigger` in autocomplete API reference

### Tests
- Unit tests for emoji handler (match, empty query, no match) and `replace_trigger` passthrough
- E2E tests for emoji selection (trigger removed) and filepath selection (trigger kept)

## [0.1.14] — 2026-03-13

### Features
- Show amber warning bubble when not in iframe, prompting user to type `check_messages`
- Update nudge text to "reply me with a send_message"

## [0.1.12] — 2026-03-11

### Fixes
- Merge default `@=filepath` trigger with custom `--autocomplete-triggers` instead of replacing it

## [0.1.11] — 2026-03-11

### Features
- Add agent-chat branding link to Connected system message

## [0.1.10] — 2026-03-11

### Features
- Pass through `has_more` from external autocomplete providers; update provider contract docs

## [0.1.9] — 2026-03-11

### Features
- Replace beep tones with spoken "Be right back" for voice progress updates
- Preview TTS voice on selection and persist choice in localStorage
- Distinct beep tones for active vs passive listening (superseded by spoken brb)
- Score and rank autocomplete results by match quality
- Add `has_more` flag to autocomplete API; skip client cache when results are truncated

### Fixes
- TTS cutoff on iOS: proportional safety timeout and numbered-list protection
- Prefer local build over npm-installed platform binary

### Docs
- Add 14+ ADRs documenting past architectural decisions
- Add ADR for spoken brb, supersede beep tone ADRs

### Refactor
- Split Makefile test targets into unit-test, e2e-test, e2e-report

### Tests
- Use @dom fuzzy query in E2E autocomplete happy path
- Add no-results scenario to E2E autocomplete suite

## [0.1.8] — 2026-03-01

### Features
- Add per-bubble TTS play button to agent messages
- Trigger-based autocomplete with external provider proxy
- Per-trigger URLs and built-in @filepath autocomplete
- Structured autocomplete response with debug info

### Fixes
- Remind agent to use chat tools after check_messages returns empty
- Include nudge text in interrupt postMessage and support standalone mode
- Nudge agent toward send_message when task is done
- Collapse repeated system messages into counter
- Eager file upload on selection to prevent silent attachment drops
- Show loading and no-results states in autocomplete dropdown
- Filepath autocomplete root-skip and empty-cache bugs

### Docs
- Add autocomplete API reference

### Tests
- Add Playwright E2E test for @filepath autocomplete

## [0.1.7] — 2026-03-01

### Fixes
- Handle loose markdown lists (blank lines between items)

### Tests
- Add loose list cases to markdown torture test

## [0.1.6] — 2026-02-27

### Features
- Show version stamp in chat on connect
- Show version mismatch between server and page
- Voice interrupt — stop/cancel phrases send Esc-Esc to parent PTY
- Add /mcp/orchestrator endpoint for external chat interaction
- Persist unchosen quick replies in chat log
- Extend interrupt detection to typed messages
- Rewrite reply-instructions template with confirmation checklist
- Ask one question at a time in voice mode
- Tell agent not to ask questions in the TUI

### Fixes
- Update test expectations to match current reply-instructions template
- Inline config.js to prevent dark-mode stuck when proxied
- Send historyEnd event after reconnect replay
- Strengthen one-question-per-message rule in voice mode

### Refactor
- Extract message formatting to embedded templates
- Rename voice-suffix template to reply-instructions
- Simplify reply instructions to express intent not steps
- Rename push_message to send_chat_message for consistency

## [0.1.5] — 2026-02-25

### Features
- Broadcast user messages to all browsers and add cursor-based event sync
- Improve markdown rendering and UX polish
- Add blockquote rendering with nested quote support
- Reload event log from disk on server restart
- Track quick_replies state for browser reconnect and strip stale replies
- Show WebSocket connect/disconnect as system messages in chat

### Fixes
- Remove optimistic display from quick reply handler to prevent duplicate bubbles
- Resolve three markdown rendering bugs
- Adjust code block font size and use CSS vars for code backgrounds
- Use readOnly instead of disabled to preserve focus while sending
- Align messages to bottom of chat when few are present
- Scroll to bottom on user message
- Move quick-replies into message flow for inline display

### Docs
- Add README and www/ landing page with screenshots

## [0.1.4] — 2026-02-23

### Features
- Add file upload with drag-drop, thumbnails, and agent file paths
- Add voice mode with STT/TTS and send_verbal_reply tool
- Add send_verbal_progress tool, quick_replies to verbal reply, and iOS TTS fix
- Add speech-detection blink indicator and harden voice mode
- Add image_urls to send tools, timestamps on events, and STT warning
- Elapsed time labels, TTS queue, voice styling, and export improvements
- Add check_messages reminder to all message responses
- Reject send_message when user is in voice mode

### Fixes
- Expand file drop zone to cover entire window
- Voice messages now trigger parent frame notification and show clear STT context
- Let Enter insert newline on mobile, send only via button
- Make speech confirmation prompt explicitly ask for yes/no
- Move voice dropdown to header row and loading indicator below messages
- Split TTS into sentence-sized chunks to avoid iOS truncation

### Refactor
- Simplify loading indicator and quick replies logic
- Simplify voice mode warmup to just "Ready"

## [0.1.3] — 2026-02-22

### Features
- Persist events to JSONL and add playback mode
- Add light/dark theme support, syntax highlighting, and table rendering
- Add -v flag with version and git commit SHA
- Add npm distribution via npx @choonkeat/agent-chat

### Fixes
- Fix link contrast and duplicate first message on reload
- Fix npx execute permission issue with postinstall chmod
- Fix make bump to update optionalDependencies versions

## [0.1.0] — 2026-02-09

### Features
- Initial release: MCP-based chat UI with WebSocket
- Send/receive messages with quick reply chips
- Non-blocking check_messages polling
- Inline canvas drawing with Rough.js hand-drawn rendering
- Lightweight markdown rendering
- Auto-growing textarea input
- Send_progress tool with animated thinking dots
