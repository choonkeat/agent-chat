<!-- agent-chat export
title: Rebuild Agent Chat Binary
date: 2026-08-05
index: 01
slug: rebuild-agent-chat-binary
session: 8736671d7a18acf7
agent: claude
version: 0.9.1 (7abf230)
-->

# Rebuild Agent Chat Binary

_2026-08-05 · 01 · claude · agent-chat 0.9.1 (7abf230)_

**USER**

> What has changed recently?

<small>took 29.5s</small><br>
**AGENT**

> Reading the recent commit history and the uncommitted edits to see what changed.

<small>took 21.0s</small><br>
**AGENT**

> **Next action: decide what to do with the one unfinished change sitting in the working folder — it is written but not saved into the project history yet.**
>
> **Unfinished (not saved yet), 3 files:**
> A new way for an outside program to ask this app one question: *"is the agent currently sitting and waiting for a message from the user, right now?"*
>
> Why it matters: if you send the agent a message while it is **not** waiting, the message just sits in a holding line (a queue) and nobody tells the agent to look — so it stays unread. The browser page solves this by poking the agent itself. Programs talking to the app without a browser had no way to poke it. This change gives them the missing signal, so they can poke only when needed.
>
> Two deliberate details in it:
> 1. Asking the question does **not** hand over any waiting message — asking must not consume the answer.
> 2. It correctly reports "nobody waiting" even when a wait was cut short, so a message can't get stranded.
>
> A test covering all three states (idle / waiting / finished) is included.
>
> **Already saved and released — version 0.9.1, released 2 days ago (Aug 3).** What landed in 0.9.x:
> 1. **Read receipts** — a message is marked read at the moment the agent actually asks for it, not earlier.
> 2. **One path for every reply** — drawn replies now go through the same send route as typed ones, instead of a separate one.
> 3. **"Conversation context only" setting** — a tick box that resets what the agent carries over between messages; off by default, and attachments reset with it.
> 4. **`/clear` with an instruction** — clears the context and hands the rest of your sentence straight to the agent.
> 5. **iPad paste fix** — the space iOS adds after a paste no longer breaks @-name autocomplete.
>
> Also present but untracked: two exported chat logs and a `.pi/` folder, neither saved into history.
>
> **Next step (under 2 min): want me to run the test suite on the unfinished change so you know whether it's ready to save?**

[Quick replies]
- Yes, run the tests
- Just commit it
- Show me the code
- Leave it alone

**USER**

> make build so we can use the new (not committed) agent-chat on this machine upon next session (we npm link)

<small>took 10.2s</small><br>
**AGENT**

> Running `make build` now — this compiles the app for every platform, so it takes a few minutes.

<small>took 29.0s</small><br>
**AGENT**

> **Next action: start a NEW session — this one keeps running the old app until you do.**
>
> Build finished. What happened, in order:
> 1. Bundled the drawing/canvas part of the page.
> 2. Compiled the app for all 6 machine types (Mac, Linux, Windows — two chip types each).
> 3. Copied the fresh Linux build into the folder this machine actually starts the app from (`~/.swe-swe/npx-cache/...`), timestamped 08:29 today.
> 4. Re-linked the package so the `agent-chat` command points at your local copy.
>
> The unsaved change (the "is the agent waiting for a message right now?" question) is now inside that built app.
>
> One caveat: your currently running session already loaded the old app into memory. Nothing you do in this chat will pick up the new one.
>
> **Next step (under 2 min): open a new session and the new build is live.**

[Quick replies]
- Got it, starting a new session
- Run the tests first
- Commit the change now
- Show me what changed

<small>took 2m 16s</small><br>
**AGENT**

> Build is done and the fresh binary is in place — start a new session to use it.

[Quick replies]
- Got it, starting a new session
- Run the tests first
- Commit the change now
- Show me what changed

**USER**

> /commit-log-then-end

<small>took 3.2s</small><br>
**AGENT**

> Saving this chat log and committing it, then ending the session.

