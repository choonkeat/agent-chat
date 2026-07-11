# Changelog

All notable changes to agent-chat are documented in this file.

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
