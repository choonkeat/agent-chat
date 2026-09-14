<!-- agent-chat export
title: Agent Chat Context Bloat Audit
date: 2026-09-15
index: 01
slug: agent-chat-context-bloat-audit
session: 05ef3d1bd6c811c1
agent: claude
version: 0.13.0 (47756c6)
-->

# Agent Chat Context Bloat Audit

_2026-09-15 · 01 · claude · agent-chat 0.13.0 (47756c6)_

**USER**

> How poorly is agent-chat causing context bloat? Eg our wrapper content around user messages, the to-and-fro between agent and the MCP server, etc. See my ~/.claude jsonl

<small>took 6.4s</small><br>
**AGENT**

> Measuring agent-chat's context overhead from ~/.claude/projects/**/*.jsonl — counting wrapper text around user messages and agent↔MCP round trips.

<small>took 2m 27s</small><br>
**AGENT**

> **Verdict: agent-chat is not your bloat problem. It is ~1.2% of everything sent to the model.** The other 92% is output from ordinary tools — file reads, searches, test runs.
>
> Measured across all 89 chat sessions on this machine (12,079 model calls, ~327MB of logs).
>
> **Where the space actually goes** (share of everything the model re-reads):
> 1. Output of non-chat tools (reading files, grep, test runs) — **92.2%**
> 2. Instructions injected by Claude Code itself — 2.4%
> 3. What the model asked those tools to do — 3.6%
> 4. **agent-chat, everything it does — 1.2%**
> 5. Your typed words + the model's typed words — 0.7%
>
> **Inside that 1.2%, two-thirds is repeated fixed text, not your words.**
> - agent-chat sent back 917,000 characters total across all sessions.
> - Only **33%** was real content (your messages, real answers).
> - **67%** was the same standing instructions pasted in again on every single call — "this IS the user's message, execute it", "the TUI is invisible", "progress sent, use send_message when done".
> - Worst offender by repetition: `send_progress` returns 167 characters of reminder after **every** call, 896 times, and carries no information at all.
>
> **Per-session reality check:** in a typical (median) session agent-chat takes **5.6%** of the conversation. In the long, heavy sessions — the ones that actually run out of room — it's **1.5%**, because file output swamps it. One small session hit 47%, but that session was small enough that it didn't matter.
>
> **The one fix worth making**, about 20 minutes: trim the three repeated blocks down to one short line each. They live in `tools.go` (the `send_progress` acknowledgment, line 560) and `prompts/agent-reply.tmpl` (2,544 characters re-pasted onto every message you send). That recovers ~153,000 words-worth of space across history and cuts agent-chat's footprint roughly in half — without touching what you or the model actually say.
>
> Next step: open `prompts/agent-reply.tmpl` and read it — it's 2,544 characters, and most of it repeats what's already in the tool descriptions and your CLAUDE.md. Tell me to cut it and I'll propose the trimmed version.

[Quick replies]
- Cut the boilerplate — show me the trimmed version
- Show me the 47% session — why so bad?
- What's in the 92% of tool output? Break that down
- Leave it alone, 1.2% is fine

**USER**

> Cut the boilerplate — show me the trimmed version

<small>took 2.4s</small><br>
**AGENT**

> Reading prompts/agent-reply.tmpl and tools.go:560 — drafting the trimmed versions with before/after character counts.

<small>took 56.0s</small><br>
**AGENT**

> Step 1 of 4: found the three boilerplate sources (2,048 chars per user message + 124 chars per progress call). Writing the trimmed versions now.

<small>took 53.0s</small><br>
**AGENT**

> Step 2 of 4 done: code compiles. Running `make test` — unit tests will flag the pinned boilerplate strings so I can update them.

<small>took 59.1s</small><br>
**AGENT**

> Step 3 of 4 done: unit tests pass, 82% of the boilerplate is gone. Starting the E2E run (~30 min) — the project rule is `make test`, not unit tests alone.

<small>took 9m 43s</small><br>
**AGENT**

> Step 4 of 4 in progress: E2E suite running (167 tests, one at a time, ~30 min). No output yet — Playwright buffers until the first spec finishes. Will report pass/fail.

<small>took 7m 33s</small><br>
**AGENT**

> **Done. Boilerplate cut by 82%; all tests pass** (205 E2E passed, 1 flaky, 1 skipped; unit tests green). Binary rebuilt.
>
> **What you get, per repetition:**
> | Repeated text | Before | After |
> |---|---|---|
> | Wrapper on each of your messages | 2,050 chars | 487 avg |
> | Reply after each `send_progress` | 124 chars | 2 chars (`ok`) |
>
> Across your 89 sessions that is **~125,000 words-worth of space** recovered — agent-chat's total footprint drops 28%.
>
> **The trimmed wrapper** (replaces 2,050 characters of standing instructions):
> > This IS the user's message — execute it, do not echo it back.
> >
> > Reply ONLY via `send_message` (blocking, terminal) or `send_progress` (non-blocking) — TUI text and AskUserQuestion are invisible to the user. `send_progress` first, then execute; do not echo the request back. Never end a turn without `send_message`.
>
> **Why not delete it outright:** every one of those instructions was added after a real failure — the model replying "OK." instead of doing the work, or going silent. So the full text still arrives on the **1st message and every 10th after**, and the short line covers the gap. Refreshing (rather than sending it once) matters because a long session gets its history compressed, which would silently drop a single early copy.
>
> **Changed:** `tools.go` (new `deliveryGuidance`, `executeNotEchoShort`, `progressAck`), `prompts/agent-reply.tmpl` (new short block), `tools_test.go` (a test pinning both forms and the 10-message refresh). Nothing in the browser UI moved.
>
> **One thing left, and it needs you:** the running chat server is still the old binary, so you're still seeing the long text. Restarting it kills this chat window, so I didn't. Next step — when you're ready to lose this window, restart the session; the change takes effect immediately after.
>
> Separately: the `git status` shows the commit isn't made yet. Want me to commit these three files?

[Quick replies]
- Commit the three files
- Show me the full tools.go diff first
- Make it every 20th message, not every 10th
- Revert it — I want the full text every time

**USER**

> When multiple user message of same type arrives to agent together does it make sense to wrap once only?

<small>took 2.1s</small><br>
**AGENT**

> Checking whether batched messages already wrap once — reading the drain-and-format path in tools.go and eventbus.go.

<small>took 51.0s</small><br>
**AGENT**

> **Yes — and it already does.** A batch of 3 messages gets **one** wrapper, not three. The code drains the whole queue, joins the messages, then appends the guidance once (`tools.go`, `composeCheckMessagesResult`).
>
> **But I found a bigger repeat while checking, and it is inside the batch.**
>
> Your message-style preamble — the "Explain in plain, non-technical language…" and "Reply for a reader with ADHD…" paragraphs — is pasted onto **every message individually**, before they are joined. So a 3-message batch does carry it 3 times.
>
> Measured across your logs:
> - "Reply for a reader with ADHD…" — repeated **191 times**, 243 chars each = 46,413 chars
> - "Explain in plain, non-technical language…" — repeated **152 times**, 169 chars each = 25,688 chars
> - **73% of what looks like your own words in the logs is actually these two paragraphs repeated.** That is bigger than the boilerplate I just cut.
>
> **Two separate fixes, ranked:**
>
> 1. **Safe, do now (~10 min):** when several messages arrive together and share the same style preamble, paste it once for the batch instead of once per message. Zero behaviour change — the model reads the identical instruction either way.
>
> 2. **Judgment call, your decision:** apply the same every-10th-message rotation to the style preamble across a session. Saves roughly 65,000 chars, but the preamble is what steers how I write, so dropping it from 9 messages out of 10 risks my replies drifting back to a default style. I would not do this without you agreeing to the risk.
>
> Next step: say the word and I'll implement fix 1 — batch-level deduplication of the style preamble. About 10 minutes, same test-and-verify pass as before.

[Quick replies]
- Do fix 1 — dedupe within the batch
- Do both 1 and 2, I'll accept the drift risk
- Neither — commit what you have
- How often do batches actually have 2+ messages?

**USER**

> Do fix 1 — dedupe within the batch

<small>took 2.4s</small><br>
**AGENT**

> Implementing fix 1: hoisting a shared style preamble out of the per-message loop in FormatMessages, so a batch carries it once.

<small>took 50.1s</small><br>
**AGENT**

> Fix 1 done and unit tests green (4 new tests). Running E2E again (~17 min) — this change touches message formatting, which the browser tests cover.

<small>took 9m 45s</small><br>
**AGENT**

> E2E still running, about 10 minutes in of ~17. No failures reported so far.

<small>took 9m 26s</small><br>
**AGENT**

> **Fix 1 shipped. All tests pass** — unit green, E2E 205 passed / 1 flaky / 1 skipped, exit 0. Binary rebuilt.
>
> **What changed:** when several messages arrive at once and share the same style preamble, it is now written once above the batch instead of once per message.
>
> Before (2 messages):
> > Reply for a reader with ADHD…
> > first message
> >
> > **Reply for a reader with ADHD…**
> > second message
>
> After:
> > Reply for a reader with ADHD…
> >
> > first message
> >
> > second message
>
> **Three cases deliberately left alone**, each with a test:
> 1. A single message — hoisting would move the voice-transcription note behind the preamble, changing existing output. Renders exactly as before.
> 2. Mixed styles in one batch (you changed style mid-batch) — each message keeps its own.
> 3. Templates that embed the message inside themselves — cannot be lifted out.
>
> **Running total across both fixes:** the standing instructions dropped 82%, and batched messages no longer repeat your style preamble.
>
> **Files changed** (5, all committed-ready): `tools.go`, `eventbus.go`, `prompts.go`, `prompts/agent-reply.tmpl`, `tools_test.go` — 212 lines added, 28 removed, 8 new tests.
>
> Next step: say go and I'll commit these five files as one change, staging them by name.

[Quick replies]
- Commit the five files
- Show me the eventbus.go diff first
- Now do fix 2 (rotate the style preamble too)
- Hold off — I want to test it live first

**USER**

> Commit the five files

<small>took 2.9s</small><br>
**AGENT**

> Staging the five files by name and committing.

<small>took 41.6s</small><br>
**AGENT**

> **Committed: `7006818`** — "perf(context): stop repeating standing guidance in every tool result". 5 files, 212 added / 28 removed, staged by name only.
>
> Not pushed — your branch is `main`, so that's your call.
>
> **What's in it:**
> 1. Standing instructions: full text every 10th message, one line in between.
> 2. `send_progress` reply: 124 characters → `ok`.
> 3. Batched messages: your style preamble written once, not once per message.
>
> **Still needs you:** the running chat server is the old binary, so you're still seeing the long wrappers. Restarting kills this window, so I left it.
>
> Next step: `git push` when you're ready, or tell me to and I'll run it.

[Quick replies]
- Push it
- Show me the commit diff
- Now do fix 2 (rotate the style preamble too)
- Done for now

**USER**

> Make build

<small>took 2.2s</small><br>
**AGENT**

> Running `make build` — cross-compiles all platforms and refreshes the npx cache the running session launches from. A few minutes.

<small>took 18.6s</small><br>
**AGENT**

> **`make build` done.** All 6 platform binaries rebuilt at v0.13.0 from commit `7006818`, and the shared launcher cache was refreshed:
>
> > refresh-npx-cache: updated `~/.swe-swe/npx-cache/@choonkeat/agent-chat-linux-x64@0.13.0/bin/agent-chat`
>
> Working tree is clean — no build output leaked into git.
>
> **Next step: start a NEW session.** The build script says so explicitly; this window is still running the old binary, so the long wrappers keep appearing here until you do. Your first message in the new session will carry the full guidance once, and the short line after that.

[Quick replies]
- Starting a new session now
- Push the commit first
- Now do fix 2 (rotate the style preamble too)
- Done for now

**USER**

> /commit-log-then-end

<small>took 2.2s</small><br>
**AGENT**

> Running /commit-log-then-end — freezing this session's chat log, scrubbing it, committing it, then ending the session.

