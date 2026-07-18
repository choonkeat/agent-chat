# Streaming chat-log auto-export (append-as-it-goes markdown + assets)

## Goal

Make the markdown chat archive (`agent-chats/*.md` + `assets/` + `index.html`)
write itself automatically, the way the JSONL event log already does — no
explicit `export_chat_md` call needed. When `AGENT_CHAT_EXPORT_DIR` is set:

1. **Event time (append):** each renderable event is appended to the session's
   .md file the moment it hits the bus; its attachments are copied into
   `assets/` at that same moment (while the upload files still exist —
   this kills the "skipped missing attachment" rot for good).
2. **Turn end (rename + index):** debounced housekeeping only — rename the
   file if the title changed (implies a header rewrite), and regenerate
   `index.html`.
3. **Session end:** nothing that isn't already on disk. SIGTERM just flushes.

This is a standalone agent-chat feature: any user gets a self-updating chat
archive in any repo by setting one env var. swe-swe's integration (separate
repo, separate task) is only "default the env var to `{workDir}/agent-chats`
plus an opt-out toggle". The recordings-dir JSONL (`AGENT_CHAT_EVENT_LOG`)
stays exactly where it is — it remains the canonical machine log that
swe-swe's resume/fork/playback/summaries read. The .md is a second, curated
output, not a replacement.

## Background (current behavior — read before changing)

- `main.go:227` — `AGENT_CHAT_EVENT_LOG` env var: when set, `NewEventBusWithLog`
  appends every event to a JSONL file. This is the model to imitate.
- `chatlogexport.go` — `runChatMarkdownExport(rootDir, slug, events, ...)` is a
  **batch** exporter: mints a new `YYYY-MM-DD-NN-{slug}.md` per call
  (`nextDailyIndex`), copies attachments (`writeImageAttachments`, content-sha
  filenames), refreshes `viewer.css`/`viewer.js`, and **prepends** a manifest
  entry into `index.html` (`upsertIndexHTML` + `manifestOpenRE`).
- `renderChatMarkdown` is a **pure left-fold** over events: a bubble's markdown
  depends only on the event itself plus two pieces of carried state — `lastTs`
  (the `<small>took Ns</small>` line compares the current event's timestamp to
  the previous bubble's; never looks ahead) and the per-export asset counter
  `n`. No coalescing. This is what makes append-mode structurally safe.
- `tools.go` `export_chat_md` — the agent supplies `title` (slugified) and
  optional `target_dir` (must stay inside cwd). This is the only way a title
  reaches the exporter today.

## Design decisions (confirmed with user 2026-07-18)

- **Append-as-it-goes**, not re-export-per-turn: the .md is written like the
  JSONL. Turn-end does *only* rename + index regeneration. (User's explicit
  requirement.)
- **`AGENT_CHAT_EXPORT_DIR` env var** enables the feature; unset = today's
  behavior, no new files. Path resolved relative to cwd at boot; same
  cannot-escape-cwd check as `target_dir` — with one deliberate difference: an
  absolute path outside cwd is a fatal misconfiguration warning + feature off,
  not a crash.
- **`index.html` becomes regenerated, not upserted**: derive the whole MANIFEST
  from a glob of `*.md` in the export dir (filename encodes date/idx/slug;
  title read from the file's header comment, falling back to
  `humanTitle(slug)`). Idempotent, so git merge conflicts get the golden-file
  treatment: accept either side, next regeneration self-heals. This changes
  the *manual* `export_chat_md` path too (shared code) — that is intended.
- **Duplicate daily NNs are accepted** (two branches' sessions can both claim
  `-02-`; assets are content-sha named so nothing clobbers; the regenerated
  index lists both). No session-id in the filename.
- **Provisional filename at boot**: `YYYY-MM-DD-NN-untitled.md` (NN claimed at
  file creation). Renamed when a title becomes known/changes.
- **Title arrives via a new `set_chat_title` tool** (agent calls it once, may
  call again to rename). Keeps agent-chat standalone rather than depending on
  a host forwarding session names. `export_chat_md` stays as the manual
  escape hatch (custom target_dir, forced full export) — demoted, not removed.
- **Rename implies header rewrite**: the title is baked in three places
  (HTML-comment header, `# H1`, byline), so a title change does a one-shot
  full rewrite from in-memory history, then returns to pure append. Rare and
  cheap; steady state is append-only.
- **`<!-- session: {uuid} -->`-style identity lives in the header comment** (a
  `session:` line) so a resumed/forked process can find and continue its file
  instead of minting a new NN.
- **Never auto-commit.** Nothing in agent-chat (or swe-swe) touches git; the
  export sits in the working tree and committing stays with the agent/user.
- **Opt-out is conversational**: a `chatlog_optout` tool stops the streaming
  export for this session and deletes its .md (assets left alone — content-sha
  names mean other sessions may share them; orphans are harmless) and
  regenerates index.html. Re-enable by calling `set_chat_title` again.

## Non-goals

- No change to `AGENT_CHAT_EVENT_LOG` / JSONL semantics.
- No swe-swe changes in this task (env-var default + Settings toggle is a
  follow-up in the swe-swe repo).
- No backfill CLI (jsonl → md for historical recordings) — separate follow-up.
- No client (`client-dist/`, `app.js`) changes.

## Architecture sketch

New `chatlogstream.go`: a `chatLogStream` struct owned by main, subscribed to
the event bus (same tap point as the JSONL logger so ordering matches):

```
type chatLogStream struct {
    dir       string   // export dir (abs)
    mdPath    string   // current file (provisional until titled)
    slug      string   // "" until set_chat_title
    date, idx string   // claimed at creation
    lastTs    int64    // renderer carry-state
    assetN    int      // renderer carry-state
    f         *os.File // O_APPEND handle
    mu        sync.Mutex
    stopped   bool     // chatlog_optout
}
```

- On each bus event (`userMessage` / `agentMessage` / `verbalReply`; all other
  types ignored): copy attachments → render ONE bubble via a new
  `renderChatBubble(e, &state, imageMap)` → append + (optionally) sync.
- `renderChatMarkdown` is refactored to call `renderChatBubble` in its loop so
  batch export and streaming share one renderer (single source of truth; the
  batch path's output must be byte-identical to before).
- Turn-end signal: reuse whatever marks "agent turn completed" on the bus
  (the send_message tool returning / bus idle debounce ~2s) → if dirty:
  rename-if-needed + `regenerateIndexHTML(dir)`.
- SIGTERM/SIGINT (existing signal handler, main.go:244): flush + close file +
  final index regeneration before exit.

## TDD steps

Work test-first. After **every** step run `make test` (per CLAUDE.md — never
`go test`/`go vet` directly). Red → green → refactor. E2E: warm the lazy CDP
endpoint first (see CLAUDE.md) — though this task should need unit tests only.

### Step 1 — Extract per-bubble renderer (pure refactor)
- **Test (red):** `TestRenderChatBubbleMatchesBatch` — for a fixture event
  list, fold `renderChatBubble` over the events and assert the concatenation
  (after the header) is byte-identical to `renderChatMarkdown`'s output.
- **Impl (green):** extract `renderChatBubble(e Event, st *renderState,
  imageMap map[string]string) string` with `renderState{lastTs int64}`;
  `renderChatMarkdown` loops over it. Existing export tests stay green
  untouched — that's the proof the refactor is pure.

### Step 2 — Regenerated (not upserted) index.html
- **Test (red):** `TestRegenerateIndexHTML` — dir with several `*.md` files
  (some with `title:` header lines, one without → falls back to
  `humanTitle(slug)`); assert MANIFEST lists all, newest first, and that
  running it twice is byte-identical (idempotent). `TestRegenerateIndexHTMLHealsConflict`
  — corrupt/merge-marker index.html gets fully rewritten, not patched.
- **Impl (green):** `regenerateIndexHTML(dir)` globs `*.md`, parses
  `date/idx/slug` from filenames and `title:`/`session:` from the header
  comment; rewrites the MANIFEST block (or whole file from the embedded
  template). Replace `upsertIndexHTML` calls; delete `manifestOpenRE` upsert
  path once nothing references it.

### Step 3 — Streaming writer core
- **Test (red):** `TestChatLogStreamAppends` — feed events one at a time;
  after each, the .md on disk parses as valid markdown and equals the batch
  render of the events-so-far (reuse Step 1's equivalence). Include an
  attachment event: asset file exists on disk *immediately* (not at
  turn-end), sha-named. `TestChatLogStreamSkipsHiddenEvents` — toolMarker
  etc. produce zero writes.
- **Impl (green):** `chatlogstream.go` per the sketch; provisional filename
  `{date}-{NN}-untitled.md`; header written at file creation with a
  `session:` line; `writeImageAttachments` logic reused per-event (extract a
  single-event helper).

### Step 4 — Title, rename, resume
- **Test (red):** `TestChatLogStreamRename` — set title after N events: file
  renamed, header (comment + H1 + byline) rewritten, body bubbles unchanged,
  subsequent events append to the renamed file. `TestChatLogStreamResume` —
  a new stream pointed at a dir containing a file with matching `session:`
  continues that file (correct `lastTs`/`assetN` recovered by re-folding the
  in-memory history, NOT by parsing the md) instead of minting a new NN.
- **Impl (green):** `set_chat_title` MCP tool (slugified like
  `export_chat_md`); rename = full rewrite from `bus.History()` then back to
  append; resume scan on stream init.

### Step 5 — Wiring: env var, turn-end debounce, SIGTERM, opt-out
- **Test (red):** `TestChatLogStreamEnvDisabled` — env unset → no dir, no
  files. `TestChatLogStreamEscapesCwd` — dir outside cwd → warning + feature
  off. `TestChatLogOptout` — tool stops appends, deletes .md, regenerates
  index; `set_chat_title` re-arms. Turn-end: index.html updated after the
  debounce, not per-event.
- **Impl (green):** read `AGENT_CHAT_EXPORT_DIR` in main; subscribe stream to
  bus; debounce turn-end (bus-idle or send_message completion); flush on the
  existing signal handler; add `chatlog_optout` tool; update `export_chat_md`
  description to mention auto mode.

### Step 6 — Full sweep + docs
- `make test` green (unit + e2e; warm CDP first).
- README + CHANGELOG: document `AGENT_CHAT_EXPORT_DIR`, `set_chat_title`,
  `chatlog_optout`, and the regenerated index.html (merge-conflict guidance:
  accept either side, next export heals).
- Manual smoke: run agent-chat locally with the env var set, chat a few
  turns with an image attachment, verify the .md grows per-event and
  index.html renders in the viewer.

## Follow-ups (out of scope, tracked for later)

- **swe-swe repo**: default `AGENT_CHAT_EXPORT_DIR={workDir}/agent-chats` at
  session spawn + per-session opt-out toggle in Session Settings; seed agent
  guidance to include `agent-chats/` when committing (never auto-commit).
- **Backfill CLI**: render historical `.events.jsonl` recordings to md
  (attachments best-effort — many upload files will be gone).
- **Homepage `Chat` listing**: unaffected by this task (JSONL untouched);
  revisit only if the JSONL location ever changes.
