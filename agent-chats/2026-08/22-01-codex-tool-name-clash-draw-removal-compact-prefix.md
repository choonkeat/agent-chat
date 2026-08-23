<!-- agent-chat export
title: Codex Tool Name Clash Draw Removal Compact Prefix
date: 2026-08-22
index: 01
slug: codex-tool-name-clash-draw-removal-compact-prefix
session: 288a385e501afe90
agent: claude
version: 0.11.0 (175a053)
-->

# Codex Tool Name Clash Draw Removal Compact Prefix

_2026-08-22 · 01 · claude · agent-chat 0.11.0 (175a053)_

**USER**

> Change our initial prompt `check_messages; reply me with a send_message` to `agent-chat mcp: check_messages; reply with send_message`

<small>took 9.8s</small><br>
**AGENT**

> Finding where the starter prompt "check_messages; reply me with a send_message" is stored.

**USER**

> This is because Codex seem to have tools of same name and were calling something else. Research.

<small>took 4.6s</small><br>
**AGENT**

> Step 1 of 3 done: text changed in client-dist/app.js (2 places). Next: rebuild the binary so the running app picks it up.

<small>took 5.5s</small><br>
**AGENT**

> Researching the Codex name-clash: how swe-swe registers the agent-chat tools for Codex vs Claude, and what its same-named built-ins are.

<small>took 2m 4s</small><br>
**AGENT**

> Step 2 of 3: rebuilding the agent-chat binary so the new text takes effect.

<small>took 35.8s</small><br>
**AGENT**

> Done — start a new session to see the new wording.
>
> Steps 1-3 of 3 done:
> 1. Changed the text in `client-dist/app.js` (both places) to `agent-chat mcp: check_messages; reply with send_message`.
> 2. Rebuilt the program and refreshed the copy that new sessions launch from.
> 3. Unit tests pass. No test was pinned to the old wording.
>
> Research on the Codex mix-up:
> - I read all 40 Codex session recordings stored on this machine. In every one, Codex called the correct agent-chat tools. So the wrong call you saw happened in a session whose recording isn't on this machine — I can't inspect it directly.
> - I did find the likely cause. Codex is handed five separate tool groups at once, and two of them have confusingly close names: agent-chat's `send_message` (talks to you) and the swe-swe orchestrator's `send_chat_message` (talks to a *different* agent session, not to you). A bare "send a send_message" can land on the wrong one.
> - One genuinely duplicated name also exists: `draw` is in both agent-chat and the whiteboard group. Same trap, different tool.
> - Codex labels the agent-chat group `swe-swe-agent-chat`, not `agent-chat`. Your new wording says "agent-chat mcp", which is close but not the exact label.
>
> Still old wording in one place: swe-swe types this same line itself when no browser tab is open (`chatNudgeText` in the swe-swe repo). Change that one too?

[Quick replies]
- Yes, change swe-swe's copy too
- Use the exact label swe-swe-agent-chat instead
- Leave it, I'll test first

**USER**

> I’d like to remove draw mcp tool actually.

<small>took 7.0s</small><br>
**AGENT**

> Checking what the draw tool touches before removing anything.

<small>took 21.6s</small><br>
**AGENT**

> Which draw do you mean? Two different things share that name — pick one and I'll do it.
>
> 1. **agent-chat's draw** (this repo). Draws a picture inside the chat as a bubble and waits for you to click Continue. Removing it means deleting: the tool itself, the picture-drawing code the browser page uses (about 1,900 lines), three reference documents it publishes, and parts of three browser tests. Roughly 45 minutes, and the picture bubbles in past chat logs stop rendering.
>
> 2. **The separate whiteboard tool** (swe-swe repo). A standalone add-on registered for every agent under the name `swe-swe-whiteboard`. Removing it means deleting its entry from five agent config blocks in one file, plus regenerating the stored test snapshots. Roughly 15 minutes, and nothing in agent-chat changes.
>
> Option 2 is what removes the duplicate name while keeping the in-chat drawing you already use.
>
> Say "1" or "2".

[Quick replies]
- 2 — remove the separate whiteboard tool
- 1 — remove agent-chat's draw
- Both
- Neither, explain more

**USER**

> 1 — remove agent-chat's draw

<small>took 11.1s</small><br>
**AGENT**

> Removing agent-chat's draw. Step 1 of 6: mapping every place it appears.

<small>took 56.7s</small><br>
**AGENT**

> Step 2 of 6 done: draw tool + its three reference documents deleted from the server code. Next: the browser page.

<small>took 59.1s</small><br>
**AGENT**

> Step 3 of 6 done: server code clean, all unit tests pass. Next: the browser page (canvas bubbles).

<small>took 54.0s</small><br>
**AGENT**

> Step 4 of 6 done: browser page, stylesheet, and build steps cleaned. Next: the three browser tests.

<small>took 1m 25s</small><br>
**AGENT**

> Step 5 of 6 done: docs and spec files updated, unit tests pass. Next: rebuild and run the browser tests.

<small>took 10m 24s</small><br>
**AGENT**

> Step 6 of 6 running: browser tests are going (cross-compiles all platforms first, so ~10-15 minutes). I'll report the result.

<small>took 1m 28s</small><br>
**AGENT**

> Browser tests: 138 passed, 9 failed — all 9 are the known connection flakiness, not the removal. Re-running just those to confirm.

<small>took 3m 31s</small><br>
**AGENT**

> Draw is gone. Nothing is committed yet — say the word and I'll commit.
>
> Steps 1-6 of 6 done:
> 1. Deleted the `draw` tool from the server, plus its three reference documents.
> 2. Deleted the picture-drawing code from the browser page, its styles, and the pre-built drawing file (about 2,500 lines total).
> 3. Dropped the drawing step from the build scripts.
> 4. Updated the README and the four spec files that described the tool.
> 5. Unit tests: all pass. Agents now see nine tools instead of ten.
> 6. Browser tests: 138 passed. The 9 that failed were re-run and passed; a rotating handful fails each run because the test browser is shared with this session and gets its page pulled out from under it. None of them touch drawing.
>
> Two things to know:
> - This session still runs the old program. New sessions get the new one.
> - Old chat logs that contain drawings will show a gap where the drawing was.
>
> Also still pending from before: swe-swe types the old wake-up line itself when no browser tab is open. Change that one too?

[Quick replies]
- Commit this
- Also fix swe-swe's wake-up line
- Show me the diff first

**USER**

> What’s our latest opening prompt? I’d like to change the suffix to `report progress before you start processing`

<small>took 7.6s</small><br>
**AGENT**

> Changing the opening line's second half to "report progress before you start processing", then rebuilding.

<small>took 22.8s</small><br>
**AGENT**

> New opening line, live for new sessions:
>
> `agent-chat mcp: check_messages; report progress before you start processing`
>
> Steps 1-3 of 3 done:
> 1. Replaced the second half in both places it appears.
> 2. Rebuilt and refreshed what new sessions launch from.
> 3. Unit tests pass.
>
> Two related lines I did NOT touch — say if you want either changed:
> - When you interrupt with voice, the line is `check_messages; ask me how to proceed`.
> - When you clear the context, the line is `resume <file> - read the whole file for context, then check_messages for your instruction ...`.
>
> Still uncommitted: this change plus the whole draw removal. Commit them now, as two separate commits?

[Quick replies]
- Yes, commit as two commits
- Change the interrupt line too
- Not yet

**USER**

> Yes, commit as two commits

**USER**

> make build

<small>took 8.4s</small><br>
**AGENT**

> Committing: first the opening-line change, then the draw removal.

<small>took 50.5s</small><br>
**AGENT**

> Build done, all six platforms. Start a new session to get the new version.
>
> Steps 1-3 of 3 done:
> 1. Commit `826387a` — the new opening line.
> 2. Commit `3b9137b` — draw removed everywhere.
> 3. `make build` succeeded: six platform builds, and the copy new sessions launch from was refreshed.
>
> Nothing is pushed. Push these two commits?

[Quick replies]
- Push them
- Not yet
- Show me the commits first

**USER**

> We have a `/clear` prefix that does /clear and a prompt. I want an equivalent `/compact` prefix

<small>took 5.0s</small><br>
**AGENT**

> Reading how the /clear prefix works, then adding /compact the same way.

<small>took 1m 41s</small><br>
**AGENT**

> Adding /compact alongside /clear in the chat page now.

<small>took 4m 23s</small><br>
**AGENT**

> `/compact fix the logout bug` now works the same way `/clear` does. Try it in a NEW session.
>
> What it does, in order:
> 1. Types `/compact` into the agent's terminal, so the agent summarises the conversation instead of losing it.
> 2. Records your instruction (the `/compact ` part is stripped) and puts it in the queue.
> 3. Wakes the agent with the normal opening line — no "read this file" line, because the summary already is the context.
>
> A bare `/compact` with no instruction just summarises. Typing it also works when the `/` suggestion box is open.
>
> Tests: 16 of 18 passed first go; the 2 that failed were the connection flakiness and passed on re-run. One is the pre-existing `/clear` test, untouched by this.
>
> One number I guessed: the wake-up line waits 20 seconds after your instruction is recorded, because summarising takes a while and waking too early would cancel it. If your compactions usually take longer, tell me and I'll raise it — it's one line (`compactNudgeDelayMs`).
>
> Commit this as one commit?

[Quick replies]
- Commit it
- Make the wait 45 seconds
- Show me the diff first

**USER**

> Commit it

<small>took 5.1s</small><br>
**AGENT**

> Committing the /compact prefix.

<small>took 12.0s</small><br>
**AGENT**

> Committed as `45137f5`.
>
> Three commits now sit unpushed on main:
> 1. `826387a` — new opening line
> 2. `3b9137b` — draw removed
> 3. `45137f5` — /compact prefix
>
> Push all three?

[Quick replies]
- Push them
- Not yet
- Also fix swe-swe's wake-up line first

**USER**

> make build

<small>took 4.2s</small><br>
**AGENT**

> Running make build (all six platforms, ~2 minutes).

<small>took 14.7s</small><br>
**AGENT**

> Build succeeded, all six platforms. Start a new session to get `/compact`.
>
> What's in the new build:
> 1. New opening line naming the agent-chat tools.
> 2. Draw removed.
> 3. `/compact <instruction>`.
>
> Three commits still unpushed. Push them?

[Quick replies]
- Push them
- Not yet
- Also fix swe-swe's wake-up line

**USER**

> /commit-log-then-end

<small>took 11.6s</small><br>
**AGENT**

> Running /commit-log-then-end.

