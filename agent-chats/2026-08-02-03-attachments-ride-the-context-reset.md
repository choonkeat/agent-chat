<!-- agent-chat export
title: Attachments Ride The Context Reset
date: 2026-08-02
index: 03
slug: attachments-ride-the-context-reset
session: 64f26c8ab65ba354
agent: claude
version: 0.8.22 (07df0ae)
-->

# Attachments Ride The Context Reset

_2026-08-02 · 03 · claude · agent-chat 0.8.22 (07df0ae)_

**USER**

> Wasn’t thinking straight. Default the experimental feature to false

<small>took 18.4s</small><br>
**AGENT**

> Flipping the "conversation context only" setting to off by default — finding where the default is set.

<small>took 2m 3s</small><br>
**AGENT**

> Default flipped in main.go, app.js, index.html copy, CHANGELOG and 4 E2E tests. Rebuilding and running `make test` — about 5 minutes.

**USER**

> Oh did we make a release of the experimental feature default true already?

<small>took 13.8s</small><br>
**AGENT**

> Checking the release history to see if a published version shipped with the setting turned on.

<small>took 29.5s</small><br>
**AGENT**

> No. It was never released.
>
> The last version we published is 0.8.22. The experimental setting was built after that, and everything since then only exists on your machine — 13 changes sitting in the project, none of them published. So nobody outside has ever had it turned on for them, and there is nothing to take back.
>
> Now finishing what I promised last turn: rebuilding and running the full test suite on the flipped default. About 5 minutes.

<small>took 10m 17s</small><br>
**AGENT**

> Build passed. Tests still running past 10 minutes — letting them finish in the background.

<small>took 38.5s</small><br>
**AGENT**

> Test run passed 13 minutes — still going. Verified the flip itself in the meantime: the setting starts off in the server, in the browser, and in the release notes. Waiting on the tests to confirm nothing else depended on the old default.

<small>took 18.1s</small><br>
**AGENT**

> No release ever went out with it on.
>
> The last published version is 0.8.22. The experimental setting was built after that and has never been published — so no user outside this machine has ever had it switched on, and there is nothing to take back or warn anyone about.
>
> The change to turn it off is done and checked: it now starts off in the server, off in the browser, and the release notes say "off by default". The test run is still going (about 15 minutes in, longer than the 5 I estimated) — I'll report the result as soon as it lands.
>
> Next: nothing for you to do until the tests finish.

[Quick replies]
- OK, tell me when tests finish
- Why are the tests so slow?
- Commit it once tests pass

<small>took 5m 47s</small><br>
**AGENT**

> Test run finished, but most failures are the known flaky connection problem in this container, not the change. Re-running the affected tests now to separate real breakage from noise.

**USER**

> make build

<small>took 9.8s</small><br>
**AGENT**

> Running `make build` — this compiles the program for every computer type it supports, so it takes a few minutes.

<small>took 19.7s</small><br>
**AGENT**

> Build finished, no errors.
>
> It produced six copies of the program, one for each kind of computer it runs on: Mac (two chip types), Windows (two chip types), and Linux (two chip types). Each is about 11–12 megabytes.
>
> One thing to know: the copy this chat session is currently running was replaced by the new one on disk, but a running program keeps using the version it started with. So the setting change we just made — the experimental one now starting switched off — won't show up here until a new session is started.
>
> Next: open a new session if you want to see the off-by-default setting live; otherwise say the word and I'll pick the tests back up where I left them.

[Quick replies]
- Finish the tests
- I'll start a new session to check it
- Commit the change now

<small>took 2m 16s</small><br>
**AGENT**

> Build done. Picking the tests back up now (I'd promised the result) — the browser connection is warmed and the full run is going. About 15 minutes.

**USER**

> > Messages with an attachment still send the ordinary way.
>
> Seems unnecessary

<small>took 10.7s</small><br>
**AGENT**

> Looking up where that sentence lives and what it's actually describing.

<small>took 19.2s</small><br>
**AGENT**

> Sentence removed from the settings panel. Rebuilding so the running app picks it up — about 30 seconds.

<small>took 17.7s</small><br>
**AGENT**

> Sentence removed. The setting's description now reads:
>
> > Every message — typed, tapped or spoken — resets the agent first and points it at this chat's log, exactly as if you had started it with `/clear `. The agent then knows only what this conversation says.
>
> You were right that it didn't belong. There are actually four kinds of message that keep working the ordinary way when the setting is on — attachments were only one of them — so singling that one out was both extra reading and a half-truth. The other three are quietly handled the same way and never needed explaining either.
>
> Nothing about how it behaves changed: sending a file still works exactly as before. Only the words on screen are shorter. The program has been rebuilt, so a newly started session will show the shorter text; this one keeps the old wording until it's restarted.
>
> Next: nothing to do unless you want the fuller "what stays normal" list written somewhere — the release notes already carry it.

[Quick replies]
- Good, leave it there
- Show me the release-notes wording
- Commit this
- Back to the tests

<small>took 16m 48s</small><br>
**AGENT**

> The earlier test run finished: 18 passed, the rest failed to start (the known connection problem in this container). Re-warming the browser and re-running to get a clean answer — about 18 minutes.

<small>took 20.5s</small><br>
**AGENT**

> Sentence removed. The setting's description now reads:
>
> > Every message — typed, tapped or spoken — resets the agent first and points it at this chat's log, exactly as if you had started it with `/clear `. The agent then knows only what this conversation says.
>
> You were right that it didn't belong. Four kinds of message keep working the ordinary way when the setting is on — files were only one of them — so naming that one alone was both extra reading and a half-truth. The other three are handled the same way and never needed explaining either.
>
> Nothing about the behaviour changed: sending a file still works exactly as before. Only the words on screen are shorter. The program has been rebuilt; a newly started session shows the shorter text, this one keeps the old wording until it is restarted.
>
> Separately, the test run I owed you: it finished, and 18 tests passed while the rest never got to start because of the connection problem this container has — not a fault in the change. I have restarted it properly. About 18 minutes; I will report the result.
>
> Next: nothing to do until that lands.

[Quick replies]
- OK, tell me when tests finish
- Commit this now
- Show me the release-notes wording
- Skip the tests

**USER**

> I don’t understand why messages with attachments (and the other 3) scenarios still send the normal way when the checkbox is ticked — what makes them unable to leverage in the /clear method

<small>took 11.2s</small><br>
**AGENT**

> Reading the send code to find the four cases that skip the reset, and why.

<small>took 1m 22s</small><br>
**AGENT**

> Three of the four have a real reason. The fourth — attachments — is just unfinished work, not a limitation.
>
> First, what the tick actually does when you send a message. It is a sequence, not a single step:
>
> 1. It tells the outer page to type into the agent's terminal window: stop, then `/clear`, then Enter. That wipes the agent's memory of the conversation.
> 2. It waits about two seconds for that to take effect.
> 3. It sends your message to the server, which holds it for the fresh agent to collect.
> 4. It types one more line into the terminal, pointing the new agent at this chat's log file and telling it to go collect your message.
>
> Now the four exceptions.
>
> **1. "stop" and the words like it.** The whole point of saying stop is to reach the agent that is currently working and tell it to quit. Step 1 above would erase that agent before it ever heard you — you would get silence instead of a stop. The reset also takes those two seconds, and stop is the one message that has to land immediately.
>
> **2. "clear context", and the yes/no that answers it.** That is the older, manual way of doing the same reset, and it puts a question on screen asking you to confirm. If the tick re-routed it, the agent would be wiped first and then handed the word "yes" — an answer to a question that is no longer being asked, and that the new agent has never seen.
>
> **3. A message you deliberately started with `/clear ` yourself.** The tick works by putting `/clear ` in front of what you typed. Do that to a message that already begins with `/clear ` and you get `/clear /clear fix the tests` — the second one stops being a command and becomes part of the words handed over. Nothing is lost by leaving it alone: your typed version already does exactly what the tick would have done.
>
> **4. Attachments.** This one has no good reason. When you attach a file it is uploaded to the server first, and your message only carries a reference to it — a short pointer to where the file now lives. The message the reset route sends at step 3 has a slot for those pointers, the server has accepted them there since the day the reset was built, and the browser simply never fills it in. So it was left out, and rather than send a broken message the code sends attachments the ordinary way instead.
>
> The practical effect of number 4: with the tick on, a message with a file attached does not reset the agent, so that one message still carries whatever the agent already knew. The other three are deliberate and should stay.
>
> Next: want me to wire attachments through the reset route so all messages behave the same? About 40 minutes including a test.

[Quick replies]
- Yes, wire attachments through
- Leave it, note it in the settings text
- How would the reset carry a file?
- Back to the tests

**USER**

> Yes, wire attachments through. It should have no difference with other messages since attachments are just suffixed full paths right?

<small>took 10.3s</small><br>
**AGENT**

> Wiring attachments through the reset route. Reading the send code first to confirm what an attachment actually carries.

<small>took 1m 7s</small><br>
**AGENT**

> Confirmed your reading: an attachment travels as a file path appended after your words, and the server already accepts paths on the reset route. Editing the browser code now.

<small>took 1m 47s</small><br>
**AGENT**

> Code and test written. Rebuilding, then running the reset tests — about 4 minutes.

<small>took 1m 21s</small><br>
**AGENT**

> First run hit the container's known connection fault before the page even loaded — re-warming the browser and retrying.

<small>took 6m 11s</small><br>
**AGENT**

> Go tests pass. Browser tests can't run: Chrome cannot reach any local address right now — even a plain page on port 3000 is refused, so every test fails before it opens the chat. Trying once more.

<small>took 1m 53s</small><br>
**AGENT**

> Attachments now go through the reset like every other message. Your reading was right.
>
> **What an attachment actually is.** When you pick a file, it is uploaded and saved before you press send. Your message then travels carrying only the file's location on disk — a line of text. The program adds that line under your words when it hands the message over. So there was nothing about a file that the reset could not carry, and the reset message has had a slot for those locations since the day it was written. The browser simply never filled the slot in, and rather than send something half-filled, it fell back to the ordinary route. That is the whole of it: unfinished work, not a limit.
>
> **What changed** (3 files):
> 1. The browser now puts the file locations into the reset message instead of stepping aside.
> 2. A message that is nothing but a file — no words — now gets drawn on screen straight away, the same as any other.
> 3. The attachment strip below the box is emptied whichever route the message took, so a file cannot ride along again with your next message.
>
> **Checked:** the program builds, and all the non-browser tests pass.
>
> **Not checked:** the browser tests. Nothing in this container can reach a local address from the browser at the moment — I tried a plain blank page and it was refused too, so every test fails before it even opens the chat. It is the same fault that ate the earlier runs today, and it is unrelated to this change. I wrote a test for the new behaviour and it is sitting ready to run.
>
> One honest gap, and it was there before today: if the connection dies in the two seconds the reset takes, your words come back to the box but an attached file has to be picked again. Ordinary sends have always behaved that way.
>
> Next: say "try the tests" and I will attempt the browser run again — it recovers on its own sometimes.

[Quick replies]
- Try the tests again
- Commit it
- Show me the code change
- Fix the lost-file-on-failure gap too

<small>took 3m 58s</small><br>
**AGENT**

> The older test run's result just landed: 39 passed, then the run died partway on the same connection fault. Nothing failed on its own merits. Still waiting on your reply above.

<small>took 10.8s</small><br>
**AGENT**

> Attachments now go through the reset like every other message. Your reading was right.
>
> **Why they could always have gone this way.** A file is uploaded and saved before you press send, so the message itself carries only the file's location on disk — a line of text. The program adds that line under your words when it hands the message over. The reset message has had a slot for those locations since the day it was written; the browser simply never filled it in, and rather than send something half-filled it took the ordinary route instead. Unfinished work, not a limit.
>
> **What changed** (3 files):
> 1. The browser now puts the file locations into the reset message instead of stepping aside.
> 2. A message that is nothing but a file — no words — is drawn on screen straight away, like any other.
> 3. The attachment strip below the box is emptied whichever route the message took, so a file cannot ride along again with your next message.
>
> **Checked:** the program builds; all non-browser tests pass.
>
> **Not checked:** the browser tests. Nothing here can reach a local address from the browser at the moment — even a plain blank page is refused — so every test fails before it opens the chat. An older run that finished in the meantime shows the same thing: 39 passed, then it died partway on that fault. Nothing failed on its own merits. The test I wrote for the new behaviour is ready to run.
>
> One honest gap, present before today: if the connection dies during the two seconds the reset takes, your words come back to the box but an attached file has to be picked again. Ordinary sends have always behaved that way.
>
> Next: say "try the tests" and I will attempt the browser run again — it recovers on its own sometimes.

[Quick replies]
- Try the tests again
- Commit it
- Show me the code change
- Fix the lost-file-on-failure gap too

**USER**

> /commit-log-then-end

<small>took 3.3s</small><br>
**AGENT**

> Running /commit-log-then-end — freezing this chat's log, committing it, then ending the session.

