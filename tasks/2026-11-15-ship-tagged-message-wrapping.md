# Ship tagged message wrapping (fix: style preset glued to the user's words)

Filed 2026-11-15. Status: **REVISED 2026-11-15: ship a small 0.19.1 patch instead** (see "Revised plan" below). The tagged-branch work (rebase, merge, phases 1-4 below) is parked on branch `ship-tagged-wrapping` (tip d6babc2, with its own phase logs) and ships later on its own schedule, or never. Repo: agent-chat
(`/repos/agent-chat/workspace`). Do not publish to npm until every
"Done when" box below is ticked.

## The bug (seen live 2026-11-15)

The user had the Message-style preset "Reply for a reader with ADHD: lead with
the next action, ... cut all preamble, recap, and closers." and typed:

> Discuss how do we support this in swe-swe? Through agent-chat? When calling
> supporting mcp?
>
> (attached: pasted-207-lines.txt -- an article on MCP Apps / Manufact)

agent-chat 0.19.0 delivered it as one blob:

```
User said: Reply for a reader with ADHD: lead with ... closers.

Discuss how do we support this in swe-swe? ...
```

`applyMessageTemplate` (`eventbus.go`, added in `ec6ff78`, 2026-07-24) pastes
the style above the message with only a blank line between them. The chat
bubble hides the style, so the user saw only their own words. The agent could
not tell where the style ended. It read "this" as the style and answered "how
do we support ADHD-friendly replies in swe-swe" instead of "how do we support
MCP Apps in swe-swe". It answered the wrong question, confidently.

Every agent-chat release since `ec6ff78` (2026-07-24) does this. swe-swe does
not pin agent-chat (`swe-npx -y @choonkeat/agent-chat`), so every swe-swe
install has it today.

## Revised plan: 0.19.1 patch on main (agreed in chat 2026-11-15)

Everything that broke released swe-swe (turn-start wording, empty-queue text)
came from the tagged branch. 0.19.0 already says `User said:` and
`{"queue":"empty"}`, which every released swe-swe Stop hook matches. So:
keep that wording exactly, and only move the style.

Target shape:

```
User said: Discuss how do we support this in swe-swe? ...

(Reply style your user set in settings, not part of their message:) Reply for a reader with ADHD: ...
```

### Phase 1 — regression test, RED on 0.19.0
`style_after_message_test.go`, using only `UserMessage{Text, Template}`:
the "Discuss how do we support this" case (check_messages and send_message
reply), a 2-message batch (style once, after both), `{{message}}` templates
(marker dropped, rest is the style), a marker-only template (no style line),
plus pins for the swe-swe Stop hook: `User said:` / `User responded:` present,
empty queue matches `queue.{0,8}empty`.

### Phase 2 — the patch, GREEN
Style rendered after the messages (and attached files) by a new
`style-note` block, outside `format-messages`, so a custom rules file's
layout keeps working and still gets the style. Latest user message's style
wins for a batch. `.Preamble` kept (always empty) so old rules files parse.
Also: a block missing from a rules file falls back to the built-in one WITH
its data (0.19.0 passes nil: `<no value>`, and an empty message), tested.

### Phase 3 — make test (unit + e2e)

### Phase 4 — live check
Local build in a dev swe-swe session (released 2.43.2 hook): ADHD preset,
send "Discuss how do we support this?" with a file attached; the agent sees
`User said: <words>` then the labelled style; the bubble shows only the words.
Idle nudge with an empty queue: the Stop hook does not block.

### Phase 5 — release
CHANGELOG `[0.19.1]`, `make bump NEW_VERSION=0.19.1`, npm publish (needs the
user's browser approval).

### Done when
- [ ] Phase 1 RED on 0.19.0 observed and logged
- [ ] Phase 2 GREEN; rules-file fallback tested
- [ ] `make test` green
- [ ] Live check done and noted here
- [ ] CHANGELOG + bump + publish (with approval)

---

Original brief (tagged branch) follows, kept for reference.

## The fix already exists, unreleased

Branch `change-message-format`, worktree
`/repos/agent-chat/worktrees/change-message-format` (last commit 34745e4,
2026-11-04). Plan: `tasks/2026-11-01-tagged-message-wrapping.md` (phases 1-6
done, phase 7 partly done). The branch delivers:

```
Your user typed:
<message id="h4m8t2">
Discuss how do we support this in swe-swe? ...
</message id="h4m8t2">

Reply in the style your user asked for:
<style id="w9c3x1">
Reply for a reader with ADHD: ...
</style id="w9c3x1">
```

CHANGELOG `[Unreleased]` on the branch already describes it.

## Work

### 1. Rebase the branch onto main — DONE (merged a15a085; full `make test` pending)
- Main has 11 commits the branch lacks (0.18.0, 0.19.0: `[[name]]` links,
  quick-reply drafting, `AGENT_CHAT_UPLOAD_MAX_MB`, ...). Some are
  rebased-duplicate copies of branch commits (`d49cab9`/`b482d15`,
  `7629687`/`4fbeef5`, `7e95d81`/`a29a59c`, `edceb22`/`d9a93d0`); drop
  the branch copy when git reports it empty.
- `git merge-tree main change-message-format` reports conflicts. Expect them
  in `CHANGELOG.md`, `client-dist/app.js`, `main.go`, `tools.go`,
  `prompts/agent-reply.tmpl`.
- Rebase in the worktree, not on main (see swe-swe memory
  `feedback_prefer_noff_merge`). Then `git merge --no-ff` into main.
- The CHANGELOG `[Unreleased]` section must sit above `0.19.0`, not merged
  into it.

### 2. Regression test for THIS bug (write it first, RED on main) — DONE (`style_glue_regression_test.go`: RED on 9cac2d8, GREEN after; `{{message}}` templates now go to the style block with the marker dropped)
Add to `wrap_test.go` (or `tools_test.go` on main for the RED run):
- A message whose style ends in an instruction and whose text starts with
  "this" / "Discuss how do we support this". Assert:
  - the user's text sits alone inside `<message id=X>...</message id=X>`;
  - the style text appears only inside `<style id=Y>`, never inside the
    message tag;
  - X != Y, and both match `^[a-z0-9]{6}$`.
- A batch of 2+ messages with the same style: the style block appears once.
- A placeholder-style template (`{{message}}` form, see
  `messageTemplatePlaceholder`): it still ends up in the style block, not
  inside the message tag. Either support it or decide and test what happens.
- A user message that itself contains `</message id="abc123">` and
  `</style ...>`: it stays inside its own tag (already in `wrap_test.go`
  "hostile text" -- confirm it survives the rebase).

### 3. Compatibility with RELEASED swe-swe 2.43.2 (and current swe-swe main)
swe-swe's Stop hook (`swe-swe/cmd/swe-swe/hook-scripts/swe-swe-stop-guard.sh`)
matches agent-chat's wording. Check both versions:

| Guard check | 2.43.2 matches | swe-swe main matches | Branch says | Effect |
|---|---|---|---|---|
| Turn start (`User said:` / `User responded:`) | old only | old + new | `Your user typed:` / `Your user replied:` / ... | 2.43.2: a chat reply does not start a new turn, so an earlier `send_message` covers it and the guard **misses** some silent turn-ends (fails open). |
| Empty queue (`queue.\{0,8\}empty`) | yes | yes | "No new messages. Do not reply..." | **Both versions break**: an empty `check_messages` no longer counts as an allowed silent turn, so the guard **blocks** and forces a pointless `send_message` every time the agent is nudged with nothing waiting. |

**Hard constraint: agent-chat must work with swe-swe as deployed, with no
swe-swe upgrade.** swe-swe runs `swe-npx -y @choonkeat/agent-chat`, so every
new session picks up the latest agent-chat. Most users stay on whatever
swe-swe they installed, often for a long time. A swe-swe release does not fix
anything for them. Only agent-chat's own text can.

Guard history (`git log -L` on the guard's turn-start line in swe-swe):
- before `75bd769a7` (2026-07-25): turns are anchored on typed messages only.
  Wording-independent.
- `75bd769a7` .. 2.43.2: anchors on `User said:` / `User responded:`. Empty
  queue = `queue.\{0,8\}empty` (since `88bbed485`, 2026-07-05).
- `9cc6427da` (swe-swe main, unreleased): old wording plus the new wording.

Required in this repo:
- **Keep the `empty-queue` text matching `queue.{0,8}empty`.** Suggested:
  `No new messages (queue empty). Do not reply just to say so; go back to your task or wait.`
  Add a test that pins it: `regexp.MustCompile("queue.{0,8}empty")` matches
  `execTemplate("empty-queue", ...)`, with a comment naming the swe-swe guard
  that depends on it.
- **Keep the old turn-start anchors for messages from the user.** The
  sender line for a typed message must contain `User said:`, and the
  `send_message` reply must contain `User responded:`. Both are still true
  for the human user, so this is not the "wrong sender" problem the tagging
  plan fixes. Example: `User said (typed):` / `User responded (typed):`, and
  the speech-to-text note on the same line for voice. Messages from another
  agent keep "Another agent session wrote this; it is not your user." Old
  guards won't anchor on those, but they never did: today they arrive as
  "User said:" only by mistake.
  Do NOT fall back to accepting the fail-open: that would ship a weaker Stop
  hook to every existing swe-swe user, permanently.
  Add a test pinning both substrings, with a comment naming swe-swe
  `cmd/swe-swe/hook-scripts/swe-swe-stop-guard.sh` and releases
  `75bd769a7`..2.43.2. swe-swe main (`9cc6427da`) also matches the
  `Your user ...` wording; either form is fine for it.
- `send_chat_message` from 2.43.2 sends text with a trailing
  `(via send_chat_message from <addr>)` and no `from_name`. Confirm the branch
  still accepts that call and delivers it (labelled as the user, the same as
  today). swe-swe main sends `from_name`/`from_session` and retries with the
  text signoff if rejected. Test both payload shapes.
- swe-swe embeds its own copy of the client (`agent-chat-dist/`, used by the
  chat playback page). Confirm an older client that sends no `via` field
  still gets voice detected from the 🎤 prefix (the branch keeps that
  fallback; test it).

### 4. Rules-file users — DONE (missing blocks fall back with their data; RED showed the old fallback also emptied format-messages, losing the message itself)
`AGENT_CHAT_PROMPTS_FILE` copies made before the branch lack the tag and the
`style-block` section. On 2026-11-01 this rendered
`<style id="<no value>"><no value></style>` (see
`tasks/2026-11-03-rules-file-upgrade.md`, Why item 2). Minimum for this
release: a block missing from the user's file falls back to the built-in
block **with its data**, plus a test. The full 3-way upgrade helper in that
plan can ship later.

### 5. Live check before release
1. `make test` and the e2e suite (`make e2e` or as the Makefile names it).
2. Point a dev swe-swe session at the local build. Set the style preset to the
   ADHD text above. Send "Discuss how do we support this?" with a file
   attached. Check that the agent's transcript shows the message and style in
   separate tags and the bubble shows only the typed text.
3. Repeat 2 on a released swe-swe 2.43.2 install, which is what most users run.
4. Leave the agent idle, nudge it with `check_messages` while the queue is
   empty, and confirm the swe-swe Stop hook does NOT block.

## Done when
- [ ] Branch rebased onto main, merged `--no-ff`, `make test` green
- [x] New regression test for this bug: RED on 0.19.0, GREEN after
- [x] `empty-queue` text matches `queue.{0,8}empty`, pinned by a test
- [ ] User deliveries still contain `User said:` / `User responded:`, pinned by a test
- [ ] Live check run against RELEASED swe-swe 2.43.2 (not main): guard blocks a
      silent turn after a chat reply, and allows an empty-queue nudge
- [x] Both `send_chat_message` payload shapes tested
- [x] Missing-block fallback passes its data (no `<no value>`), tested
- [ ] Live check (section 5) done and noted in this file
- [ ] CHANGELOG `[Unreleased]` updated (one sentence per change)
- Then, and only then: `make bump NEW_VERSION=0.20.0` and the npm publish
  (needs the user's browser approval).
