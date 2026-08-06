<!-- agent-chat export
title: Ding On Long Runs Audio Context Opened On Interaction
date: 2026-08-06
index: 02
slug: ding-on-long-runs-audio-context-opened-on-interaction
session: 1a3f7eee872a5494
agent: claude
version: 0.9.1 (517b133)
-->

# Ding On Long Runs Audio Context Opened On Interaction

_2026-08-06 · 02 · claude · agent-chat 0.9.1 (517b133)_

**USER**

> Are we running the latest code? Want to test

<small>took 12.8s</small><br>
**AGENT**

> Checking whether the running server matches the current code.

<small>took 1m 23s</small><br>
**AGENT**

> **Yes — this chat is running the newest code.**
>
> Reload this page (Cmd/Ctrl+R), then open Settings. You should see the two newest additions: the per-chat "conversation context only" switch, and the ding sound on long runs. Takes under a minute.
>
> How I confirmed it, 3 checks:
> 1. The program file was rebuilt today at 13:02:47 — after the last code edit at 13:02:36.
> 2. This chat's server started at 13:11, after that rebuild, so it loaded the new version.
> 3. Nothing has been edited since; the saved project state and the files on disk match exactly.
>
> Two other chats on this machine started at 09:38 and 12:31 — both before the rebuild. They're still running the older version until they're restarted.
>
> Separately: want me to run the full automated test suite too (a few minutes)?

[Quick replies]
- Run the test suite
- I'll reload and check first
- Both

**USER**

> I want to test the ding. You report progress then sleep for a while then send message I guess?

<small>took 12.8s</small><br>
**AGENT**

> Starting the ding test — checking how long a run has to be before it dings, then waiting that long.

<small>took 36.7s</small><br>
**AGENT**

> Turn up your volume now — the sound plays when my next message lands, about 40 seconds from now.
>
> The rule: a run has to last at least 20 seconds before it makes a sound. Short answers stay silent on purpose. I'm padding this one well past that.

<small>took 50.6s</small><br>
**AGENT**

> **Did you hear two quick rising notes just now?**
>
> That was the sound. It played the moment this message arrived, because the run lasted longer than 20 seconds.
>
> How it decides:
> 1. The run must take at least 20 seconds. Anything faster stays silent — you were still watching the screen anyway.
> 2. It only plays for a run happening right now. Reopening or reloading the chat replays past messages without any sound.
> 3. You can switch it off in Settings, and that choice is remembered on this browser.
>
> If you heard nothing: check the tab isn't muted, then check the Settings switch is on, then say so and I'll run it again.

[Quick replies]
- Heard it
- Heard nothing
- Run it again
- Too quiet

**USER**

> Heard nothing

<small>took 11.2s</small><br>
**AGENT**

> Looking at the sound code to find why nothing played.

**USER**

> When I toggle the “ Ding when a long reply lands ” option I hear a sound

<small>took 14.2s</small><br>
**AGENT**

> Sound itself works, so the problem is the trigger. Reading the code that decides when to play it.

<small>took 1m 38s</small><br>
**AGENT**

> Code logic reads correct, so I'm reproducing it in a test browser to see what actually happens when a long run ends. About 2 minutes.

<small>took 5m 17s</small><br>
**AGENT**

> Found the likely cause: the sound is built fresh at the moment it plays, and browsers block that when you're not touching the page — the exact moment the ding is for. Rewriting it to build the sound while you're active and keep it ready.

<small>took 10m 47s</small><br>
**AGENT**

> Fix written and the app rebuilt. Test suite is running now (about 5 more minutes).

<small>took 44.2s</small><br>
**AGENT**

> Still running the test suite — no results yet. Next update when it finishes.

<small>took 11m 55s</small><br>
**AGENT**

> **Start a new chat session, then ask for something that takes over 20 seconds.** This window keeps the old version until you do — the fix can't reach it.
>
> What was wrong: the sound was built from scratch at the exact moment it needed to play. Browsers refuse to start new sound at that moment unless you've just touched the page — and the whole point of the ding is that you haven't. Toggling the switch worked because clicking the switch *is* touching the page.
>
> What changed: the sound is now prepared the first time you click or type in the chat, kept ready, and reused every time after that.
>
> Checks done: all 7 ding tests pass, including a new one that fails if the old behaviour returns. 40 other tests failed on a known connection fault in the test setup, unrelated to this change.
>
> Separately: want me to commit this?

[Quick replies]
- Starting a new session now
- Commit it
- Still no sound
- Explain the browser rule

**USER**

> Commit it

**USER**

> Make build

<small>took 13.3s</small><br>
**AGENT**

> Committing the ding fix, then running `make build`.

**USER**

> /commit-log-then-end

<small>took 12.0s</small><br>
**AGENT**

> Step 1 of 2 done: committed (07efcf3). Running `make build` now — it builds for every platform, so about 2–4 minutes.

