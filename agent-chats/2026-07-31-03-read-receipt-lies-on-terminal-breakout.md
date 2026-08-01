<!-- agent-chat export
title: Read Receipt Lies On Terminal Breakout
date: 2026-07-31
index: 03
slug: read-receipt-lies-on-terminal-breakout
session: d3688ebe7ee98c05
agent: claude
version: 0.8.21 (ad470fa)
-->

# Read Receipt Lies On Terminal Breakout

_2026-07-31 · 03 · claude · agent-chat 0.8.21 (ad470fa)_

**USER**

> Sometimes I go to Agent Terminal and break out of its tool call waiting for agent chat. Then when I reply on agent chat, my bubble immediately turns blue (read) but on agent terminal side the agent doesn’t actually know whilst I can’t do anything on agent chat side either — now I must manually type “check message” on agent terminal
>
> The right behaviour would’ve been my message isn’t least unread then I know there’s a disconnect and send as interrupting.
>
> Discuss

<small>took 1m 44s</small><br>
**AGENT**

> **Next: approve fix 1 — "blue only when the agent really has it." About half a day.**
>
> You diagnosed it correctly. Here's what's actually going on.
>
> ## Why the bubble turns blue
>
> 1. When the agent asks you something, it leaves a standing request with the chat server: *hand me the next thing the user says.*
> 2. Pressing Esc in Agent Terminal stops the agent from listening — but nothing tells the chat server. The standing request is still sitting there with nobody behind it.
> 3. Your next reply gets handed to that abandoned request. **The hand-over is what turns the bubble blue.**
> 4. The reply goes nowhere. The server does keep a spare copy, which is why typing `check_messages` recovers it.
>
> So today blue means "handed over," not "the agent has it." You're right that it should mean the second one.
>
> ## What already exists but you can't reach
>
> A dim (unread) bubble has a "⋯" menu with an interrupt option — it presses Esc twice in the terminal and types `check_messages` for you. That is exactly the behaviour you want. It's hidden in your case *because the bubble went blue*, so the escape hatch disappears at the one moment you need it.
>
> ## Fix 1 — repair the colour (agent chat only)
>
> Flip the bubble to blue only when the agent takes its next action, not when the message is handed over. The server already tracks that signal; it just isn't wired to the colour.
>
> - Cost: on a normal reply the bubble stays dim for one extra beat, usually under a second.
> - Gain: dim = genuine disconnect signal, and the interrupt button is sitting right there on that bubble.
>
> ## Fix 2 — auto-recover (needs the terminal side too)
>
> Have Agent Terminal tell the chat window whenever the agent returns to its prompt. Chat then knows nobody is listening and fires the interrupt itself — no click, no typing.
>
> - Cost: a change in the other repo, and it does nothing when chat runs without that terminal.
>
> Fix 1 makes the problem visible and manually fixable. Fix 2 makes it invisible. Fix 1 first, because fix 2 is worthless if the colour still lies.
>
> **Reply "do fix 1" and I'll implement and test it.**

[Quick replies]
- Do fix 1
- Do both
- More detail on fix 2
- Not yet — let me think

**USER**

> > Flip the bubble to blue only when the agent takes its next action, not when the message is handed over.
>
> How? I understand the user reply is return value of send_message. Unless we stick it into the unread queue and return some other instruction to the agent to pick it up manually?

<small>took 40.9s</small><br>
**AGENT**

> **Next: approve a three-state bubble instead of two. Still about half a day.**
>
> Short answer to your question: we don't change delivery at all. Your reply still comes back as the return value of `send_message`, exactly as now. The colour is not the return value — it's a separate note the server pushes to your browser. Today both fire at the same instant. We just move the colour note later.
>
> ## The three states
>
> 1. **Dim** — sitting in the queue, nobody has picked it up.
> 2. **Half (outline / grey tick)** — handed to the agent's waiting request. Honest statement: it left, nothing has confirmed it arrived.
> 3. **Blue** — the agent did something in this chat *after* receiving it. That's the confirmation.
>
> ## Why state 3 is knowable
>
> Every time the agent calls any agent-chat tool, the server already records "the previous hand-over evidently arrived" — that line exists today and drives the spare-copy recovery. We reuse the same signal to drive the colour. Nothing new to detect.
>
> ## What you'd see in your broken case
>
> Your reply stops at state 2 and stays there. That is the disconnect signal you asked for. The "⋯" interrupt button stays available on state-2 bubbles, so one click sends Esc-Esc plus `check_messages` instead of you typing it.
>
> ## One honest limitation
>
> If you break out, then later make the agent do anything else in this chat, that counts as confirmation and the bubble goes blue even though the agent never read your text. Narrower than today's failure, and the spare copy still holds your message.
>
> ## The work
>
> 1. Server: stop treating hand-over as "read"; emit a "handed over" note instead.
> 2. Server: emit the "read" note when the agent's next tool call arrives.
> 3. Browser: add the middle style, keep the interrupt button on it, and make reconnecting tabs rebuild the right state.
>
> **Reply "go" and I'll build it.**

[Quick replies]
- Go
- Skip the middle state — just dim vs blue
- How does a reconnecting tab know?
- Not yet

**USER**

> Go

<small>took 10.9s</small><br>
**AGENT**

> **Still waiting on one word: "go" — or tell me to drop the middle state.**
>
> Queue had nothing new, just a redelivery of your "How?" question, which I answered in the previous message (three-state bubble: dim → handed over → read).
>
> Side note worth one line: that exchange just reproduced your bug. My answer reached you, then my waiting request was killed by the terminal nudge, so your reply had nowhere to land.
>
> **Reply "go" and I'll build it.**

**USER**

> Stop

<small>took 10.2s</small><br>
**AGENT**

> **Stopped. Nothing was changed — no files touched, no code written.**
>
> Your "Go" arrived and was superseded by "Stop" before I started.
>
> State: the three-state bubble plan (dim → handed over → read) is agreed on paper only.
>
> **What next?**

[Quick replies]
- Build it now
- Write the plan to a file first
- Different approach — let me explain
- Park it

**USER**

> Write detailed spec in tasks/

<small>took 1m 26s</small><br>
**AGENT**

> Code trace done. Writing the spec to `tasks/2026-08-01-read-receipt-lies-on-terminal-breakout.md` now — about 3 minutes.
>
> Found one thing that changes the design: `send_message` deliberately does **not** fire the "previous delivery arrived" signal (tools.go:340-344), so I can't reuse it for the colour. Spec uses a separate proof marker instead.

<small>took 1m 6s</small><br>
**AGENT**

> **Spec written: `tasks/2026-08-01-read-receipt-lies-on-terminal-breakout.md`** — open it and skim the "Design" table.
>
> Nothing else was changed. No code touched.
>
> ## One finding that changed the design
>
> The signal I planned to reuse for the colour turns out to be deliberately switched off for exactly the tool we need it on. `send_message` skips it on purpose (a repeat call might be a re-send after a lost delivery). If I had reused it, every normal reply would have got stuck at the middle state forever. The spec adds a separate marker instead and leaves the existing recovery logic untouched.
>
> ## What the spec covers
>
> 1. Why the bubble goes blue: the abandoned request grabs your message, and grabbing is what fires the "read" note.
> 2. Three states: queued (dim) → handed over (new outline style) → read (normal).
> 3. The exact files and line numbers to change — 3 files, about 10 small edits.
> 4. Five edge cases, including the honest one where the bubble still lies.
> 5. Tests to write, plus a manual four-step reproduction.
>
> Also noted in the spec: the "Send as interrupting" button already exists and already does the right thing. It just vanishes at the moment you need it, because it only lives on dim bubbles.
>
> **Reply "build it" and I'll implement the spec.**

[Quick replies]
- Build it
- Let me read the spec first
- Change the design — I'll explain
- Park it

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session (no uuid needed -- it ends the calling session). If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session. Afterwards, tell the user in one line: they can save their own /commit-log-then-end command to decide what this button actually does -- their own redaction rules, commit message wording, whether to push, what to leave out -- and swe-swe will run that instead of these instructions.

