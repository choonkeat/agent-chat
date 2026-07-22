<!-- agent-chat export
title: Mcp Contract Send Message Terminal Send Progress Non Terminal
date: 2026-07-22
index: 01
slug: mcp-contract-send-message-terminal-send-progress-non-terminal
session: 325a77afc6c11504
agent: claude
version: 0.8.17 (a9ccfee)
-->

# Mcp Contract Send Message Terminal Send Progress Non Terminal

_2026-07-22 · 01 · claude · agent-chat 0.8.17 (a9ccfee)_

**USER**

> Tried to use codex ibis swe-swe with agent chat but kept getting sticks codex self diagnosed and said:
>
> the MCP contract should explicitly distinguish "task complete / genuinely blocked" from "work still in progress." The key missing instruction is: NEVER call send_message or emit a final response while required work remains; send_progress is non-terminal, and after it returns immediately continue making tool calls in the same turn. Also state plainly that ending the assistant turn suspends execution—there is no background worker. The current wording ("always end a task by calling send_message and waiting") describes completion, but it doesn't strongly warn that a premature final silently pauses unfinished work.
>
> The API design could make this harder to misuse: split send_message into send_final (allowed only on completion) and request_input (allowed only on a real blocker); add task_state: in_progress|blocked|complete; reject send_final when in_progress; and provide a nonblocking heartbeat/progress facility or automatic heartbeat managed by the host. Most importantly, when send_message blocks and yields a cell/session ID, the tool description should say: wait on that cell only if the task is complete or input is essential—otherwise you chose the wrong tool.
>
> A concise prompt rule that would have prevented my behavior: "If you have promised an artifact and can safely continue, you are not blocked. Do not finalize, ask for confirmation, or wait for a reply. Keep the same turn alive, execute the work, and send nonblocking progress at ≤60-second intervals."

<small>took 1m 27s</small><br>
**AGENT**

> Rebuild + restart to apply: `GOOS=linux GOARCH=amd64 go build -o npm-platforms/linux-x64/bin/agent-chat .`
>
> Landed the anti-stall contract in 3 places (`make unit-test` green):
>
> 1. `prompts/agent-reply.tmpl` — two new rules appended to every user message: `send_message` is TERMINAL (only on complete-or-genuinely-blocked); "if you promised an artifact and can safely continue, you are NOT blocked — keep the same turn alive, send `send_progress` at least every 60s"; and "ending your turn SUSPENDS execution — there is no background worker."
> 2. `tools.go` send_message / send_verbal_reply descriptions — same TERMINAL warning, plus "a premature send_message silently pauses unfinished work."
> 3. `tools.go` send_progress / send_verbal_progress — now explicitly NON-TERMINAL: "the correct tool whenever work remains — after it returns, immediately continue making tool calls in the same turn."
>
> Not done (deliberately): the API split into `send_final` / `request_input` + a `task_state` enum with server-side rejection. That's a breaking MCP surface change affecting every agent config; the prompt fix is the cheap 90%. Say the word if you want it.
>
> Next: run the rebuild command above, restart, then retry codex — it should now progress instead of parking on send_message.

[Quick replies]
- Rebuild and restart
- Also do the send_final/request_input split
- Show me the full diff
- Commit this

**USER**

> The tools.go change affects more than codex though. How would Claude react for example

<small>took 45.9s</small><br>
**AGENT**

> Reworded to remove a real conflict — `make unit-test` green.
>
> The risk you spotted was concrete: my first draft said "do not ask for confirmation", which directly contradicts the pre-existing bullet "If the request is ambiguous, risky, or destructive, confirm with send_message BEFORE acting." Claude follows that safety bullet closely, so the pair would have read as "confirm / don't confirm" — the classic way to make a model pick unpredictably.
>
> New wording (tmpl + both tools.go descriptions):
> > `send_message` is TERMINAL: call it when the work is COMPLETE, when you need a decision only the user can make, **or to confirm before a risky/destructive step (see above)**. But if you promised an artifact and can safely continue, you are NOT blocked — do not finalize and do not ask permission to keep going.
>
> How Claude reacts to the rest, concretely:
> 1. "Ending your turn SUSPENDS execution" — matches how Claude already behaves. No change.
> 2. "send_progress at least every 60 seconds" — Claude will emit somewhat more progress chatter on long tasks. That's the only behavior change I expect, and it's also what keeps the MCP idle-abort window from firing (memory: 60s progress keepalive is already the fix for reply-loss).
> 3. "TERMINAL / NOT blocked" — Claude's failure mode is the opposite of codex's (Claude goes silent, codex parks on send_message), and "NEVER end your turn without calling send_message" is untouched above it, so the silence guard still holds.
>
> Next: `GOOS=linux GOARCH=amd64 go build -o npm-platforms/linux-x64/bin/agent-chat .`, restart, retry codex.

[Quick replies]
- Rebuild and restart
- Show me the tmpl diff
- Commit this
- Drop the 60s progress rule

**USER**

> Commit this

<small>took 16.0s</small><br>
**AGENT**

> Committed `a1040d6` on `fix-codex` — 3 files (prompts/agent-reply.tmpl, tools.go, tools_test.go). `agent-chats/index.html` and the untitled chatlog left unstaged.
>
> Not pushed, not rebuilt yet.
>
> Next: `GOOS=linux GOARCH=amd64 go build -o npm-platforms/linux-x64/bin/agent-chat .` then restart to load the new descriptions.

[Quick replies]
- Rebuild now
- Push the branch
- Open a PR
- Done for now

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session with uuid db6b073e-8f79-4c30-a72d-18a8be8646e6. If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session.

