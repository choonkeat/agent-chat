<!-- agent-chat export
title: Publish Status Check
date: 2026-07-18
index: 03
slug: publish-status-check
session: 661053820081c406
agent: claude
version: 0.8.14 (b858a6b)
-->

# Publish Status Check

_2026-07-18 · 03 · claude · agent-chat 0.8.14 (b858a6b)_

**USER**

> Have we published or latest changes pending /npm-login-and-publish?

<small>took 21.0s</small><br>
**AGENT**

> **Yes — we're fully published.** `@choonkeat/agent-chat` **0.8.14** went out to npm today at 09:41 UTC, matching the local `package.json` version.
>
> Since the release bump there's only one commit — `f34c598`, a session-log docs export — which contains no code changes, and it's already pushed to origin/main. The working tree just has the auto-export chat log churn (`agent-chats/index.html` + today's untitled log), which is expected from the streaming export feature.
>
> So no `/npm-login-and-publish` needed right now.

[Quick replies]
- Great, thanks
- Publish anyway
- Show what changed since 0.8.13

