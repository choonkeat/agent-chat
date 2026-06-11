# Markdown chat export embeds agent-side screenshots, not just user uploads

**Date:** 2026-05-30
**Status:** Accepted

## Context

The original export tool, `export_chat_html`
(ADR [2026-04-25-export-chat-html.md](2026-04-25-export-chat-html.md)),
rendered the chat **browser-side from the live DOM**. Its explicit
principle was *"the DOM is the source of truth — anything visible to the
user appears in the export"*, and it went out of its way to inline
agent-authored artifacts (canvas drawings always full-size, screenshots,
TTS buttons). There was no user/agent distinction: both parties'
screenshots were embedded.

That tool was replaced by `export_chat_md` (commit `dff1d6d`), which
renders **server-side from the JSONL event log** — no browser round-trip.
In the rewrite, `writeImageAttachments()` was written to scan only
`userMessage` events:

```go
for _, e := range events {
    if e.Type != "userMessage" {   // ← only user turns
        continue
    }
    ...
}
```

and `renderChatMarkdown()` only called `imageBlock()` in the `userMessage`
case. Agent turns (`agentMessage` / `verbalReply`) carry a `Files` field
too — images attached via `send_message` / `send_progress` `image_urls` —
but the exporter never copied or rendered them.

Neither `dff1d6d` nor the follow-up rewrite `f0c49fe` mentions this
restriction; the commit body only says *"Image attachments copied to
assets/…"*. There is no ADR for the markdown rewrite. So the user-only
behavior was an **undocumented regression**, not a considered decision —
the move from "render the DOM" to "walk the event log" dropped agent
images by omission.

## Decision

Restore the original all-parties behavior in the markdown exporter:

- `writeImageAttachments()` now scans `userMessage`, `agentMessage`, and
  `verbalReply` turns (an explicit allowlist, so hidden bookkeeping events
  like `toolMarker` still never contribute files).
- `renderChatMarkdown()` renders an `imageBlock()` inside the **AGENT**
  blockquote, mirroring the user turn: body first, then a `>`-separated
  flex `<div>` of thumbnails. An agent turn with only an image (empty
  text) is no longer skipped.

Output for an agent turn with no attachment is byte-identical to before
(`**AGENT**\n\n> body\n\n`), so existing exports and the locked-in
elapsed-time / blockquote tests are unaffected.

## Consequences

- Agent screenshots are archived and render inline in the `.md`, in PR
  diffs, in md-serve preview, and in the bubble viewer — same asset-copy
  and relative-path mechanism as user uploads.
- The asymmetry between the HTML era and the markdown era is closed; the
  documented intent of the 2026-04-25 ADR ("anything visible to the user
  appears in the export") again holds for the shipping exporter.
- Locked in by `TestRunChatMarkdownExportEmbedsAgentImages` in
  `tools_test.go`.
