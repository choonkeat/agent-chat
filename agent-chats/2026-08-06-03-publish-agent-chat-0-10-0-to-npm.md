<!-- agent-chat export
title: Publish Agent Chat 0 10 0 To Npm
date: 2026-08-06
index: 03
slug: publish-agent-chat-0-10-0-to-npm
session: 5519f11d2adcebe2
agent: claude
version: 0.9.1 (07efcf3)
-->

# Publish Agent Chat 0 10 0 To Npm

_2026-08-06 · 03 · claude · agent-chat 0.9.1 (07efcf3)_

**USER**

> /npm-login-and-publish

<small>took 9.5s</small><br>
**AGENT**

> Starting the npm release flow: changelog, version bump, commit, push, then publish.

<small>took 59.3s</small><br>
**AGENT**

> **Reply "go" to publish version 0.10.0.** Step 1 of 6 done: release notes drafted in CHANGELOG.md.
>
> Why 0.10.0 and not 0.9.2: there are new features, not just fixes. This project's rule is that new features move the middle number.
>
> Three things go out in this release:
>
> 1. **A ding when a long run finishes.** Runs over 20 seconds end with two short rising notes, so you can walk away and still know when the answer landed. On by default, switchable in Settings. (Also fixed since then: the sound was silent in a background tab.)
> 2. **"Conversation context only" now belongs to the chat, not the browser.** Ticking it in one chat used to tick it in every chat on this machine, including ones already running. Now each chat holds its own answer, and the setting only carries forward into new chats you start.
> 3. **A new tool called `agent_waiting`** -- it answers one question: "if I push a message right now, will the agent actually read it, or will it sit unread?" The browser already worked around this invisibly; automated callers had no way to tell.
>
> The full wording is in CHANGELOG.md, lines 5-36, if you want to read it before I commit.
>
> Next: say "go" and I'll bump the version, commit, push, build the six platform binaries, then ask you for a one-time npm code.

[Quick replies]
- go
- Show me the exact changelog text
- Make it 0.9.2 instead
- Wait -- let me read it first

**USER**

> go

<small>took 2.2s</small><br>
**AGENT**

> Step 2 of 6: bumping version to 0.10.0, then committing and pushing.

<small>took 46.5s</small><br>
**AGENT**

> **Open this link and sign in to npm:** [REDACTED]
>
> Steps 2 and 3 of 6 done: version bumped to 0.10.0, committed and pushed (f0519c9).
>
> Your saved npm sign-in had expired, which is why the link is needed. Meanwhile I'm building the six program files (one per operating system) -- takes a couple of minutes. After that I'll ask you for a 6-digit code from your authenticator app.

<small>took 1m 31s</small><br>
**AGENT**

> **Send me the 6-digit code from your authenticator app.** It expires in about 30 seconds, so send it right when you read this.
>
> Step 5 of 6 done. Signed in as choonkeat, and all six program files are built and stamped 0.10.0.
>
> Next: I paste the code and publish all seven packages. Takes under a minute.

[Quick replies]
- Ready -- sending the code now

**USER**

> [REDACTED]

<small>took 1m 23s</small><br>
**AGENT**

> **Version 0.10.0 is live. Run `npx @choonkeat/agent-chat@0.10.0` to try it.**
>
> All 6 of 6 steps done. All seven packages confirmed on npm:
>
> 1. `@choonkeat/agent-chat` (the one people install)
> 2. `@choonkeat/agent-chat-darwin-arm64`
> 3. `@choonkeat/agent-chat-darwin-x64`
> 4. `@choonkeat/agent-chat-linux-arm64`
> 5. `@choonkeat/agent-chat-linux-x64`
> 6. `@choonkeat/agent-chat-win32-arm64` and `-win32-x64`
>
> Release commit f0519c9 is pushed to GitHub, so the code that produced this version is on the remote with its notes.
>
> Separately: three untracked chat-log files are sitting in `agent-chats/`. Commit them next?

[Quick replies]
- Yes, commit the chat logs
- No, leave them
- Show me what's in them first

**USER**

> Yes, commit the chat logs

<small>took 6.6s</small><br>
**AGENT**

> Committing the chat logs now -- freezing this session's log first so it commits complete.

