<!-- agent-chat export
title: Streaming Chatlog 0 8 14 Release
date: 2026-07-18
index: 02
slug: streaming-chatlog-0-8-14-release
agent: claude
version: 0.8.13 (65cde4c)
-->

# Streaming Chatlog 0 8 14 Release

_2026-07-18 · 02 · claude · agent-chat 0.8.13 (65cde4c)_

**USER**

> Please read tasks/2026-07-18-streaming-chatlog-auto-export.md (just written, uncommitted). It is the agreed design for streaming chat-log auto-export: append-as-it-goes markdown + assets driven by a new AGENT_CHAT_EXPORT_DIR env var, turn-end doing only rename + index.html regeneration, index.html becoming regenerated-from-glob instead of upserted, plus set_chat_title and chatlog_optout tools. The design decisions in it are confirmed with the user — don't relitigate them, but do flag anything that contradicts the code you find.
>
> Then: (1) commit the plan first as its own commit (docs(tasks): ...), (2) execute the TDD steps in order, committing per step, following the plan's red-green structure and running make test after every step (never go test directly; for e2e warm the lazy CDP endpoint first per CLAUDE.md). Step 1 is a pure refactor proven by a byte-identical equivalence test — if that test can't be made to pass byte-identically, stop and say so rather than loosening the assertion.

<small>took 1m 39s</small><br>
**AGENT**

> Read the plan. Now reading the current exporter/tools/eventbus code, then I'll commit the plan and start Step 1 (extract per-bubble renderer, proven by a byte-identical equivalence test).

<small>took 5m 25s</small><br>
**AGENT**

> ✅ Plan committed (f19ee01). ✅ Step 1 done (240bb15): renderChatBubble extracted, byte-identical equivalence test passed on first green, full make test green (unit + 76 E2E). Starting Step 2: regenerated (not upserted) index.html.

<small>took 5m 52s</small><br>
**AGENT**

> ✅ Step 2 done (65b916a): index.html now fully regenerated from the *.md glob (idempotent, heals merge conflicts); upsertIndexHTML deleted. make test green. Starting Step 3: streaming writer core (chatlogstream.go).

<small>took 6m 1s</small><br>
**AGENT**

> ✅ Step 3 done (5f82bc7): streaming writer core — per-event append + immediate asset copy, byte-identical to batch render. make test green. Starting Step 4: set_chat_title, rename + header rewrite, session resume.

<small>took 7m 19s</small><br>
**AGENT**

> ✅ Step 4 done (9db3337): set_chat_title tool, rename = full rewrite from history, session resume via session: header (fold state re-derived from bus history, not md parsing). make test green. Starting Step 5: env-var wiring, turn-end debounce, SIGTERM flush, chatlog_optout.

<small>took 7m 33s</small><br>
**AGENT**

> ✅ Step 5 done (1fe91b2): AGENT_CHAT_EXPORT_DIR wiring, 2s turn-end index debounce, SIGTERM flush + final regen, chatlog_optout with set_chat_title re-arm. make test green. Step 6: README/CHANGELOG docs + manual smoke test with a live server.

<small>took 18m 23s</small><br>
**AGENT**

> All 6 steps done — 7 commits on main, `make test` (unit + 76 E2E) green after every step.
>
> **Commits**
> 1. `f19ee01` docs(tasks): the plan itself
> 2. `240bb15` Step 1 — `renderChatBubble` extracted; the byte-identical equivalence test passed without loosening anything
> 3. `65b916a` Step 2 — `index.html` regenerated from the *.md glob (idempotent, heals merge markers); `upsertIndexHTML` deleted
> 4. `5f82bc7` Step 3 — streaming writer: per-event append + immediate asset copy, on-disk file always byte-identical to a batch render
> 5. `9db3337` Step 4 — `set_chat_title` (rename = full rewrite from history), restart-resume via `session:` header (state re-derived by re-folding bus history)
> 6. `1fe91b2` Step 5 — `AGENT_CHAT_EXPORT_DIR` wiring, 2s turn-end index debounce, SIGTERM flush + final regen, `chatlog_optout` with `set_chat_title` re-arm
> 7. `83d07d1` Step 6 — README + CHANGELOG
>
> **Manual smoke test (live server, clean env, port 4777)** — all verified: bubbles + sha-named asset appear on disk the moment each event fires (index.html correctly absent until the 2s debounce); `set_chat_title` renamed file + regenerated index; `chatlog_optout` deleted the .md and kept assets; re-arm rebuilt it from history; a real SIGTERM restart resumed the *same* file via the session header with correct elapsed-time continuity; viewer renders the archive fine (screenshot taken — thumbnail broken only because my test image was fake PNG bytes; the asset URL itself serves 200).
>
> **Notes**
> - No contradictions found between the plan and the code.
> - Session identity derives from a hash of the `AGENT_CHAT_EVENT_LOG` path (stable across restarts); without an event log there's no history to resume, so it falls back to a random UUID.
> - One extra behavior beyond the plan's letter: enabling the env var mid-session (event log has history but no export file yet) backfills the fresh .md from history at boot, best-effort on attachments.
> - Nothing pushed; `agent-chats/` in this repo untouched. Follow-ups (swe-swe env-var default, backfill CLI) remain tracked in the task file.

[Quick replies]
- Push to origin
- Run a /code-review on the branch
- Show me the smoke-test .md file

**USER**

> If we publish new npm package, what happens to existing old swe-swe instances when they launch new sessions and picking up new agent-chat?

<small>took 1m 16s</small><br>
**AGENT**

> Short answer: old swe-swe instances are safe — the new feature stays dormant for them, with two small visible side effects.
>
> **How they pick it up:** swe-swe spawns `swe-npx -y @choonkeat/agent-chat` (unpinned), so new sessions get the new binary once the npx cache refreshes. Running sessions keep their old process.
>
> **What changes for an old swe-swe instance, per new session:**
>
> 1. **Streaming export: OFF.** Old swe-swe doesn't set `AGENT_CHAT_EXPORT_DIR`, so no stream is created, no `agent-chats/` dir appears, nothing new is written. The JSONL event log (`AGENT_CHAT_EVENT_LOG`) is completely untouched, so resume/fork/playback/summaries all behave exactly as before.
>
> 2. **Two new tools appear in tools/list** (`set_chat_title`, `chatlog_optout`). If the agent calls them without the env var, it gets a clean "export is disabled — set AGENT_CHAT_EXPORT_DIR" error. Harmless, but agents may waste a call. If you'd rather old hosts see zero change, I can gate registration on the env var instead — trade-off: the tools then aren't discoverable at all when disabled. Say the word and I'll make that change.
>
> 3. **The one real behavior change that reaches old instances: `export_chat_md`'s index.html.** It's now regenerated from the `*.md` files on disk instead of prepending an entry. I checked this repo's real archive: all 14 existing exports have `title:` headers matching their manifest entries, so a regeneration reproduces the same listing — existing archives migrate cleanly. The caveats: any *hand-edits to index.html itself* (custom titles not in the .md header, hand-added entries, files not matching `YYYY-MM-DD-NN-slug.md`) get overwritten on the next export. Upside: conflicted/broken index.html heals automatically.
>
> Nothing is published yet — this would go out as 0.8.14 whenever you want to run the bump/publish flow.

[Quick replies]
- Gate the new tools on the env var
- Keep as is — publish 0.8.14
- Keep as is, don't publish yet

**USER**

> Export chat logs, git push and make build

<small>took 18.5s</small><br>
**AGENT**

> Exporting the chat log, then committing it, auditing history for public-repo safety, pushing, and running make build.

<small>took 50.8s</small><br>
**AGENT**

> Chat log exported + committed (5e1ced6), history audit clean (origin/main..HEAD, 8 commits — no secrets/PII/infra leakage), pushed to origin/main. Now running make build (cross-compiles all platforms — takes a while).

<small>took 28.2s</small><br>
**AGENT**

> All three done:
>
> 1. **Chat log exported & committed** — `agent-chats/2026-07-18-01-streaming-chatlog-auto-export.md` + updated `index.html`, committed as `5e1ced6` (only those two paths staged).
> 2. **History audited & pushed** — scanned `origin/main..HEAD` (all 8 new commits) for secrets, PII, private IPs/hostnames, tunnel identifiers, and workflow leakage: clean (the only grep hit was the pre-existing public `*.lvh.me` line in CHANGELOG). Pushed: `97b5d14..5e1ced6  main → main`.
> 3. **make build** — cross-compiled all 6 platform packages (darwin/linux/win32 × x64/arm64) successfully; local binary reports `agent-chat 0.8.13 (5e1ced6)`.
>
> Note: the version is still 0.8.13 — `make bump` hasn't run, so nothing is published to npm yet. Also, this session's running agent-chat server is still the old binary; a restart would pick up the new build. And the open question from earlier stands: want me to gate `set_chat_title`/`chatlog_optout` registration on `AGENT_CHAT_EXPORT_DIR` before any release, or ship them always-visible?

[Quick replies]
- Ship as is — bump & publish 0.8.14
- Gate the new tools first
- Done for now

**USER**

> /npm-login-and-publish with @CHANGELOG.md update

<small>took 2m 4s</small><br>
**AGENT**

> npm token was stale (401). Please log in here: https://www.npmjs.com/login?next=/login/cli/[redacted]
>
> Meanwhile: version bumped to 0.8.14, CHANGELOG updated (also backfilled the missing 0.8.11–0.8.13 entries), committed and pushed (b858a6b). Now building all platform binaries; I'll ask for the OTP when the build is done.

<small>took 27.3s</small><br>
**AGENT**

> Ready to publish 0.8.14 — all 6 platform binaries built and stamped (`agent-chat 0.8.14 (b858a6b)`), release commit pushed.
>
> Two things needed from you:
> 1. **Complete the npm web login** (still showing 401): https://www.npmjs.com/login?next=/login/cli/[redacted]
> 2. Then reply here with a **fresh 6-digit OTP** from your authenticator, and I'll publish all 7 packages immediately.

[Quick replies]
- Logged in — OTP is: 
- Login link expired, give me a new one
- Abort the publish

**USER**

> Logged in — OTP is:

<small>took 9.3s</small><br>
**AGENT**

> Login confirmed ✓ (npm whoami → choonkeat). But your reply came through without the OTP digits — please send just the fresh 6-digit code from your authenticator and I'll publish immediately.

[Quick replies]
- 123456

**USER**

> [redacted 6-digit OTP]

<small>took 1m 3s</small><br>
**AGENT**

> 🚀 **0.8.14 published** — all 7 packages went up cleanly and the registry already serves it (`npm view @choonkeat/agent-chat` → 0.8.14):
>
> - `@choonkeat/agent-chat` (main)
> - `@choonkeat/agent-chat-linux-x64` / `-linux-arm64`
> - `@choonkeat/agent-chat-darwin-x64` / `-darwin-arm64`
> - `@choonkeat/agent-chat-win32-x64` / `-win32-arm64`
>
> Release commit `b858a6b` (chore(release): 0.8.14) is pushed. The CHANGELOG got the 0.8.14 section (streaming chat-log auto-export, the check_messages interrupt rework, the pending-queue restart fix) — and while I was at it I backfilled the missing 0.8.11–0.8.13 sections from git history (the parent_url/App-Preview feature had actually shipped in 0.8.11, not "Unreleased").
>
> What ships in 0.8.14: the streaming export (dormant until `AGENT_CHAT_EXPORT_DIR` is set), `set_chat_title` + `chatlog_optout` tools (always registered, clean error when disabled), and the glob-regenerated `index.html`. Existing swe-swe instances pick it up on their next new session via npx.
>
> Natural follow-up when you're ready: the swe-swe side — defaulting `AGENT_CHAT_EXPORT_DIR={workDir}/agent-chats` at session spawn + a Settings opt-out toggle (tracked for `/workspace/tasks/`).

[Quick replies]
- Write the swe-swe follow-up task file
- Done for now
- Export this chat log too

**USER**

> It’s already written but you can review it /workspace/tasks/2026-07-18-chatlog-export-env-default.md

<small>took 1m 42s</small><br>
**AGENT**

> Reviewed `/workspace/tasks/2026-07-18-chatlog-export-env-default.md` against both the swe-swe template code and the agent-chat 0.8.14 I just shipped. **The plan is sound — every structural claim checks out.** Verified: `cmd.Dir = workDir` is indeed set in `launchMcpLessFleet` (mcp_less.go:115), `AGENT_CHAT_PORT`/`DISABLE` are reserved while `EXPORT_DIR` is not, the `AGENT_CHAT_EVENT_LOG` append does happen after all user env layers (so the presence-check design is genuinely required, and `envLookup` genuinely can't distinguish missing from empty — the `envHas` helper is needed). Empty-value-as-opt-out matches shipped agent-chat exactly.
>
> Findings worth folding in:
>
> 1. **The fork follow-up is now answerable — and its wording points the wrong way.** 0.8.14's `session:` identity = sha256 of the `AGENT_CHAT_EVENT_LOG` path. `/api/fork` copies the JSONL to a *new* path (main.go:5283), so a forked session gets a new identity → it mints a **new NN file with the full copied history backfilled** (attachments best-effort — old uploads may be gone). The follow-up says a fork "should continue its file, not mint a duplicate" — but continuation would be *wrong*: the source session may still be live, and two processes appending to one .md is interleaved garbage. Recommend rewording the follow-up to bless new-file-per-fork as correct, and only if someone wants opt-in continuation later, that's a new agent-chat env override (`AGENT_CHAT_SESSION_ID`), not a swe-swe change. Restart/resume of the *same* session (same JSONL path) correctly continues its file.
>
> 2. **Rollout note is already stale in a good way**: 0.8.14 is published, so the moment this swe-swe change ships, every new chat session starts archiving (that's the confirmed default-ON). Step 3's "after the agent-chat feature is published" e2e is unblocked *now*.
>
> 3. **Docs nit for Step 3**: a user-relocated path that escapes workDir disables the feature *silently* (warning goes to agent-chat's stderr, not the chat). Worth one sentence wherever the env var is documented.
>
> 4. Line-ref drift only: the `AGENT_CHAT_EVENT_LOG` append is now at main.go:5288 (not :5273); `reservedEnvKeys` at env_store.go:25 ✓.
>
> Want me to apply finding 1 (and the small notes) as edits to the task file, or leave it as-is for the swe-swe session to handle?

[Quick replies]
- Apply the edits to the task file
- Leave it — the swe-swe session will handle it
- Done for now

**USER**

> Apply the edits to the task file

<small>took 44.3s</small><br>
**AGENT**

> Edits applied to `/workspace/tasks/2026-07-18-chatlog-export-env-default.md` (left uncommitted — it's the swe-swe repo, and the file was uncommitted-by-you territory; say the word if you want it committed):
>
> 1. **New "Fork semantics" design decision** documenting the verified 0.8.14 behavior: session identity = sha256 of the `AGENT_CHAT_EVENT_LOG` path, `/api/fork` copies the JSONL to a new path → fork mints a new NN file with backfilled history (attachments best-effort). Explicitly marked as correct behavior, not a bug — a live source session and its fork must not interleave appends into one file.
> 2. **Follow-up reworded** to pin those semantics in the integration test instead of "should continue its file", with the note that opt-in fork-continuation would be an agent-chat `AGENT_CHAT_SESSION_ID` env feature, not swe-swe work.
> 3. **Rollout section updated**: 0.8.14 is published, so shipping the swe-swe change activates archiving for every new chat session immediately; Step 3's live e2e is unblocked now.
> 4. **Step 3 docs item**: added the silent-failure caveat (escaping path → export disabled with only a stderr warning).
> 5. Line refs corrected (`AGENT_CHAT_EVENT_LOG` append at main.go:5288; `buildSessionEnv` at :747).
>
> The task file is ready for whichever swe-swe session picks it up.

[Quick replies]
- Commit it in /workspace too
- Done for now

**USER**

> Export chat logs already? Do it and scrub

