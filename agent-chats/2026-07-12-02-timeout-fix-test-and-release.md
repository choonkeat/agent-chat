<!-- agent-chat export
title: Timeout Fix Test And Release
date: 2026-07-12
index: 02
slug: timeout-fix-test-and-release
agent: claude
version: 0.8.9 (6301293)
-->

# Timeout Fix Test And Release

_2026-07-12 · 02 · claude · agent-chat 0.8.9 (6301293)_

**USER**

> This chat is
>
> > [system] Connected · agent-chat · v0.8.9 (6301293)
>
> And I’m trying to test our timeout issue fix

<small>took 10.7s</small><br>
**AGENT**

> Got you — connected on v0.8.9 (6301293), which is exactly the build with the idle-abort reply-loss fix. I'm here and listening.
>
> To test the timeout fix, a good exercise: I can kick off a longer task, and you send a reply that would previously get lost across the idle-abort window. What would you like to try?

[Quick replies]
- Run a long task, then I'll reply mid-way
- Just checking the connection — nothing needed
- Explain what the fix actually does

**USER**

> No need. I’ll leave this chat idle. What’s the duration when things break/timeout?

<small>took 18.0s</small><br>
**AGENT**

> The relevant timeout is Claude Code's stdio idle abort: **`CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT`, default 30 minutes** (`tools.go:127`).
>
> That's the window where a blocking MCP call (like `send_message` waiting on your reply) gets aborted if nothing is sent on the stdio channel — and critically, that abort fires *no* cancelled notification, which is what used to orphan the server-side wait and lose your reply.
>
> **The fix (commit 6301293)** keeps the call alive so it never actually hits that 30-min wall:
> - A **60-second progress keepalive** (`progressKeepaliveInterval`, `tools.go:133`) — every 60s the blocking call emits an MCP progress ping, which *resets* the 30-min idle window. So `send_message` can block on you indefinitely.
> - **Limbo redelivery** — if a delivery does die in transit, the message is re-handed to me on the next `check_messages` behind a `---REDELIVERY---` sentinel, so nothing is silently dropped.
>
> So leaving this chat idle should *not* break anything now — the 60s heartbeat keeps resetting the 30-min timer. If you idle long enough and it still holds, that's the fix working. (One known-still-open gap: a full server restart wipes the in-memory queue.)

[Quick replies]
- Good — I'll idle and see if it holds
- What's the restart-wipes-queue gap about?
- Nothing else, thanks

**USER**

> I’m back from idle. If you see and respond to this correctly, you’ll say Hello 123456

<small>took 4.4s</small><br>
**AGENT**

> Hello 123456 ✅
>
> Your reply after idling came through cleanly and was routed to me correctly — the timeout fix is working. The `send_message` call held across your idle period and delivered your reply without loss.

[Quick replies]
- Great, fix confirmed
- Let's try a longer idle
- That's all, thanks

**USER**

> Love it. Let’s git commit and push if we haven’t. And /npm-login-and-publish

<small>took 23.7s</small><br>
**AGENT**

> Git is already committed & pushed (main up to date with origin; fix 6301293 + doc 897c229 both on origin). Nothing to commit — .claude/ and .pi/ are local tooling dirs, leaving untracked.
>
> Heads up: published npm 0.8.9 predates the fix, so we do need a version bump to actually ship it. Starting /npm-login-and-publish now…

<small>took 14.4s</small><br>
**AGENT**

> Ready to release **@choonkeat/agent-chat** (7 packages: main + 6 platform binaries).
>
> Current published: **0.8.9** · local package.json: 0.8.9
>
> The unreleased commits on top of 0.8.9 are the idle-abort reply-loss fix (bugfix), so I'd recommend a patch bump to **0.8.10**. What version should I publish?

[Quick replies]
- 0.8.10
- 0.9.0
- Let me specify a version

**USER**

> 0.8.10

<small>took 18.8s</small><br>
**AGENT**

> 👋 Checked — no new messages (just a redelivery of your earlier "0.8.10", already handled).
>
> Release progress so far:
> ✅ Bumped to **0.8.10** — root package.json + all 6 optionalDependencies synced to 0.8.10
>
> Next up: commit & push, verify npm auth, build platform binaries, then I'll ask you for a fresh OTP to publish. Want me to keep going, or hold here?

[Quick replies]
- Keep going
- Hold here

**USER**

> Keep going

<small>took 27.7s</small><br>
**AGENT**

> ✅ Committed & pushed **0.8.10** (`80c7515`, on origin/main).
>
> ⚠️ npm token is dead (E401) — please complete a web login:
>
> **‹npm-login-url redacted›**
>
> Meanwhile I'm cross-compiling all 6 platform binaries in the background. Once you've logged in and the build finishes, I'll ask you for a fresh 6-digit OTP to publish.
>
> Reply here once you've completed the login.

[Quick replies]
- Logged in
- Login failed
- Waiting on the page

**USER**

> Shucks I was late. Give me another login url

<small>took 21.5s</small><br>
**AGENT**

> No worries — here's a fresh login URL:
>
> **‹npm-login-url redacted›**
>
> ✅ Build done — all 6 platform binaries cross-compiled and stamped 0.8.10.
>
> Once you've logged in, reply and I'll immediately ask for a fresh 6-digit OTP to publish (I'll hold off requesting it until you confirm, so the OTP window doesn't expire).

[Quick replies]
- Logged in
- Give me another URL
- Login failed

**USER**

> Logged in

<small>took 7.1s</small><br>
**AGENT**

> ✅ Logged in as **choonkeat**. Everything's staged for publish (bumped, committed, pushed, built).
>
> Please send me a **fresh 6-digit OTP** from your authenticator and I'll publish all 7 packages immediately.

[Quick replies]
- Here's the OTP: 

**USER**

> ‹otp redacted›

<small>took 1m 5s</small><br>
**AGENT**

> 🎉 **Published @choonkeat/agent-chat 0.8.10** — all 7 packages verified live on npm:
>
> - `@choonkeat/agent-chat@0.8.10` (main)
> - `@choonkeat/agent-chat-darwin-arm64@0.8.10`
> - `@choonkeat/agent-chat-darwin-x64@0.8.10`
> - `@choonkeat/agent-chat-linux-arm64@0.8.10`
> - `@choonkeat/agent-chat-linux-x64@0.8.10`
> - `@choonkeat/agent-chat-win32-arm64@0.8.10`
> - `@choonkeat/agent-chat-win32-x64@0.8.10`
>
> This release ships the idle-abort reply-loss fix (6301293) that we just tested end-to-end — the 60s progress keepalive + limbo redelivery that kept your post-idle "Hello 123456" reply alive. Git is committed & pushed (`80c7515` on origin/main).
>
> Nice run — the whole release was driven through the very fix it delivers. Anything else?

[Quick replies]
- That's all, thanks
- Update the changelog too
- Draft release notes

**USER**

> Update the changelog too

<small>took 22.8s</small><br>
**AGENT**

> ✅ Changelog updated & pushed (`e79357b` on origin/main).
>
> Added a **[0.8.10] — 2026-07-12** section under Fixes documenting the idle-abort reply-loss fix: the 60s progress keepalive resetting the ~30-min stdio idle window, the single-active-waiter guard, and the `---REDELIVERY---` redelivery path.
>
> Release fully wrapped:
> - 📦 npm 0.8.10 — all 7 packages live
> - 🔖 `80c7515` release commit + `e79357b` changelog, both on origin/main
>
> Anything else?

[Quick replies]
- That's all, thanks
- Draft GitHub release notes

**USER**

> /ck:export-chat-logs

