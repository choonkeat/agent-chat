# Export chat as self-contained HTML via browser-rendered DOM

**Date:** 2026-04-25
**Status:** Accepted

## Context

Users wanted a way to archive a chat — including inline screenshots —
without having to manually save the page or screenshot the UI.  Two
existing surfaces were close but not enough:

1. The top-right `btn-download` button serialised `messages.children`
   into an HTML blob and triggered a browser download.  But the
   serialised HTML kept relative `/uploads/...` `<img src>` references,
   so the export only rendered correctly while the agent-chat server
   was running.
2. The JSONL event log on disk held everything an offline render would
   need, but no consumer existed and any server-side renderer would
   diverge from the live `marked.js` + DOM render the user actually
   sees.

The user also wanted a way to trigger this from the agent itself
(via MCP), so a chat could be auto-archived at the end of a session.

## Decision

Add an MCP tool `export_chat_html` that asks a connected browser to
build a self-contained HTML export and POST the bytes back to the
server, which writes them to disk under a date-prefixed default path.

The render is intentionally browser-side:

- The DOM is the source of truth — anything visible to the user
  appears in the export, and edge cases like canvas drawings and TTS
  buttons just work because the same code paths produce both views.
- We avoid maintaining a server-side markdown renderer that has to
  track `marked.js` GFM extensions, sanitisation, syntax highlighting
  and code-block styles in lockstep.
- The same `buildExportHtml()` powers both the user-facing download
  button and the agent-driven export, so they cannot drift.

To make the export self-contained, every `<img>` whose src is not
already a `data:` URI is fetched and rewritten to a base64 data URI
before serialisation.  Bubbles are cloned before rewriting so the live
DOM is not bloated with multi-MB inline images.

## Server ↔ browser protocol

```
agent ──► MCP: export_chat_html(title, target_path?)
              │
              ▼
agent-chat: bus.CreateExport() → token
            bus.PublishTransient({type: "exportRequest", token})
              │  (non-logged WS broadcast — see below)
              ▼
browser: handleExportRequest(token)
         buildExportHtml() // walks DOM, inlines images
         fetch POST /api/export?token=...  (HTML body)
              │
              ▼
agent-chat: handleExport → bus.ResolveExport(token, html)
            agent-side: write to {target or default path}, return summary
```

A separate **transient** broadcast channel was added to the EventBus
(`SubscribeTransient`/`PublishTransient`).  Logged events on the main
bus are persisted to JSONL and replayed on browser reconnect — that
is the wrong shape for an exportRequest, which is per-call and would
fire spuriously on every reconnect if it were logged.  Transient
broadcasts share the same per-connection `writeCh` used by the WS
handler but skip the event log entirely.

`pendingExports` mirrors the existing `pending` ack map: the tool
handler creates a token, fires the request, and blocks on a channel
until the browser resolves it (or the timeout / context expires).

## Path convention and safety

Default output path: `./agent-chats/YYYY-MM-DD-{slug}.html` where
`{slug}` is the sanitised `title` parameter.  Three rules:

1. **Title is required** in the default flow.  The agent must commit
   to a short kebab-case label (e.g. `auth-bug-fix`).  This is cheap
   for the model and gives every export a human-meaningful name.
2. **Server sanitises** the title (`slugifyTitle`): lowercase, every
   non-`[a-z0-9]` run collapsed to a single `-`, trailing dashes
   trimmed.  The agent cannot smuggle path separators or unicode
   surprises into the filename.
3. **Collision suffixes**: if the file already exists, the server
   tries `…-2.html`, `…-3.html`, etc.  Silent overwrite was rejected
   — losing an earlier export to a same-day same-title collision is
   the wrong default.

The optional `target_path` override is validated with
`filepath.Rel(cwd, target)`: any path that escapes the current
working directory is refused.  This is the only path-safety boundary
the tool enforces — anything inside cwd is allowed because the agent
already has full filesystem access in that directory through other
tools.

## Failure modes

- **No browser connected** — `TransientSubscriberCount() == 0` → fail
  fast with a clear error rather than waiting out the 60-second
  timeout.
- **Browser render error** — the client posts to
  `/api/export?token=…&error=1` with the error message as the body,
  so the agent receives a meaningful failure instead of a generic
  timeout.
- **Multiple browser tabs** — every tab receives the request and
  races to POST.  The first POST consumes the token; subsequent ones
  get HTTP 404 from `/api/export`.  This costs a small amount of
  duplicated rendering work but is simpler than picking a "primary"
  tab and matches the broadcast semantics already used elsewhere.

## Alternatives considered

- **Server-side markdown renderer** (e.g. `goldmark`).  Rejected for
  the maintenance burden of keeping output identical to `marked.js`,
  and because canvas drawings would need a separate rasterisation
  pipeline.
- **Client-side download with image inlining only** (no MCP tool).
  Rejected because the user wanted agents to drive the export; the
  download button still benefits from the same code path so it gets
  the inlining upgrade too.
- **Headless browser fallback when no tab is connected**.  Out of
  scope for v1 — a separate `export_chat_html_headless` could be
  added later if the use case appears.
