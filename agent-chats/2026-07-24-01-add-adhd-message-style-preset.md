<!-- agent-chat export
title: Add Adhd Message Style Preset
date: 2026-07-24
index: 01
slug: add-adhd-message-style-preset
session: e8e42b26feeb175b
agent: claude
version: dev (unknown)
-->

# Add Adhd Message Style Preset

_2026-07-24 · 01 · claude · agent-chat dev (unknown)_

**USER**

> What has changed recently?

<small>took 21.1s</small><br>
**AGENT**

> The big recent change is a **per-message style template** (commits fd9e15b + 00da6f5, Jul 24).
>
> 1. **Message-style templates** (fd9e15b) — a new settings panel stores a template in browser localStorage. Every outgoing message wraps the agent-facing text via a `{{message}}` placeholder (or prepends it as a preamble), while the chat bubble still shows your raw typed text. Template rides on the queued UserMessage, not the broadcast event, so bubbles render un-wrapped. Touches app.js, index.html, style.css, eventbus.go, main.go, tools.go + tests.
>
> 2. **"Don't use analogies"** (00da6f5) — added to the Non-technical preset, alongside the avoid-jargon instruction.
>
> 3. **Chatlog index churn fix** (ca240a0 + c208d62) — `index.html` now only rewritten at commit moments, not on every streaming append.
>
> Before those: **0.8.19 release** (1884f98) and the `make build` npx-cache refresh (22df520).
>
> Want the full diff of the template feature, or is the summary enough?

[Quick replies]
- Summary's enough
- Show me the template diff
- What's uncommitted right now?

**USER**

> See if we can be inspired by https://github.com/ayghri/i-have-adhd/blob/main/skills/i-have-adhd/SKILL.md for our message style. Eg add adhd

<small>took 47.7s</small><br>
**AGENT**

> Added an **ADHD** preset. Build OK. It sits between Direct and Off in the Message-style settings panel.
>
> The template distills all 10 rules from the i-have-adhd SKILL.md (lead with the action, number steps, restate state, concrete time estimates, cap lists at 5, no preamble/recap/closers) and wraps each message via `{{message}}`.
>
> Two files changed:
> - `client-dist/app.js` — `adhd` entry in `msgStylePresets`
> - `client-dist/index.html` — the `data-preset="adhd"` button
>
> Not yet live in this running session (server still on the old binary). Next: reload the chat UI at http://localhost:4001 after a restart to see the button, or I can commit the change now — which do you want?

[Quick replies]
- Commit it
- Show me the template text
- Leave it uncommitted

**USER**

> Show me the template text

<small>took 8.1s</small><br>
**AGENT**

> Here's the full `adhd` template (the `{{message}}` at the end is where your typed text gets substituted):
>
> ```
> Shape your reply for a reader with ADHD:
> 1. Lead with the next action — a command, path, or result, not context or a plan.
> 2. Number multi-step work; one bounded action per step.
> 3. End with ONE concrete next step doable in under two minutes.
> 4. Suppress tangents — finish the first thing, then offer the second as a separate question.
> 5. Restate state every turn ("step 3 of 5 done: X. Next: Y").
> 6. Give time estimates in concrete units ("about 15 minutes"), never "a bit".
> 7. Make completed work visible — what now works, and how to see it.
> 8. Matter-of-fact on errors: location, cause, fix. No "uh oh".
> 9. Cap lists at 5 items; past five, split into "do now" vs "later".
> 10. No preamble, no recap, no closers.
>
> {{message}}
> ```
>
> Next: reply "Commit it" and I'll commit both files, or ask me to tweak any rule.

[Quick replies]
- Commit it
- Tweak a rule
- Leave it uncommitted

**USER**

> That’s too long. Which makes me wonder if our template text approach should wrap every message or only every N message (and 1st message upon changing)?

<small>took 30.9s</small><br>
**AGENT**

> **Wrap every message, but keep templates short.** That's the correct default — don't switch to every-N.
>
> **Why every-message wins:** LLMs weight recent context. A style directive injected only on message 1 (or every 10th) decays — by the time you're deep in a conversation the model has drifted back to its default voice. "First message + every N" reintroduces exactly the drift the feature exists to prevent, and adds state (a per-session counter) for a worse result.
>
> **The real problem is length, not cadence.** A 12-line block repeated every message is wasteful *and* clutters — that's what you reacted to. Fix the content, not the mechanism:
>
> 1. Short templates (1–3 lines) cost ~nothing per message and steer reliably.
> 2. My ADHD preset duplicated all 10 rules that already live in your global CLAUDE.md — so the agent was getting them twice. A one-line nudge is enough.
>
> **If you truly want a long template without per-message cost**, the clean split is: full text on 1st-message-after-change + a short reminder every message after — never a plain every-N, which leaves gaps where the model forgets.
>
> Recommendation: I shorten the ADHD preset to one line and keep every-message wrapping. Want that?

[Quick replies]
- Shorten ADHD to one line
- Do the full+short hybrid instead
- Leave it, let me think

**USER**

> Shorten ADHD to one line

<small>took 17.4s</small><br>
**AGENT**

> Done. ADHD preset is now one line, build OK:
>
> ```
> Reply for a reader with ADHD: lead with the next action, number multi-step work, restate progress ("step 3 of 5 done"), give time estimates in concrete units, cap lists at 5, and cut all preamble, recap, and closers.
>
> {{message}}
> ```
>
> Kept every-message wrapping. Changed files: `client-dist/app.js` (template), `client-dist/index.html` (button — unchanged this round).
>
> Next: reply "Commit it" and I'll commit both files together.

[Quick replies]
- Commit it
- Trim it further
- Leave it uncommitted

**USER**

> Restore suppress tangent, remove time estimate and cap lists

<small>took 14.7s</small><br>
**AGENT**

> Updated, build OK:
>
> ```
> Reply for a reader with ADHD: lead with the next action, number multi-step work, restate progress ("step 3 of 5 done"), suppress tangents (finish one thing, then offer the next as a separate question), and cut all preamble, recap, and closers.
>
> {{message}}
> ```
>
> Restored suppress-tangents; dropped time estimates and cap-lists.
>
> Next: reply "Commit it" and I'll commit `app.js` + `index.html`.

[Quick replies]
- Commit it
- Tweak again
- Leave it uncommitted

**USER**

> Commit it

**USER**

> make build

<small>took 9.1s</small><br>
**AGENT**

> Committed: `7389bb4` — feat(settings): add ADHD message-style preset. Two files (`app.js`, `index.html`); binary is gitignored, so it's not in the commit.
>
> Not yet live in this running session — the server is still on the pre-build binary. To see the ADHD button, the server needs a restart, which I won't do unprompted (it would kill this chat channel).
>
> Next: want me to run `make test` before you push, or leave it here?

<small>took 20.0s</small><br>
**AGENT**

> `make build` done. All 6 platforms compiled and the npx-cache binary refreshed (`agent-chat-linux-x64@0.8.19`).
>
> The refresh script's own note: "start a NEW session to pick it up." This session's server keeps the old binary until then — a new swe-swe session will show the ADHD button.
>
> Next: reply "Run make test" to verify, or "Push it" to push commit 7389bb4.

[Quick replies]
- Run make test
- Push it
- Done for now

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session with uuid f2be9c83-ba54-488e-8c7e-c58ddf984d8f. If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session.

