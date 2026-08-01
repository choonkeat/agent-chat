<!-- agent-chat export
title: Keyboard Hides Last Messages Tdd Fix E2e Flakiness Root Cause
date: 2026-07-31
index: 02
slug: keyboard-hides-last-messages-tdd-fix-e2e-flakiness-root-cause
session: 86a5103452d2e7d0
agent: claude
version: 0.8.21 (ad470fa)
-->

# Keyboard Hides Last Messages Tdd Fix E2e Flakiness Root Cause

_2026-07-31 · 02 · claude · agent-chat 0.8.21 (ad470fa)_

**USER**

> There’s an uncommitted bug report in tasks/

<small>took 26.8s</small><br>
**AGENT**

> `tasks/2026-07-31-keyboard-hides-last-messages.md` — here it is in plain language.
>
> ## What goes wrong
> On an iPhone, you tap the box where you type your message. The on-screen keyboard slides up, so the space left for the page gets shorter. The typing bar — which always sits at the bottom of the screen — moves up with the new bottom edge and comes to rest on top of the last few messages.
>
> Nothing is deleted. If you drag the conversation upward with your finger, the covered messages come back. But at the exact moment you start typing, the message you're replying to and its suggested-reply buttons are hidden behind the typing bar.
>
> It happened twice, at two different points in the conversation.
>
> ## Why it happens
> The typing bar is set to always hug the bottom of the visible area. The conversation underneath it just scrolls normally. When the conversation is scrolled all the way to the end, there is nothing underneath the bar and everything looks right. At any other position, the bar simply paints over whatever is beneath it.
>
> Opening the keyboard shrinks the visible area but does not move the conversation. So the conversation stays exactly where it was, the bar moves up, and it lands on top of messages. The hidden strip is as tall as the keyboard — on a modern iPhone, roughly 40% of the screen.
>
> The reason nothing corrects for this: the code has no instruction that says "when the visible area changes size, scroll back down to the newest message." It only reacts to the user scrolling, and to new messages arriving.
>
> ## Two different situations, both need handling
> - **Inside the swe-swe pane** (the chat is embedded in another page): the keyboard never covers the chat directly. The outer page makes the chat area shorter, and the chat is told about it in the ordinary way.
> - **In a plain Safari tab**: the keyboard floats on top instead. As far as the page knows, nothing changed size — the chat is only told about it through a separate iPhone-specific signal.
>
> Fix one and not the other, and the bug looks solved in one place while staying broken in the other.
>
> ## The proposed fix
> About eight lines: whenever the visible area changes size, scroll back down to the newest message. One exception, already built in — if you have deliberately scrolled back through older messages, leave you where you are. Yanking a reader forward would be its own bug.
>
> Two smaller follow-ups, both marked lower priority: also re-scroll when you tap into the typing box, and again as the typing box grows taller with long messages (that one hides a line or two, not a screenful).
>
> Three bigger alternatives were considered and rejected — reserving permanent empty space at the bottom, taking the typing bar out of the normal page flow, and restructuring so the conversation scrolls inside its own box rather than the whole page scrolling. The first two need the same size-measuring work anyway and waste screen space; the third is genuinely the cleanest but is a much larger change than this bug warrants.
>
> ## How it gets checked
> Automated tests can't catch this — it needs a real keyboard. The report lists five by-hand steps on an iPhone: tap the box and confirm the last message and its reply buttons stay visible; scroll far back, tap the box, and confirm the view does *not* jump; dismiss the keyboard and confirm no leftover gap; then repeat the whole thing in a plain Safari tab.
>
> Screenshots were left out on purpose — they show a private server address in the browser bar, and this project is public.
>
> **Next:** say the word and I'll apply the eight-line fix so you can test it on your phone.

[Quick replies]
- Apply the fix
- Not yet — just wanted to understand it
- Apply the fix plus the two follow-ups

**USER**

> Git commit the bug file. Tdd fix.

<small>took 56.6s</small><br>
**AGENT**

> Step 1 of 4 done: bug report committed (`6e5fbe2`).
>
> Now writing the failing test first, then the fix. About 15 minutes.

<small>took 4m 32s</small><br>
**AGENT**

> Step 3 of 4 done: test red → fix → green.
>
> - `e2e/keyboard-repin.spec.cjs` — 3 tests, all passing
> - Before the fix: 392px of the last message hidden behind the input bar
> - Fix: ~10 lines in `client-dist/app.js`
>
> Running the full test suite now (about 5 minutes).

<small>took 10m 0s</small><br>
**AGENT**

> Done — two commits, and the tests prove the fix.
>
> 1. `6e5fbe2` — the bug report, committed as written.
> 2. `adbb262` — the fix plus a new test file, `e2e/keyboard-repin.spec.cjs`.
>
> ## How the test-first part went
> I wrote three checks before touching any working code, and confirmed they failed for the right reason:
>
> - **Last message stays visible when the keyboard opens** — failed, with 392 pixels of the last message hidden behind the typing bar. That is the bug, measured.
> - **A reader who has scrolled back is left alone** — passed from the start. Nothing was going to move them, because nothing was reacting to the keyboard at all.
> - **The iPhone-only path is wired up** — failed, because it did not exist yet.
>
> Then I added the fix — ten lines in `client-dist/app.js` — and all three passed.
>
> ## What the fix does
> When the visible area changes size, the conversation scrolls back down to the newest message. Unless you had deliberately scrolled back through history, in which case you stay where you are.
>
> Two details beyond the original suggestion in the report:
> - It scrolls twice — once straightaway and once a fraction of a second later. Safari has not finished settling when it first reports the change, and the immediate scroll covers the case where the page is not the one you are looking at, where the delayed one never runs.
> - Only the size-change signal is used, not the pinch-and-pan one. Reacting to pan would fight you while you are zooming.
>
> ## Test results
> - Go tests: all pass.
> - The three new checks: pass on their own and inside the full run.
> - Full browser suite: 29 other tests failed — every single one because the test browser could not reach the test server, not because anything was wrong with the page. I tracked that down: the browser reaches this machine's servers through a forwarder that takes a few seconds to notice a newly opened one, so a test that connects instantly gets refused. My new test retries for up to 20 seconds and passes every run. The older tests do not retry, which is why they have always been flaky here.
>
> The fixed version is live for new sessions — the copy that new sessions launch has been refreshed.
>
> **Next:** open this chat on your iPhone, tap the typing box, and check the last message and its reply buttons stay visible. Reply here if anything still gets covered.
>
> Separately: the 29 flaky tests are a one-line-each fix now that the cause is known. Want me to do that pass?

[Quick replies]
- Tested on iPhone — looks good
- Still covered — here's what I see
- Yes, fix the 29 flaky tests

**USER**

> Make build already?

<small>took 23.6s</small><br>
**AGENT**

> `make build` done — all six versions rebuilt with the fix.
>
> Mac (two chip types), Linux (two), Windows (two). The copy that new chat sessions launch was refreshed too.
>
> **Next:** start a new session, open it on your iPhone, tap the typing box, and check the last message and its reply buttons stay visible. This session is still running the old copy, so test in a fresh one.

[Quick replies]
- Tested — the last message stays visible
- Still covered — here's what I see
- Yes, fix the 29 flaky tests

**USER**

> Yes, fix the 29 flaky tests

<small>took 1m 2s</small><br>
**AGENT**

> All 53 navigation points across 15 test files now retry. Running the full suite to confirm — about 4 minutes.

<small>took 18m 43s</small><br>
**AGENT**

> Flaky tests fixed — the whole suite now passes, 94 out of 94, twice in a row.
>
> Committed as `bd8d5be`.
>
> ## What was actually wrong
> Every test starts its own copy of the chat server on a randomly picked door number, then immediately tells the test browser to open it. The browser reaches this machine through a middleman that takes a few seconds to notice a newly opened door. So the browser knocked before the middleman knew the door existed, and got turned away.
>
> I measured it: opening the page instantly failed all four times I tried; waiting eight seconds first succeeded all four times.
>
> Nothing was wrong with the app, and nothing was wrong with the tests' own logic. That is why the failures moved around from run to run.
>
> ## What I changed
> One new shared piece of code that keeps knocking until the door answers, giving up after 20 seconds. All 53 page-opening spots across 15 test files now go through it. When the door is already open — which is most of the time — the first knock works and nothing is slower.
>
> One extra detail: a refused page load sometimes reports itself as a different kind of error, because the browser swaps in its own error page and that counts as a competing page load. That shape is treated as "keep knocking" too.
>
> ## Results
> - Before: 29 failures out of 93, every one a connection refusal, zero real problems.
> - After: 94 of 94 pass. Ran it twice to be sure.
> - Whole suite takes about 6 minutes.
>
> **Next:** run `make test` yourself once to confirm you see the same green result.
>
> Separately: a new bug report appeared in `tasks/` today — `2026-08-01-read-receipt-lies-on-terminal-breakout.md`, uncommitted. Want me to look at that one next?

[Quick replies]
- Yes, take the read-receipt bug next
- I'll run make test first
- Nothing more for now

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session (no uuid needed -- it ends the calling session). If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session. Afterwards, tell the user in one line: they can save their own /commit-log-then-end command to decide what this button actually does -- their own redaction rules, commit message wording, whether to push, what to leave out -- and swe-swe will run that instead of these instructions.

