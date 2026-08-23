# Clipboard / paste / drop edge cases — 13 filed findings

Filed 2026-08-25. **Nothing implemented yet.** All 13 are audit findings from
two research passes; every line reference below was re-verified against HEAD
(`5d28938`) at filing time.

Origin symptom: *"Sometimes I copy things and paste it is empty."*

The whole surface is three places:

| | file | what it does |
|---|---|---|
| paste | `client-dist/app.js:1230-1284` | the `paste` listener on `chatInput` |
| drop | `client-dist/app.js:1194-1200` | the `drop` listener on `dropZone` |
| staging | `client-dist/app.js:976-993` | `addStagedFiles`, shared by both |

Server side: `main.go:500` `handleUpload`, `main.go:614` `saveUploadedFile`.

---

## Ranked: do now

Ordered by how likely each is to be the reported "pasted and it's empty",
cheapest-first within a tier. **P1-P4 together are ~1h40m and cover the whole
"empty" symptom.**

### P1 — Paste only works while the textarea has focus *(~20 min)*

`client-dist/app.js:1230` binds `paste` to `chatInput` only. Click a transcript
bubble (or anywhere outside the composer), then Cmd/Ctrl+V → nothing happens.
No chip, no error, no text. This is the most likely everyday cause and it is
not a clipboard-format problem at all.

**Fix:** move the listener to `document`, guard on "focus is not in another
editable/input element", and route through the same handler. Keep
`e.preventDefault()` semantics identical.

**Watch out:** don't hijack paste when the user is in the file-rename input or
any future text field; check `document.activeElement`.

---

### P2 — Drop has none of the paste protections *(~30 min)*

`client-dist/app.js:1194` reads only `e.dataTransfer.files` and calls
`addStagedFiles`. Everything the paste path learned is missing here:

- Drop a **folder** → Chrome hands over a 0-byte `File` → uploads empty, chip
  looks completely normal. (See also P4.)
- Drop a **link or selected text** → `files.length === 0` → the handler does
  nothing at all. Not even the failed chip that paste would show.

**Fix:** extract the paste body into a shared `handleTransfer(dataTransfer)`
and call it from both listeners. Drop then inherits the failed chip, the
zero-byte guard, and the text handling for free.

---

### P3 — We only read `text/plain` *(~15 min)*

`client-dist/app.js:1244` — `cd.getData('text/plain') || ''`. There is no
fallback to `text/html`, `text/rtf`, or `text/uri-list`. An app that offers
rich text or a URL *without* a plain-text flavour gives us `text === ''`, which
falls into the `files.length === 0 && text.length === 0` branch at
`app.js:1246-1250` → the "clipboard-empty" failed chip.

**Fix:** fall back in order `text/plain` → `text/uri-list` → strip-tags of
`text/html` → `text/rtf`. Insert the recovered text rather than showing the
failed chip.

---

### P4 — Zero-byte files upload silently *(~15 min)*

No `f.size === 0` guard anywhere in the staging path (`app.js:976`). The chip
renders normal, the upload succeeds, and the agent receives 0 bytes. This is
literally "I pasted and it's empty" with no error anywhere. Common with iOS
share-sheet handoffs, Windows Explorer, and dropped folders (P2).

**Fix:** in `addStagedFiles`, when `file.size === 0`, stage a failed chip
(`addFailedPasteChip`, `app.js:1000`) instead of starting an upload. Name it so
the reason is visible, e.g. `photo.heic (empty)`.

---

### P5 — Rich text + inline image loses the image *(~30 min)*

`client-dist/app.js:1279` — `if (text.trim().length > 0) return;` — returns as
soon as text is non-empty, so the files collected just above are discarded.
Paste a paragraph-with-screenshot from Word / Google Docs / Slack and the text
lands while the image vanishes silently.

**Fix:** when both are present, stage the file(s) **and** let the text insert.
Guard against the Excel/Sheets case where the "image" is just a snapshot of the
copied cells (see P7) — a heuristic on `files.length === 1 && type === image/png
&& text looks like TSV` is the distinguishing signal.

---

### P6 — HEIC previews render blank *(~30 min)*

iPhone photos arrive as `image/heic`. `app.js:983` does
`URL.createObjectURL(file)` for anything with an `image/` type, but Chrome
cannot decode HEIC, so the `<img>` renders nothing. The chip looks empty even
though the upload actually succeeded.

**Fix:** exclude `image/heic` / `image/heif` from `isImage` preview generation
and show a named file chip instead. Optionally add an `onerror` on the preview
`<img>` that falls back to the file-chip rendering — that also covers any other
codec the browser refuses.

---

### P7 — Big Excel pastes bypass the 30-line rule *(~15 min)*

The stage-as-`.txt` rule at `app.js:1257` sits **inside** the
`files.length === 0` branch, and `app.js:1279` returns early whenever text is
present. Excel ships an `image/png` snapshot alongside the TSV, so
`files.length > 0` and a 5000-row paste floods the composer instead of becoming
an attachment.

**Fix:** hoist the `PASTE_AS_FILE_MIN_LINES` check above the `files.length`
branch so it applies to every paste that carries text. Overlaps P5 — do them
together.

---

### P8 — The 30-line rule is line-count-only *(~15 min)*

`app.js:1207-1216`. Two holes:

- A single-line 2 MB minified JSON or base64 blob is **1 line** → dumped
  straight into the composer.
- `countLines` splits on `\n` only, so `\r`-only line endings (old Mac apps,
  some Java and terminal output) count the whole paste as 1 line and a 5000-row
  paste bypasses the rule entirely.

**Fix:** add a byte-length threshold alongside the line count (e.g. stage as
`.txt` above ~8 KB regardless of lines), and normalise `\r\n` and `\r` to `\n`
before counting.

---

### P9 — Whitespace-only paste is invisible *(~10 min)*

Copy blank Excel cells and the clipboard is `"\t\t\r\n"`. The empty-paste guard
at `app.js:1248` only fires on `text.length === 0`, so whitespace passes
straight through and inserts something you cannot see. (Note the `files.length
> 0` path at `app.js:1279` already uses `.trim()`; the no-files path does not.)

**Fix:** use `text.trim().length === 0` in the guard at `app.js:1248` and show
the failed chip named "clipboard-empty" as it does today.

---

## Later — fidelity, not emptiness

### P10 — Web images copied as `text/html` `<img src>` with no file *(~45 min)*

Some Firefox and webview variants put only an `<img src="https://...">` in
`text/html` with no `File` attached → `files.length === 0` → failed chip. We
never read the `src`. Fix would fetch the URL and stage the result; note CORS
will block a share of these, so failure has to stay graceful.

### P11 — `File.name` can be empty *(~15 min)*

`main.go:622` — `savedName := prefix + "-" + fh.Filename`. An empty filename
saves as `abcd1234-` with no extension, which nothing downstream can open by
type. Fix on the client (synthesise `pasted-<timestamp>.<ext-from-mime>`) and
defensively on the server.

### P12 — No client-side 50 MB precheck *(~20 min)*

The server caps the body at `main.go:507` (`50<<20`). The client never checks,
so a large video uploads in full and *then* fails. Fix: check `file.size`
against the same limit in `addStagedFiles` and fail the chip immediately with a
clear name.

### P13 — Table structure is thrown away *(half a day)*

Excel and Google Sheets both put a real HTML `<table>` in `text/html`. We never
read it, so the agent receives tab-separated soup with no column alignment and
no cell-boundary information. Fix: convert the `text/html` table to a markdown
table when the paste is staged as `.txt`. Depends on P3 (reading `text/html` at
all).

---

## Suggested order for a fresh agent

1. **P1** alone — highest hit rate, one listener move, ~20 min.
2. **P2 + P4** — extract the shared handler, add the zero-byte guard. Drop and
   paste converge here; do them in one pass.
3. **P3 + P9** — both are edits to the same 5-line guard block.
4. **P5 + P7** — both restructure the `files.length` / text branch at
   `app.js:1257-1284`; doing them separately means touching it twice.
5. **P6**, then **P8**.
6. Later tier (P10-P13) only on demand.

## Verification

There is no paste/drop E2E coverage today. `make test` is the gate (never bare
`go test` / `go vet`), and E2E needs CDP warmed first — see `CLAUDE.md`.

Playwright can drive both paths without a real OS clipboard:

- **paste**: `page.dispatchEvent(sel, 'paste', { clipboardData: ... })` with a
  synthesised `DataTransfer` built in `browser_evaluate`.
- **drop**: same trick with a `drop` event and `dataTransfer.items.add(...)`.
- **zero-byte** (P4): `new File([], 'empty.png', {type:'image/png'})`.
- **no-plain-text** (P3): a `DataTransfer` carrying only `text/html`.
- **focus** (P1): click a transcript bubble first, then dispatch the paste on
  `document`.

Remember `client-dist/` is `//go:embed`-ed — rebuild and restart before any
manual check:

```
GOOS=linux GOARCH=amd64 go build -o npm-platforms/linux-x64/bin/agent-chat .
```
