<!-- agent-chat export
title: What 0 9 0 Adds Over 0 8 22
date: 2026-08-03
index: 01
slug: what-0-9-0-adds-over-0-8-22
session: d83779efd5de405b
agent: claude
version: 0.9.0 (9a2df86)
-->

# What 0 9 0 Adds Over 0 8 22

_2026-08-03 · 01 · claude · agent-chat 0.9.0 (9a2df86)_

**USER**

> Checking if I’m using the latest code build

<small>took 16.5s</small><br>
**AGENT**

> Checking which agent-chat binary is running and whether it matches the newest code.

<small>took 22.8s</small><br>
**AGENT**

> Yes — you're on the latest build. Nothing to do.
>
> How I know:
> 1. The newest saved version of the code is from today, 01:25:29.
> 2. The program you're talking to right now was built 3 seconds later, at 01:25:32, and started at 01:25:52.
> 3. The file that's running is an exact byte-for-byte match of the one just built — same size, same fingerprint.
> 4. There are no unsaved code edits sitting around. The only files not yet saved into history are chat transcripts, which don't affect the program.
>
> Separately: an older window from yesterday afternoon is still running an out-of-date version (0.8.22). It's harmless, but it won't have today's fixes. Want me to shut it down?

[Quick replies]
- Yes, shut down the old one
- Leave it running
- What's in 0.9.0 that 0.8.22 lacks?

**USER**

> What's in 0.9.0 that 0.8.22 lacks?

<small>took 14.5s</small><br>
**AGENT**

> Reading the change log between 0.8.22 and 0.9.0.

<small>took 23.5s</small><br>
**AGENT**

> 0.9.0 adds one big feature, and three smaller fixes around it. Here it is, plain:
>
> **1. You can now wipe the agent's memory from the chat box.**
> Type `/clear` followed by an instruction — for example `/clear now check the tests`. The agent forgets everything it was holding in its head, then comes back and does what you asked. It's not starting blind: it's handed the written transcript of your conversation, so it can read back what was said. A bare `/clear` with nothing after it just wipes and waits.
>
> **2. A checkbox that does that automatically, every message.**
> It's in the message-style panel, labelled "Conversation context only", and it's off unless you tick it. With it on, the agent is wiped before every single message you send — so it only knows what the conversation actually says, never things it worked out silently and never wrote down.
>
> Why you'd want it: on a real ten-turn session, this kept the agent's memory load flat at about 40,000 units while the same work without it climbed past 335,000. That's 56.6% less reading, and roughly 45% less billed. It's marked experimental because wiping the agent every message is a big change in how it behaves.
>
> Three kinds of message ignore the checkbox on purpose: telling it to stop, asking it to clear context, and anything you already typed `/clear` in front of.
>
> **3. Three things that were broken now work.**
> - A message sent with the checkbox on used to disappear for about two seconds before showing up. It now appears the moment you hit send.
> - Typing a bare `/clear` and pressing Enter did nothing at all — the autocomplete dropdown ate the keypress.
> - On iOS, pasting a file path after typing `@` inserted a stray space and closed the dropdown.
>
> One note on the numbers: your running build is slightly newer than 0.9.0 — it also has two unreleased changes about when a message shows as read.
>
> Next: want me to shut down the old 0.8.22 window that's still running?

[Quick replies]
- Yes, shut it down
- Leave it running
- Show me the checkbox
- How do I turn the checkbox on?

**USER**

> /commit-log-then-end

<small>took 2.5s</small><br>
**AGENT**

> Running /commit-log-then-end: freezing this chat log, then committing it.

