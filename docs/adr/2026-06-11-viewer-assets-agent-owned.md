# Exported viewer assets are agent-chat-owned and overwritten every export

**Date:** 2026-06-11
**Status:** Accepted

## Context

`export_chat_md` writes a self-contained chat archive into a directory: the
`.md` turn logs, an `index.html` shell, and two static viewer assets —
`viewer.css` and `viewer.js` — both embedded into the binary via
`//go:embed` and copied out by `ensureViewerAssets()`.

The original `ensureViewerAssets()` was **write-once**: if a file already
existed it was skipped, on the rationale that a user might patch the served
copy and we should not clobber their edits.

That rationale has a cost. When we *fix* a bundled asset — e.g. adding the
missing `.bubble blockquote`, `.bubble table`, `.bubble hr`, and
`.bubble h4–h6` rules to `viewer.css` (these elements are emitted by
`marked.parse` but were unstyled, so quoted text and GFM tables rendered
broken) — **no existing archive ever picks up the fix.** Every previously
exported directory keeps the stale CSS until the user manually deletes it.

We briefly considered a middle path (hash-stamp each file, refresh pristine
copies, preserve hand-edits) but rejected it: `viewer.css` / `viewer.js` are
*our* presentation layer, not a user document. The right place to customize
them is the embedded source, not a served copy that silently forks.

## Decision

`viewer.css` and `viewer.js` are **agent-chat-owned**. `ensureViewerAssets`
overwrites them with the embedded version on **every** export. They are never
treated as user-owned and are never preserved.

A user who wants custom viewer styling edits the embedded source under
`chatlog-viewer/assets/` and rebuilds; patching the served copy is not
supported — it will be clobbered on the next export.

`index.html` is **out of scope** and keeps its create-if-missing behavior:
unlike the static assets it is *mutated in place* by `upsertIndexHTML` (manifest
entries are spliced into it), so its on-disk body legitimately diverges from
any embedded template and must not be blindly overwritten.

## Consequences

- Bundled CSS/JS fixes propagate to **every** archive on its next export, with
  no manual intervention — the blockquote/table fixes included.
- Any hand-edit to a served `viewer.css` / `viewer.js` is lost on the next
  export. This is intentional and now documented.
- No stamping, hashing, or content-comparison machinery — `ensureViewerAssets`
  is a plain embedded-file copy loop.
