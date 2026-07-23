# The chat archive `index.html` is only rewritten at commit moments

**Date:** 2026-07-24
**Status:** Accepted

## Context

The streaming chat-log export (`AGENT_CHAT_EXPORT_DIR`) appends every bubble
to `{date}-{NN}-untitled.md` as it happens, and — until now — regenerated the
archive's `index.html` after each quiet turn (a 2s debounce in
`chatLogStream.scheduleIndexRegen`), plus once more on the SIGTERM path in
`Close()`.

`index.html` is a **tracked** file. The live `.md` is not: it is untracked
until someone commits it, and its filename is still provisional — a later
`set_chat_title` renames `…-untitled-{uuid}.md` to `…-{slug}.md`. So the
live regeneration meant:

- Every session permanently dirtied the working tree (`M agent-chats/index.html`)
  for work nobody had chosen to commit yet.
- The manifest entries it added pointed at untracked, about-to-be-renamed
  files. Committing `index.html` in that state — incidentally, or via a
  blanket stage — publishes dead links. This actually happened: the committed
  `index.html` carried an entry for an `untitled-{uuid}.md` that was never
  tracked.
- Parallel sessions (worktrees, branches) each regenerated from their own
  directory glob, so `index.html` conflicted on merge. The
  "regeneration heals a conflicted index" behaviour was papering over churn
  this feature was itself creating.

Against that, live regeneration bought exactly one thing: an in-progress
session appearing in the list if you opened `agent-chats/index.html` in a
browser mid-session.

`chatlog_close` already exists precisely to produce the commit-ready state —
it freezes the `.md`, regenerates the index once, and returns the exact paths
to `git add`. The live regeneration added nothing to that story.

## Decision

**`index.html` is rewritten only when the export set changes in a
committable way**, and **provisional exports are never listed**.

Regeneration happens at, and only at:

- `chatlog_close` — the commit moment; the export is frozen and titled.
- `chatlog_optout` — may remove an entry that is already committed.
- `export_chat_md` — an explicit, user-initiated export.
- `set_chat_title`, **only when** the pre-rename filename already appears in
  the manifest (`indexReferencesMD`). An export still private to the session
  is not in the index, so renaming it must not touch the index.

Removed: the `HandleEvent` → `scheduleIndexRegen` debounce (and the
`indexDebounce` / `indexTimer` fields), and the regeneration in `Close()`.
`Close()` now only flushes and closes the file — a session merely ending is
not a decision to publish.

`regenerateIndexHTML` additionally skips any export whose slug is provisional
(`untitled` / `untitled-{uuid}`, i.e. `isProvisionalSlug`). Those files are by
definition not commit-ready — `chatlog_close` refuses to close an untitled
export — and their names still change. Without this, a *legitimate* close in
one session would still rake in every other session's in-flight untitled file.

## Consequences

- A session in progress is not listed in `index.html` until it is closed out.
  Reading the live file directly still works; only the landing page waits.
- The working tree stays clean during a session. `git status` noise, and the
  accidental-commit-of-a-dead-link failure mode, both go away.
- Existing stale `untitled-*` entries in a committed `index.html` disappear on
  the next regeneration, since provisional exports are now filtered out.
- Merge conflicts in `index.html` become rare rather than routine; the healing
  behaviour stays as a safety net.
