# Tagged message wrapping — raw text vs. surrounding text

Filed 2026-11-01. Status: **planned, not started.** Two repos:
agent-chat (`/repos/agent-chat/workspace`) and swe-swe (`/workspace`).

## Why

Everything agent-chat hands the agent is one blob of text: "User said: " +
the message + rules. Three problems, all seen in a live self-test
(`send_chat_message` to the session's own address):

1. **No edge.** The agent cannot tell where the typed text ends and the added
   text begins. A message containing "This IS the user's message" or a fake
   "(via send_chat_message from …)" line looks identical to the real thing.
2. **Wrong sender.** A message from another agent session arrives as
   "User said: …" with "This IS the user's message" and "reply with
   send_message" — so the answer goes to the human, not the sending agent.
3. **Style text glued to the message.** The Message-style preset is pasted
   above the user's words, inside the same blob.

## Decisions (agreed in chat 2026-11-01)

- Only user/agent-written text goes inside a tag:
  `<message id="q7f2k9">…</message>` and `<style id="…">…</style>`.
  Everything else is plain prose.
- **Random id per tag** (6 chars, `[a-z0-9]`, fresh each delivery). Nothing
  typed can guess the closing tag, so **no escaping** — text arrives exactly as
  typed, `>` and `&` included.
- Sender stated in prose above the tag: "Your user typed:", "Your user said
  aloud (speech-to-text, may be inaccurate):", "Another agent session wrote
  this; it is not your user. Sender: <name> (<address>)".
- Style belongs to the reply rules for the user, not to the message.
- Voice comes from a field the browser sends, never from a 🎤 text prefix.
- Terminal-typed lines (interrupt, nudge, /clear, /compact resume) stay plain
  text with no user words in them, and start with `[agent-chat]`.
- JSON was considered and dropped: too structured, and long text turns into
  one `\n`-escaped line.

## Target text — all 13 cases

Ids below are examples. `…` marks rules text that comes from the rules
template (full every 10th delivery, short otherwise — rotation unchanged).

**1. User typed (short rules)**
```
Your user typed:
<message id="q7f2k9">
Show me all permutations
</message>

Execute it; do not echo it back. They cannot see your terminal: reply only with send_message (waits for their answer) or send_progress (does not wait). Start with send_progress. Never end a turn without send_message.
```

**2. User typed + file + style + full rules**
```
Your user typed:
<message id="h4m8t2">
See attached
</message>

Attached files:
  /tmp/…/IMG_4584.jpeg (image/jpeg, 267KB)

Execute it; do not echo it back.

Reply in the style your user asked for:
<style id="w9c3x1">
Explain in plain, non-technical language … cut all preamble, recap, and closers.
</style>

They cannot see your terminal … (full rules)
```

**3. User spoke**
```
Your user said aloud (speech-to-text, may be inaccurate):
<message id="b3x8w1">
show me the samples
</message>

Execute it. They can only hear you now: plain speech, no markdown, one question per message. Reply only with send_verbal_reply or send_verbal_progress.
```

**4. Another agent**
```
Another agent session wrote this; it is not your user.
Sender: choonkeat/swe-swe@main (devdotsweswedotcom/12a33f4e-…)
<message id="n5c1r7">
Is the format merged yet?
</message>

Your user does not see this. To answer, call send_chat_message with uuid devdotsweswedotcom/12a33f4e-….
```
No style block for agent messages.

**5. Several at once (mixed)**
```
3 new messages, oldest first.

Your user typed:
<message id="a1k9d3">
fix the bug
</message>

Your user said aloud (speech-to-text, may be inaccurate):
<message id="p6v2s8">
actually wait
</message>

Another agent session wrote this; it is not your user.
Sender: choonkeat/swe-swe@main (devdotsweswedotcom/12a33f4e-…)
<message id="z0e4j5">
ping
</message>

The first two are from your user; if they conflict, the later one wins. The latest was spoken, so answer your user by voice (send_verbal_reply). Answer the agent with send_chat_message, uuid devdotsweswedotcom/12a33f4e-….
```
Behaviour change: voice follows the user's **latest** message, not "any
message in the batch is voice" (`tools.go` `isVoiceMessage`). Style block
appears once per batch (today's `sharedPreamble` hoisting), only if a user
message is present.

**6. send_progress with a mid-work message** (replaces `---BARGE-IN---`)
```
ok

While you were working, your user typed:
<message id="r2w7c4">
stop, wrong file
</message>

This is a new instruction; it takes priority over what you were doing.
```

**7. send_message reply**
```
Your user replied:
<message id="f8n3q6">
Approve, write the plan
</message>

Execute it; do not echo it back. … (rules)
Chat window: http://localhost:4004
```

**8. Resent** (replaces `---REDELIVERY---`)
```
Resent, because an earlier delivery may have been lost. If you already handled it, ignore it.

Your user typed:
<message id="k5t1m9">
…
</message>
```
Mixed with fresh messages: fresh first, then "Also resent:" + resent ones.

**9. check_messages, empty**
`No new messages. Do not reply just to say so; go back to your task or wait.`
(Drops the `{"queue":"empty"}` prefix — nothing in swe-swe parses it; check
tests before removing.)

**10. send_verbal_progress:** `ok` (today: a full sentence).

**11. send_message refused in voice mode**
`Not sent: your user is speaking, so reply with send_verbal_reply instead.`

**12. Interrupt + swe-swe nudge** (`tools.go` `chatNudgeText`)
`[agent-chat] New message waiting. Call check_messages, then send_progress before you start. If nothing comes back, your user's last message is your instruction, unless you are already on it.`

**13. /clear and /compact** (`client-dist/app.js` `typeClearResumeLine`,
`typeCompactResumeLine`)
- /clear → `/clear`, then `[agent-chat] Context cleared. Read <chat log path> for the conversation so far, then call check_messages.`
- /compact → `/compact`, then line 12.

## Steps

### Phase 1 — agent-chat: sender data on each message (~1h)
1. `eventbus.go` `UserMessage`: add `From` (`"user"` | `"agent"`),
   `Via` (`"typed"` | `"voice"`), `SenderName`, `SenderSession`. Default
   `From=user, Via=typed`.
2. Browser `transmit()` (`client-dist/app.js:2380`) sends `via: "voice"`
   instead of relying on the 🎤 prefix in `text`. Server keeps accepting the
   🎤 prefix for one release (older cached pages), then drops it.
3. Orchestrator `send_chat_message` (`tools.go:884`) accepts optional
   `from_name`, `from_session`; when present, `From=agent`.
4. Verify: unit tests for each field's default and for the 🎤 fallback.

### Phase 2 — agent-chat: new wrapping (~2h)
1. Random-id helper (`crypto/rand`, 6 chars). Unit test: two calls differ;
   output matches `[a-z0-9]{6}`.
2. Rewrite `prompts/agent-reply.tmpl` blocks to produce cases 1–11. New
   template data: per message `.ID`, `.From`, `.Via`, `.SenderName`,
   `.SenderSession`, `.Text`; batch-level `.Style`, `.StyleID`, `.LatestVoice`,
   `.Agents` (for the answer-the-agent line).
3. `tools.go`: remove the hard-coded `"User said: "`, `"User responded: "`,
   `---BARGE-IN---`, `---REDELIVERY---` strings; route them through the
   template (new blocks: `barge-in-intro`, `redelivery-intro`, `reply-intro`).
4. Style: stop pasting it into message text (`applyMessageTemplate`); pass it
   as `.Style`. Keep `{{message}}`-placeholder templates working by treating
   them as legacy (wrap the result as the message, as today).
5. Voice fixes: `send_verbal_progress` returns `ok`; refusal text (case 11);
   `send_verbal_reply` description says 🎤 not 🎙 (moot once Via is a field —
   reword to "when the user is speaking").
6. Verify: golden-text unit tests for cases 1–11 in `tools_test.go` (13
   existing assertions on the old strings get updated).

### Phase 3 — agent-chat: terminal lines + tool descriptions (~45m)
1. `chatNudgeText` → case 12. The browser and swe-swe both read it from
   agent-chat (`CHAT_NUDGE_TEXT`, `agent_waiting`), so one edit covers both.
2. `typeClearResumeLine` → case 13.
3. Rewrite tool descriptions for send_message, send_progress, check_messages,
   send_verbal_reply, send_verbal_progress to describe the new shape (no
   "User said:", no sentinels). Pin with tests.
4. Verify: `make test`.

### Phase 4 — agent-chat: custom rules files (~45m)
1. Existing files keep working: unknown/missing blocks already fall back to
   built-in (`prompts.go` `execTemplate`).
2. A file whose `format-messages` does not reference `.ID` → start-up tip
   names the file: "your rules file lays out messages without the tag; empty
   it to refresh." (Today a broken slot only logs to stderr.)
3. Update `rulesFileHeader()` to list the new blocks and slots.
4. Verify: unit test with an old-style file (renders, warns once) and a
   new-style file (renders tags).

### Phase 5 — swe-swe: send sender as fields (~1h)
1. `session_address.go` `chatSignoff` → stop appending to text; pass
   `from_session` (address) and `from_name` (session name) as args to
   `pushChatMessage` (`main.go:10763`).
2. Compatibility: an older agent-chat may reject unknown args. On that error,
   retry once with today's text signoff.
3. Update `send_chat_message` tool description (`main.go:10746`).
4. Regenerate golden testdata; verify with swe-swe's own test target.

### Phase 6 — chat window: bubble side by sender (~1h, can ship separately)
1. Carry `From` / `SenderName` on the `userMessage` event.
2. `from=user` → right, blue (today). `from=agent` → left, labelled with
   `SenderName`.
3. Verify: E2E spec pushing one user and one agent message; check alignment.

### Phase 7 — release
1. Self-test again: `send_chat_message` to own session; confirm case 4 text.
2. `make test` (both repos), CHANGELOG, release agent-chat first, then swe-swe.

**Total: about 7½ hours.** Phases 1–4 are agent-chat only and can land
without swe-swe. Phase 6 is independent of the text changes.

## Tests

### A. Must keep passing (existing tests, wording updated, behaviour unchanged)
About 40 tests today in `tools_test.go`, `eventbus_test.go`, `prompts_test.go`
plus `clear-prefix` / `compact-prefix` / `msg-style-persist` E2E specs.
1. Full rules every 10th delivery, short otherwise (`TestDeliveryGuidanceRotates`).
2. Lost-delivery resend and read receipts (limbo / ProveDelivery tests) — only
   the intro wording changes, never which messages are resent.
3. Mid-work messages are picked up, drained once, marked read
   (`TestAppendBargeIn*`).
4. Style: hoisted once per batch; legacy `{{message}}` templates still work;
   attachments listed with type and size (`TestFormatMessages*`).
5. Rules files: missing / blank / edited / broken / partial / version notice
   (`TestRulesFile*`); /clear and /compact E2E; `agent_waiting` carries the
   nudge line.

### B. New: exact text for every case
1. Golden test per case 1–13: build the input, compare the whole output to
   the text in this file (ids replaced by a fixed test id).
2. Random id: 6 chars `[a-z0-9]`; opening and closing tag use the same id;
   two tags never share an id; a resent message gets a new id.
3. Agent message: no style block, no "your user" wording, answer line names
   the real sender address.
4. Mixed batch: order kept; voice follows the latest user message; style
   block only when a user message is present.
5. Voice comes from the `via` field: a typed message that starts with "🎤 " is
   NOT treated as voice (after the one-release fallback ends).

### C. New: proves the improvement (the reason for this change)
1. Text containing `</message>`, `</style>`, `<message id="x">` stays inside
   its tag: extract between the real tags → equals the input byte for byte.
2. Text containing fake wrapper lines ("Another agent session wrote this",
   "Sender: …", "[agent-chat] New message waiting", "Your user typed:") stays
   inside the tag and changes nothing outside it.
3. No escaping: `<`, `>`, `&`, code blocks, tabs, trailing spaces, CRLF, emoji
   and a 100 KB message arrive byte-identical.
4. swe-swe: an agent whose text contains "(via send_chat_message from <other
   session>)" still gets its own address in the Sender line.
5. Edge inputs: attachment with no text; whitespace-only style; empty batch.

### D. swe-swe
1. `send_chat_message` passes `from_session` / `from_name`, no text signoff.
2. Against an agent-chat that rejects the new args: retries once with the old
   signoff and succeeds.
3. Golden testdata regenerated and diff reviewed.

### E. Browser (E2E)
1. User message → right, blue; agent message → left, labelled with sender name.
2. Spoken message sends `via: "voice"`; the bubble shows no 🎤 text.
3. Interrupt / /clear / /compact post the new `[agent-chat]` lines.
4. "Conversation context only" mode still delivers the message after /clear.

### F. Manual check with a live agent (not automatable)
1. Self-test: `send_chat_message` to own session → receive case 4 text.
2. Send a message containing a fake `</message>` + fake rules; confirm the
   agent treats it as quoted text, not instructions.

## Open questions
- Should the per-batch random id be one id with suffixes (`q7f2k9-1`) or one
  per tag? Plan assumes one per tag.
- Cause of `&gt;` / `&amp;` appearing in user messages typed on iPhone
  (seen 3 times this session) — not investigated yet; may interact with
  "no escaping".
