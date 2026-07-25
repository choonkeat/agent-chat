<!-- agent-chat export
title: Iphone Pdf Paste Shows A Failed Chip
date: 2026-07-31
index: 01
slug: iphone-pdf-paste-shows-a-failed-chip
session: bcab031db976a3ff
agent: claude
version: 0.8.20 (05715f7)
-->

# Iphone Pdf Paste Shows A Failed Chip

_2026-07-31 · 01 · claude · agent-chat 0.8.20 (05715f7)_

**USER**

> Ok multi-line file paste works — new feature in recent commits — see attachment in this message 
>
> I want to double check that file copy paste still works? Because I tried to iPhone > Share PDF > Copy and paste here but  nothing happened (ok copy paste image in clipboard works; but the pdf didn’t)
>
> [pasted-42-lines.txt](./assets/2026-07-31-01-1-47bdbfbf86e1.txt)
>
> _[REDACTED] — screenshot of the iOS share sheet, removed before committing: it showed contact names and profile photos._

<small>took 1m 15s</small><br>
**AGENT**

> Running a live browser test now: pasting a PDF into the chat box. Result in about a minute.

<small>took 2m 25s</small><br>
**AGENT**

> **Next action: on iPhone, tap the paperclip button instead of pasting the PDF.** It opens the Files picker, and picking the PDF there works.
>
> **What I checked (all 4 steps done)**
>
> 1. Built the app fresh with the newest code.
> 2. Ran a live browser test that pastes a PDF into the chat box — the PDF attached, uploaded, and came back downloadable. So file paste is not broken.
> 3. Confirmed the app puts no limit on file type. PDF, zip, anything — nothing is filtered out.
> 4. Confirmed the new "long paste becomes a .txt file" feature only runs when the clipboard has no file in it, so it can't have swallowed your PDF.
>
> **Why the iPhone paste did nothing**
>
> When you do Share → Copy on a PDF, iOS does not hand the actual file to a web page. It puts a reference to the file on the clipboard, and web pages can't read that — so the app receives an empty paste and has nothing to attach. Images are the exception: iOS puts the actual image data on the clipboard, which is why image paste works. This is an iOS restriction, not something in the app.
>
> Two ways in on iPhone, both working today:
> - Paperclip button → pick the PDF from Files.
> - Share → Save to Files first if the PDF is inside another app, then paperclip.
>
> Separately: I can make a silent paste show a short hint like "iOS didn't share that file — use the paperclip" instead of doing nothing. Want that?

[Quick replies]
- Yes, add the hint
- No, leave it as is
- Show me the test you ran

**USER**

> Where would this notice show up? Do we have a toast mechanism?

<small>took 23.6s</small><br>
**AGENT**

> **No toast exists. Closest thing already built: `addSystemBubble()` — a small grey centred line in the chat log.** That's where I'd put the notice.
>
> Three placement options, ranked:
>
> 1. **Grey system line in the chat** (recommended) — reuses `addSystemBubble()` at `client-dist/app.js:2292`. Zero new styling. Bonus: it already auto-collapses, so if you paste five times you get "3 earlier notices" instead of five repeated lines. Looks like: `[system] iOS didn't share that file — use the paperclip button.`
> 2. **Orange warning bubble** — the `.bubble.warning` style already exists (used for "Cannot interrupt" and the connect-your-terminal hint). Louder, sits left-aligned like an agent message, does not auto-collapse. Better if you want it hard to miss.
> 3. **Line under the text box, next to the attachment chips** — no styling exists for it, so this needs new CSS and a timer to clear it. About 30 minutes. Nicest visually since it appears where you were looking.
>
> One catch worth knowing before you pick: the app can't tell "iOS blocked a file" apart from "you pasted an empty clipboard" — both look identical, an empty paste. So the notice would fire on any empty paste, including a stray Cmd-V with nothing copied. Wording has to be soft, e.g. "Nothing to paste. If that was a file, use the paperclip button."
>
> Next: say "option 1" and I'll wire it up and rebuild — about 10 minutes.

[Quick replies]
- Option 1
- Option 2
- Option 3
- Skip it

**USER**

> There’s actually a file upload failure ux— a file is there but is red. We could use that to indicate the paste happened but failed. Maybe “clipboard empty.pdf” is the filename (if we know the filename/extension even better just use it)

<small>took 1m 29s</small><br>
**AGENT**

> Step 2 of 3 done: failed-paste chip coded + 4 tests written. Rebuilding and running the tests now.

<small>took 4m 13s</small><br>
**AGENT**

> **Built and tested — see the screenshot: a red chip named after the real file, Send still works.**
>
> Step 3 of 3 done.
>
> **What happens now on an empty paste**
>
> 1. A red chip appears in the attachment row, same red as a failed upload.
> 2. The name is the real filename when the clipboard says one (`Scan Jul 31.pdf`). If it only says the kind, you get `clipboard-empty.pdf`. If it says nothing, `clipboard-empty`.
> 3. Hovering it says: "Nothing came through on paste — use the paperclip button to attach the file."
> 4. It never blocks Send, and it disappears when you send. Tap the × to dismiss.
>
> **Files changed**
> - `client-dist/app.js` — new `addFailedPasteChip()` and `pasteFailureName()`, hooked into the paste handler.
> - `e2e/paste-upload.spec.cjs` — 4 new tests: empty paste, file-URL naming, kind-only naming, dismissing the chip. All 4 pass. Go tests pass.
>
> **Not yet visible to you.** Your live chat still runs the old copy. To see it, restart this session's server — that ends the current chat window.
>
> Next: want me to commit these two files?
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-31-01-3-3620a9db9941.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-31-01-3-3620a9db9941.png" alt="chip.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Commit it
- Show me the diff first
- Wait — change the wording

**USER**

> Git commit and make build

<small>took 15.0s</small><br>
**AGENT**

> Committed as f93b81c. Running `make build` now — cross-compiles all platforms, about 2-4 minutes.

<small>took 20.9s</small><br>
**AGENT**

> **Next action: start a NEW chat session to see the change. This window still runs the old copy.**
>
> Both done:
>
> 1. Committed as `f93b81c` — only the two files I changed (`client-dist/app.js`, `e2e/paste-upload.spec.cjs`). Nothing else was staged.
> 2. `make build` finished — all 6 platform builds (Mac, Linux, Windows) plus the cached copy that new sessions launch from.
>
> In the new session, paste something that carries nothing — you'll get the red chip with the filename.
>
> Separately: this is not published to npm yet, so other machines won't have it. Want me to cut a release?

[Quick replies]
- Not now
- Yes, publish a release
- Show me the commit

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session (no uuid needed -- it ends the calling session). If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session. Afterwards, tell the user in one line: they can save their own /commit-log-then-end command to decide what this button actually does -- their own redaction rules, commit message wording, whether to push, what to leave out -- and swe-swe will run that instead of these instructions.

