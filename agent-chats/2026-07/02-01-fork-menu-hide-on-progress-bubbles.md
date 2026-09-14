<!-- agent-chat export
title: Fork Menu Hide On Progress Bubbles
date: 2026-07-02
index: 01
slug: fork-menu-hide-on-progress-bubbles
agent: claude
version: 0.8.5 (36d851e)
-->

# Fork Menu Hide On Progress Bubbles

_2026-07-02 · 01 · claude · agent-chat 0.8.5 (36d851e)_

**USER**

> Discuss @tasks/2026-07-01-fork-menu-hide-on-progress-bubbles.md

<small>took 12m 55s</small><br>
**AGENT**

> Read the task + traced the code. The diagnosis holds up, and it's a clean fix — smaller than the task write-up suggests. My notes:
>
> **Step 3 is already done.** `agent_tool_name` is on the `Event` struct with a JSON tag (`eventbus.go:65`, `json:"agent_tool_name,omitempty"`) and both `send_progress`/`send_verbal_progress` already stamp it (`tools.go:518/545`). So it's already on the wire for both history replay (`event.agent_tool_name`) and live WS (`data.agent_tool_name`). No server change needed.
>
> **Why Type isn't enough** — the 4-way matrix:
> | tool | event Type | forkable? |
> |---|---|---|
> | send_message | agentMessage | ✅ |
> | send_progress | agentMessage | ❌ |
> | send_verbal_reply | verbalReply | ✅ |
> | send_verbal_progress | verbalReply | ❌ |
> Type collides within each column; only `agent_tool_name` separates them. (quick_replies won't work as a proxy — a real `send_message` often has none.)
>
> **My recommended shape — simpler than the task's step 2.** Compute a `forkable` boolean at the two dispatch sites (whitelist: `tool === 'send_message' || tool === 'send_verbal_reply'`) and thread it through `addAgentMessage → addBubble`. Then at `app.js:494`:
> ```
> if (forkSession && seq && forkable) div.appendChild(createMenuButton(...));
> else div.appendChild(createTtsButton(...));
> ```
> Non-forkable agent bubbles fall back to the plain play button, so the ⋯ menu only ever appears when there's actually a fork to offer. That means **`openBubbleMenuFor` needs zero changes** — no conditional fork item, no half-empty menu. One less moving part than the task proposes.
>
> **Whitelist, not blacklist** — deliberately fail-closed. Old persisted history events (pre-stamping) lack `agent_tool_name`; a whitelist hides fork on those, a blacklist would keep offering it. Given the task's own framing ("silent wrong-cut is worse than a visible error"), defaulting unknown→non-forkable is the right bias, and the server already rejects bad anchors defensively anyway.
>
> **E2E is trivial** — the existing spec drives `window.addAgentMessage(...)` directly (fork-button.spec.cjs:133), so I just add the new arg: a `send_progress`-flavored call asserts `.bubble-menu-btn` count 0 + `.bubble-tts-btn` count 1; a `send_message` one still asserts the menu.
>
> One thing to decide: thread a **boolean `forkable`** (my lean — keeps addBubble/menu agnostic) vs. the **raw tool name** (more flexible if you later want per-tool menu items). I'd go boolean.
>
> Want me to implement it?

[Quick replies]
- Implement it (boolean forkable)
- Use raw tool name instead
- Just discuss, don't code yet

**USER**

> Implement it (boolean forkable)

<small>took 2m 47s</small><br>
**AGENT**

> Implemented: `isForkableTool` helper + `forkable` plumbed through all 4 dispatch sites → addAgentMessage/addBubble. Non-forkable (progress) agent bubbles now fall back to the plain play button; ⋯ menu only on forkable replies. Rebuilt the embedded binary, CDP warmed, extended the E2E spec. Running `make test`…

<small>took 23m 25s</small><br>
**AGENT**

> Done — implemented, built, and green. `make test`: all unit tests + 62/62 E2E pass.
>
> **What changed (client-only, `client-dist/app.js`):**
> - Added `isForkableTool(toolName)` — whitelists `send_message` / `send_verbal_reply` (fail-closed: unknown/missing → non-forkable).
> - `addBubble` / `addAgentMessage` take a new `forkable` arg. Menu gate is now `forkSession && seq && forkable`; non-forkable agent bubbles fall back to the plain play button. So the ⋯ menu appears **only** when a fork is genuinely on offer — `openBubbleMenuFor` needed no change.
> - All 4 dispatch sites (live WS + history replay, for both `agentMessage` and `verbalReply`) now compute `forkable` from `agent_tool_name`.
>
> **Server:** confirmed no change needed — `agent_tool_name` is on the `Event` struct (`eventbus.go:65`) and every path `json.Marshal`s the full struct (`main.go:516/556` live, history stream, `get_chat_history`). Step 3 of the task was already satisfied.
>
> **Tests (`e2e/fork-button.spec.cjs`):**
> - New: a `send_progress`-style bubble (`forkable=false`, has seq) shows the plain play button, no ⋯ menu.
> - New: `isForkableTool` mapping — the two reply tools true, the two progress tools + `undefined` false.
> - Updated the existing menu tests to pass `forkable=true` (they'd otherwise render non-forkable now).
>
> Rebuilt the embedded binary (`npm-platforms/linux-x64/bin/agent-chat`) so the running server picks it up. Marked the task file resolved.
>
> Nothing committed — want me to commit this?

[Quick replies]
- Commit it
- Show me the app.js diff
- Leave it uncommitted

**USER**

> Commit it

<small>took 29.1s</small><br>
**AGENT**

> Committed to `main` as `ddb3bc8` — 3 files changed (+127/−21):
> - `client-dist/app.js` — the fix
> - `e2e/fork-button.spec.cjs` — new + updated tests
> - `tasks/…-fork-menu-hide-on-progress-bubbles.md` — marked resolved
>
> The rebuilt binary is gitignored (build artifact), so it's not in the commit — but it's live on the running server. Not pushed. Want me to push?

[Quick replies]
- Push it
- Leave it local
- Cut a release too

**USER**

> /ck:export-chat-logs and make build

