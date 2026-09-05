<!-- agent-chat export
title: Release Status Since 0 11 0 And User Impact
date: 2026-09-05
index: 01
slug: release-status-since-0-11-0-and-user-impact
session: f6af482e88bb2c08
agent: claude
version: 0.11.0 (39d2a11)
-->

# Release Status Since 0 11 0 And User Impact

_2026-09-05 · 01 · claude · agent-chat 0.11.0 (39d2a11)_

**USER**

> We’ve done a bunch of stuff on and off. I want to make a release and I want to know where we are — what’s the impact on users using previously released version of agent chat

<small>took 16.7s</small><br>
**AGENT**

> Reading git history since the last release tag and the changelog to map user-facing impact.

<small>took 15.9s</small><br>
**AGENT**

> Last release is 0.11.0 (npm, 2026-08-16). 12 commits since. Reading each commit's message and diff for user-facing/breaking impact.

<small>took 1m 11s</small><br>
**AGENT**

> Checking one coupling: swe-swe hardcodes the old wake-up text; verifying whether it matches on it. Then running unit tests.

<small>took 44.9s</small><br>
**AGENT**

> **Next release should be 0.12.0** (one breaking change). Last published: 0.11.0 on 2026-08-16. 12 commits since, all on main, unit tests green, tree clean.
>
> **Impact on users running 0.11.0 or older, ranked:**
>
> 1. **BREAKING: `draw` MCP tool and the three `whiteboard://` resources are gone.** Any prompt, skill or allowlist that calls agent-chat's `draw` gets "unknown tool". Tool count 10 → 9. swe-swe is safe: its only references are a dead `case "draw"` in the transcript summariser and a test constant.
> 2. **Wake-up nudge text changed** to `agent-chat mcp: check_messages; report progress before you start processing`. Nothing breaks, but swe-swe's server-side nudge (`chatNudgeText` in swe-swe-server/main.go) still types the old sentence, so transcripts will show two wordings until swe-swe is updated.
> 3. **Chat archive: no change unless opted in.** Month layout is behind `-chatlog-layout=month`; default stays flat, so an 0.11.0 copy and a 0.12.0 copy sharing one archive stay compatible. New `agent-chat migrate-chatlogs` subcommand exists but is manual. One visible diff: `assets/viewer.js` is rewritten on the next export (image-URL fix), so users will see a one-file change in their repo.
> 4. **New, additive:** `/compact <instruction>` prefix; paste works after clicking a bubble; dropped folders/empty files show a failed chip instead of uploading 0 bytes; html/uri-list/rtf clipboard fallbacks.
> 5. **Blocker before publishing:** CHANGELOG `[Unreleased]` only covers the chatlog work. Items 1, 2 and 4 are unwritten.
>
> Rollout: swe-swe installs via `swe-npx -y @choonkeat/agent-chat` (unpinned, 15-min latest cache), so new sessions pick up 0.12.0 within ~15 minutes of publish; running sessions keep 0.11.0.
>
> Release plan (about 20 minutes): 1) write the missing changelog entries, 2) `make bump VERSION=0.12.0` + commit, 3) `make publish` with your OTP, 4) verify `npm view` shows 0.12.0.
>
> Next step: reply "go" and I do steps 1–2 now, then ask you for the OTP.

[Quick replies]
- go
- Show me the full changelog draft first
- Hold — questions first

**USER**

> Wrt wake up nudge, I feel we should be control the text instead of both parties always having to coordinate
>
> Wrt chat archive no change unless opt in — but the index.html generator is totally compatible if some ppl working on the repo decides to opt in and others don’t opt in, correct? We tolerate presence of both styles in the going-to-be-released version. Then we can make it default in the subsequent version — even if more old path chat logs enter, they will be automatically migrated until old conversation session dies and everyone is on the default=on version?
>
> Wrt /compact prefix, right now for both /clear and /compact prefix we remove it from the user speech bubble — erasing evidence of it being used. I think we should leave the evidence in. So studying chat logs can let us know what’s going on. AND how can we squeeze in this prefix into our slash command autocomplete (even though they are not actually slash commands) ONLY for when typed into the beginning of the text? AND rename them to /clear-and-then and /compact-and-then ?
>
> Discuss. Split separate questions into separate send_message so I answer one at a time

<small>took 4.1s</small><br>
**AGENT**

> Three topics: nudge text ownership, mixed-layout archive compatibility, /clear & /compact prefix handling. Checking the code for each, then one send_message per topic.

<small>took 1m 20s</small><br>
**AGENT**

> **Question 1 of 3: wake-up nudge — agree, agent-chat should own the text. Proposal below, reply yes/no.**
>
> Where the sentence lives today:
> 1. agent-chat `app.js` `chatNudgeText` — sent to swe-swe via `agent-chat-interrupt` postMessage on all 6 browser-driven nudges. swe-swe just types what it receives. Already agent-chat-controlled.
> 2. swe-swe server `chatNudgeText` const (main.go:9207) — typed only when zero browsers are connected and `agent_waiting` says nobody is parked. This is the one hardcoded copy.
> 3. swe-swe `terminal-ui.js` fallback string `check_messages; i sent u a chat message` — used only if the postMessage carries no text. Dead in practice.
>
> Proposal:
> 1. Move the sentence into Go (one const), inline it into the page like `CTX_ONLY_SESSION` so app.js reads it from the server.
> 2. `agent_waiting` returns `{"waiting": false, "nudge": "<sentence>"}`. swe-swe already calls `agent_waiting` right before deciding to type, so it types `nudge` and falls back to its const only when the field is absent (older agent-chat).
> 3. swe-swe deletes its const once agent-chat ships this.
>
> Cost: agent-chat about 30 minutes, swe-swe about 15 minutes. Additive, fits in 0.12.0.
>
> Next step: reply "yes" to include it in 0.12.0, then I send question 2 (archive layout).

[Quick replies]
- yes, include in 0.12.0
- no, keep as is
- different approach (I'll explain)

**USER**

> Does the plan work with older version of swe-swe? We can’t guarantee folks upgrade both

<small>took 4.0s</small><br>
**AGENT**

> Checking the four version pairings (old/new swe-swe × old/new agent-chat) against the code paths.

<small>took 11.7s</small><br>
**AGENT**

> **Yes, all four pairings work. One correction to the plan: swe-swe keeps its constant as the fallback instead of deleting it.**
>
> 1. **Old swe-swe + new agent-chat:** swe-swe unmarshals `agent_waiting` into a struct with only `waiting`, so the extra `nudge` field is ignored. Headless nudge types the old sentence; browser nudges type the new one (swe-swe already types whatever the postMessage carries). Same two-wording drift as today, nothing breaks.
> 2. **New swe-swe + old agent-chat (0.10.0–0.11.0):** `agent_waiting` exists but has no `nudge` field. swe-swe falls back to its constant. Works.
> 3. **New swe-swe + agent-chat before 0.10.0:** `agent_waiting` is an unknown tool. swe-swe already treats that error as "nobody parked" and nudges with its constant. Works.
> 4. **Both new:** one sentence, owned by agent-chat.
>
> Next step: reply "yes" to include it in 0.12.0, then I send question 2 (archive layout).

[Quick replies]
- yes, include in 0.12.0
- no, keep as is

**USER**

> yes, include in 0.12.0

<small>took 3.6s</small><br>
**AGENT**

> Q1 decided: nudge text moves into agent-chat, ships in 0.12.0. Sending question 2 (archive layout).

<small>took 10.2s</small><br>
**AGENT**

> **Question 2 of 3: archive layout. Your mixed-layout reading is correct; your "automatically migrated" reading is not. Decide whether manual-only migration is acceptable.**
>
> What 0.12.0 guarantees:
> 1. `scanChatExports` reads the root and every `YYYY-MM/` directory. Index, daily NN numbering and session resume all see both shapes. Opt-in writers and default writers can share one repo and any 0.12.0 copy regenerating `index.html` lists everything.
> 2. The one hazard is a copy older than 0.12.0: it lists only flat files, so its `index.html` regen drops the month chats until a newer copy exports again. Self-healing, but a tracked-file churn. That is the only reason the default stays flat this release.
>
> What happens when the default flips (0.13.0):
> 3. New chats go into month directories. Existing flat chats stay flat forever, still listed. Nothing moves on its own.
> 4. A lingering old session keeps writing flat files; those also stay flat. A mixed archive is a permanent supported state, not a transition.
> 5. Tidying is one manual `agent-chat migrate-chatlogs -apply` whenever someone wants it. Auto-moving at export time is what the revert avoided: an export silently renaming tracked files other sessions have open.
>
> Next step: reply "manual is fine" to keep this as is, or "I want auto-migrate" and I'll cost a safe variant. Then question 3 (reset prefixes).

[Quick replies]
- manual is fine
- I want auto-migrate
- flip the default now in 0.12.0

**USER**

> manual is fine. But we should have agent chat version in the logs. So we can tell if folks are still using old or all has moved

<small>took 4.9s</small><br>
**AGENT**

> Checking what version info the chat logs already carry, then sending question 3.

<small>took 22.8s</small><br>
**AGENT**

> **Q2 closed: every chat log already carries the version.** Line 8 of each file's header is `version: 0.11.0 (39d2a11)`, present in all 51 committed logs. To see who is on what:
>
> ```
> grep -h '^version:' agent-chats/*.md agent-chats/*/*.md | sort | uniq -c | sort -rn
> ```
>
> Separately, later: `index.html` does not show the version, so this stays a grep. Say if you want it in the listing.
>
> **Question 3 of 3: reset prefixes. Proposal, reply per numbered item.**
>
> 1. **Keep the evidence.** Today `routeClearPrefix` strips the command from the bubble and the server records only the instruction, so the log shows nothing. Proposal: the browser sends the command as a separate `command` field; the server writes `**USER** _(/clear-and-then)_` in the log and the bubble shows a small badge; the agent still receives the bare instruction (a leading `/clear-and-then` in its queued message would confuse it). "Conversation context only" mode gets the same badge on every message, which is accurate evidence too.
> 2. **Autocomplete at position 0 only.** `findTrigger` already reports the trigger position. When `/` is at position 0, agent-chat prepends its two built-in entries to whatever swe-swe's slash-command list returns; anywhere else, nothing. Client-side only, works with old swe-swe.
> 3. **Rename** to `/clear-and-then <instruction>` and `/compact-and-then <instruction>`. Bare `/clear` and `/compact` keep working as plain wipe/summarise. The old `/clear <instruction>` spelling is dropped (0.12.0 is already a breaking release) and the MOTD tip is updated.
>
> Cost: about 2 hours including e2e updates. Ships in 0.12.0 with Q1.
>
> Next step: reply "all three" or name the item to change.

[Quick replies]
- all three
- keep old /clear <x> as alias too
- change item 1 (I'll explain)

<small>took 3m 25s</small><br>
**AGENT**

> Q1 implemented while you read Q3: nudge text now lives in Go, inlined as CHAT_NUDGE_TEXT, returned by agent_waiting. Unit tests green. Writing the swe-swe handoff task next.

<small>took 25.2s</small><br>
**AGENT**

> **Q3 still open (proposal is in the message above): reply "all three" or name the item to change.**
>
> Progress: Q1 done in code. `chatNudgeText` now lives in `tools.go`, is inlined into the page as `CHAT_NUDGE_TEXT`, and `agent_waiting` returns it as `nudge`. Unit tests pass. swe-swe follow-up is filed at `/workspace/tasks/2026-09-05-agent-chat-owns-the-nudge-text.md`.
>
> Remaining before publish: Q3 decision → implement → changelog → bump 0.12.0 → publish.

[Quick replies]
- all three
- keep old /clear <x> as alias too
- change item 1 (I'll explain)

**USER**

> all three

<small>took 3.9s</small><br>
**AGENT**

> Q3 approved. Implementing in 4 steps: 1) rename prefixes + keep the command in bubble and log, 2) strip it agent-side only, 3) autocomplete entries at position 0, 4) update e2e specs, README, ADR. Starting step 1.

<small>took 1m 9s</small><br>
**AGENT**

> Step 1 of 4 in progress: mapped every code and test site for the prefix rename. Editing app.js, main.go and the chat-log writer now.

**USER**

> Adding one more change: we have a beep sound when agent chat took more than n seconds and it’s human’s turn. For browser tab our of focus, we can beep regardless of whether it took more than n seconds — discuss

<small>took 3m 8s</small><br>
**AGENT**

> Steps 1–3 of 4 done: prefixes renamed, command kept in bubble (badge) and log, agent still gets the bare instruction, `/` at position 0 offers the built-ins. Unit tests green. Step 4: updating e2e specs, README, ADR, then running the affected specs.

<small>took 2m 3s</small><br>
**AGENT**

> Step 4 of 4: specs, README and ADR updated. Rebuilding the binary and warming the browser, then running the three affected e2e specs (about 5 minutes).

<small>took 5m 32s</small><br>
**AGENT**

> **Q3 done: 29 of 29 e2e pass across clear, compact and ding specs.** Two more specs (autocomplete, chat-submit) are running now, about 4 minutes.
>
> **Ding when the tab is away: agree, with one signal choice to make.**
>
> 1. **Use `document.visibilityState === 'hidden'`, not `document.hasFocus()`.** Inside swe-swe, agent-chat is an iframe: clicking the terminal pane makes `hasFocus()` false while you are looking straight at the chat, so every one-liner would ding. `visibilityState` is hidden only when the tab is in the background, the window is minimised, or the phone is on another app. That is the case you mean.
> 2. **Rule becomes:** ding if the run took over 20s, OR the page was hidden when it finished. Visible short runs stay silent, as today.
> 3. **Known limit, unchanged:** iOS suspends audio for backgrounded pages, so a hidden-tab ding on iPhone still depends on the screen being on (same as the existing long-run ding).
> 4. Cost: about 20 minutes with an e2e test. Ships in 0.12.0.
>
> Next step: reply "yes" and I implement it, then write the changelog and bump.

[Quick replies]
- yes, visibilityState rule
- use hasFocus instead
- skip it for 0.12.0

<small>took 4m 44s</small><br>
**AGENT**

> **Ding question still open (proposal in the message above): reply "yes, visibilityState rule", "use hasFocus instead", or "skip it for 0.12.0".**
>
> Progress since: all 52 affected e2e tests pass (clear, compact, ding, autocomplete, chat-submit, welcome-replies). CHANGELOG Unreleased now covers every change: draw removal, the `/clear-and-then` rename with the badge, built-in slash autocomplete, the nudge, and the paste fixes.
>
> After your answer: implement the ding (if yes) → `make bump VERSION=0.12.0` → commit → publish with your OTP.

[Quick replies]
- yes, visibilityState rule
- use hasFocus instead
- skip it for 0.12.0

**USER**

> yes, visibilityState rule

<small>took 5.6s</small><br>
**AGENT**

> Ding approved. Step 1 of 3: adding the hidden-tab rule and its test. Then bump to 0.12.0 and commit.

<small>took 2m 4s</small><br>
**AGENT**

> Step 1 of 3 done: ding rule in, 11 of 11 ding tests pass, two feature commits landed. Step 2: changelog release header + bump to 0.12.0.

<small>took 2m 29s</small><br>
**AGENT**

> **Step 2 of 3 done. Step 3 needs you: log in to npm, then send me a fresh 6-digit code from your authenticator.**
>
> 1. Open this link and approve the login (the saved npm token had expired):
>    [REDACTED]
> 2. Reply here with a fresh one-time code (6 digits) from your authenticator app. Publishing needs it separately from the login.
>
> What is ready to ship as 0.12.0 (4 commits on main, not yet pushed):
> - Reset commands renamed to `/clear-and-then` and `/compact-and-then`, shown as a badge and kept in the chat log.
> - Typing `/` at the start of a message lists those commands.
> - The wake-up sentence now comes from agent-chat itself.
> - The finish sound plays for any run while the tab is hidden.
> - Earlier work: the draw tool removed, paste fixes, month folders for chat logs (off by default).
>
> Version 0.12.0 is stamped, all six platform builds are done, unit and affected browser tests pass.
>
> Next step: after publishing I will ask before pushing the commits to GitHub.

[Quick replies]
- Logged in, code is: 
- Hold the publish

<small>took 5m 16s</small><br>
**AGENT**

> **Corrected login link (the first one died; use this one instead):**
> [REDACTED]
>
> 1. Open the link and approve the login. The login command is waiting for that approval.
> 2. Reply here with a fresh 6-digit code from your authenticator. The publish needs it separately.
>
> Everything else for 0.12.0 is ready: version stamped, all six builds done, tests green, four commits on main not yet pushed.

[Quick replies]
- Logged in, code is: 
- Hold the publish

<small>took 5m 6s</small><br>
**AGENT**

> **The login link expires after 5 minutes, so run the login yourself when you are ready. Type this into the chat box, exactly, starting with the exclamation mark:**
>
> ```
> ! npm login --auth-type=web
> ```
>
> 1. It prints a link. Open it and approve.
> 2. When it says "Logged in", reply here with a fresh 6-digit code from your authenticator. The publish needs that code separately.
>
> Everything else for 0.12.0 is ready and waiting.

[Quick replies]
- Logged in, code is: 
- Hold the publish

**USER**

> Expired link

<small>took 8.7s</small><br>
**AGENT**

> Generating a fresh link now. It is valid for 5 minutes from when it appears.

<small>took 21.2s</small><br>
**AGENT**

> **Fresh link, valid until about 5 minutes from now. Open and approve:**
> [REDACTED]
>
> Then reply with a fresh 6-digit code from your authenticator, and I publish 0.12.0 right away.

[Quick replies]
- Approved, code is: 
- Link expired again

**USER**

> Logged in. Don’t use otp, use an approval link easier. Maybe we can fix our prompt too

<small>took 9.5s</small><br>
**AGENT**

> Login confirmed. Switching the publish to the browser-approval flow (no code typing). Checking the publish script first.

<small>took 41.4s</small><br>
**AGENT**

> **Approve link 1 of up to 7 (one per package):**
> [REDACTED]
>
> I post each next link here as soon as npm asks. No code typing needed.

**USER**

> Approved

<small>took 1m 14s</small><br>
**AGENT**

> Publish finished on that one approval: all 7 packages accepted. Confirming they are visible on the registry.

<small>took 41.6s</small><br>
**AGENT**

> **0.12.0 is published. Next decision: push the commits to GitHub?**
>
> Publish result (one approval covered all 7 packages):
> - 6 platform packages: visible on the registry at 0.12.0.
> - Main package `@choonkeat/agent-chat`: accepted by npm, not yet visible 90 seconds later. This is the usual 1–2 minute registry delay. I am watching it and will confirm.
>
> Pending push: 16 commits on main are not on GitHub yet (12 from earlier sessions plus today's 4: the reset rename, the ding rule, the changelog, the version bump). Pushing publishes them to the public repo.
>
> Reply "push" and I run `git push`.
>
> Separately, later: you mentioned fixing our prompt so future releases use the approval link, not a code. I have already recorded that in my notes for this project. Want it in the repo docs too?

[Quick replies]
- push
- don't push yet
- push, and add the release note to the repo docs

**USER**

> /commit-log-then-end

<small>took 5.9s</small><br>
**AGENT**

> Running /commit-log-then-end: checking the registry result, then saving and committing this chat log.

<small>took 28.0s</small><br>
**AGENT**

> **Done: 0.12.0 is live on npm** (main package became `latest` at 23:38 UTC, all 7 packages visible).
>
> Not pushed: 16 commits remain local on main, per your choice. Run `git push` when ready.
>
> Left for later, one each: 1) swe-swe reads the nudge from `agent_waiting` (task filed at /workspace/tasks/2026-09-05-agent-chat-owns-the-nudge-text.md), 2) release-flow note in repo docs.
>
> Now committing this chat log and ending the session.

