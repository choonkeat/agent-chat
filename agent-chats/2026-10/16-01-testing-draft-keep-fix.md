<!-- agent-chat export
title: Testing Draft Keep Fix
date: 2026-10-16
index: 01
slug: testing-draft-keep-fix
session: e5ff0cab5973c727
agent: claude
version: 0.17.0 (6163230)
-->

# Testing Draft Keep Fix

_2026-10-16 · 01 · claude · agent-chat 0.17.0 (6163230)_

**USER**

> Testing our latest fix here

<small>took 9.8s</small><br>
**AGENT**

> Message received; checking what the latest fix covers so you know what to test.

<small>took 4.7s</small><br>
**AGENT**

> Your message got through and was marked read. Next: try the draft fix from commit 6163230. It takes about 2 minutes:
>
> 1. Type a few words in the box and **don't send**.
> 2. Reload the page. The words should still be there.
> 3. Leave the words unsent and ask me (or another tab) to post a message. Your text should stay put.
> 4. Send it. Reload again, and the box should be empty because sent words don't come back.
>
> Reply with the step number where it broke, or "all good".

[Quick replies]
- All good
- Step 2 broke
- Step 3 broke
- Step 4 broke

**USER**

> /swe-swe:commit-session-chat-log and /npm-login-and-publish

<small>took 3.4s</small><br>
**AGENT**

> Step 1 of 2: committing this session's chat log. Step 2 of 2 is the npm release.

<small>took 6.0s</small><br>
**AGENT**

> Got your message ("/swe-swe:commit-session-chat-log and /npm-login-and-publish") and I'm working on it. The queue is empty now, so there's nothing newer. Step 1 of 2: closing the chat log.

