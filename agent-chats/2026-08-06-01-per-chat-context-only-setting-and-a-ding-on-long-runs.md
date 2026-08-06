<!-- agent-chat export
title: Per Chat Context Only Setting And A Ding On Long Runs
date: 2026-08-06
index: 01
slug: per-chat-context-only-setting-and-a-ding-on-long-runs
session: 60184a74bb470509
agent: claude
version: 0.9.1 (7abf230)
-->

# Per Chat Context Only Setting And A Ding On Long Runs

_2026-08-06 · 01 · claude · agent-chat 0.9.1 (7abf230)_

**USER**

> 2 changes
> 1. Make “Conversation context only (experimental)” per chat if possible, instead of cookie affecting all sessions?
> 2. Add a Ding sound option when long running agent (amber/orange Send button) is done (button turns blue) 
>
> Discuss

<small>took 13.8s</small><br>
**AGENT**

> Reading the code for both: (1) where "Conversation context only" is stored, (2) how the Send button switches amber→blue.

<small>took 1m 10s</small><br>
**AGENT**

> Both are doable. One decision needed before I build — it's at the bottom.
>
> **1. "Conversation context only" per chat — yes, ~1 hour**
>
> Why it leaks across chats today: the tick is saved as a cookie (a small note the browser keeps for a whole website). Cookies are shared by every chat on this machine — they ignore the number that tells one chat apart from another. So ticking it once ticks it everywhere.
>
> Two ways to fix, ranked:
>
> 1. **Store the answer with the chat itself** (recommended). Each chat is its own little running program, so it can remember its own answer. Survives reloads, nothing piles up, no size limit.
> 2. Keep the cookie but stamp the chat's ID on it. Faster to write, but you accumulate one note per chat forever and the browser caps the total, so I'd have to add cleanup. Not worth it.
>
> **2. Ding when a long job finishes — yes, ~45 minutes**
>
> The Send button already knows both states, so I hook the moment amber turns blue. Three things I'd build in by default:
>
> 1. Only ding if the wait was longer than ~20 seconds — otherwise every one-line answer dings.
> 2. Sound made by the browser itself, no audio file to load.
> 3. An on/off switch in Settings, next to the other options.
>
> One caveat: browsers refuse to play sound until you've clicked inside the page. You always type a message first, so it should be fine — I'll confirm it actually plays inside the chat panel before calling it done.
>
> **Decision (30 seconds):** when you open a brand-new chat, should "Conversation context only" start **off every time**, or **start as whatever you last chose** and let you override it per chat?

[Quick replies]
- Start as whatever I last chose
- Start off every time
- Build both changes now
- Just do the ding first

**USER**

> Build both changes now

<small>took 21.9s</small><br>
**AGENT**

> Building both. Going with: new chats start from your last choice, then each chat keeps its own. Step 1 of 4: reading the chat-state and replay code.

<small>took 2m 20s</small><br>
**AGENT**

> Step 1 of 4 done: "Conversation context only" now lives with each chat (server-held), cookie demoted to the seed for new chats. Next: the ding.

<small>took 1m 44s</small><br>
**AGENT**

> Step 2 of 4 done: ding built (20s threshold, on by default, switch in Settings, plays once when you turn it on). Next: unit tests, then E2E.

<small>took 3m 22s</small><br>
**AGENT**

> Step 3 of 4 done: 20 targeted browser tests pass (6 new ding tests, 14 context-only). Now running the full suite.

<small>took 12m 52s</small><br>
**AGENT**

> Both changes are done. **Start a new chat to see them** — this one is still running the old binary.
>
> **Step 4 of 4 done. What now works:**
>
> 1. **"Conversation context only" is per chat.** Each chat keeps its own answer, held by the chat itself, so a reload or a second tab on the same chat agrees. Ticking it here no longer reaches into chats already open. The old shared setting became the seed: a brand-new chat opens with whatever you last chose.
> 2. **Ding when a long reply lands.** Two short rising notes the moment the Send button goes from amber back to blue, for runs over 20 seconds. Under 20 seconds it stays silent, so one-line answers never chirp. It's on already; the switch is in Settings, under the context-only box, and flipping it on plays the tone once so you can hear it.
>
> **Tests:** 143 of 144 browser tests pass plus all Go tests. The one failure was an unrelated image test that passes on its own — the known port flakiness, not this change. 6 new tests cover the ding; 1 existing test changed on purpose, because it asserted the old "ticking reaches every chat" behaviour.
>
> **Next step (30 seconds):** open a new chat, click the gear, and check the ding box is there — then let the next long run finish and tell me if 20 seconds is the right cut-off.
>
> Separately: nothing is committed yet. Want me to commit these 6 files?

[Quick replies]
- Commit it
- Wait, let me test first
- Make the threshold 30s
- Show me the diff

**USER**

> make build so I can test locally in new session

<small>took 5.0s</small><br>
**AGENT**

> Running make build now — cross-compiles every platform, about 2 minutes.

<small>took 27.5s</small><br>
**AGENT**

> Build done — start a new session now and both changes will be live.
>
> **To test in the new session:**
>
> 1. Click the gear icon. You'll see two boxes: "Conversation context only" (now labelled **this chat**) and the new **"Ding when a long reply lands"**.
> 2. Tick the ding box — it plays the tone immediately so you can hear it.
> 3. Give me a task that takes over 20 seconds, look away, and listen.
> 4. For the per-chat check: tick "Conversation context only" in the new session, then come back to this chat's gear — it should still be off here.
>
> All six platforms rebuilt and the cached copy that new sessions launch was refreshed.
>
> Nothing is committed yet — six files changed. Commit them?

[Quick replies]
- Commit it
- Wait, testing first
- Show me the diff

**USER**

> Commit it

<small>took 2.8s</small><br>
**AGENT**

> Committing the six changed files by name (no chat logs, no .pi/).

<small>took 21.3s</small><br>
**AGENT**

> Committed as `17b3a16` on main, 7 files. Not pushed — say the word if you want it on the remote.
>
> Next step (30 seconds): start a new session, open the gear, tick the ding box to hear the tone.

[Quick replies]
- Push it
- Leave it local
- Testing now

**USER**

> /commit-log-then-end

<small>took 10.5s</small><br>
**AGENT**

> Saw an edit to the ding test that requires one shared audio context. Making the code match before saving the log.

