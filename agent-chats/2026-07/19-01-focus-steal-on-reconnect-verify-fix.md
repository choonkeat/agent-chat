<!-- agent-chat export
title: Focus Steal On Reconnect Verify Fix
date: 2026-07-19
index: 01
slug: focus-steal-on-reconnect-verify-fix
session: 750e34fe48c3ee4a
agent: claude
version: 0.8.14 (b858a6b)
-->

# Focus Steal On Reconnect Verify Fix

_2026-07-19 · 01 · claude · agent-chat 0.8.14 (b858a6b)_

**USER**

> did we fix and release the textarea get focus on reconnect issue? verify our code. If user complains experiencing the same problem (focus get stolen from Terminal tab because it went into Agent Chat textarea due to reconnect), what problem could it be?

<small>took 10.3s</small><br>
**AGENT**

> Checking the code and git history for the textarea-focus-on-reconnect fix…

<small>took 1m 34s</small><br>
**AGENT**

> **Yes, a fix was released — but it's incomplete, and I found the residual bug.**
>
> **What shipped:** commit `102b934` "fix(client): don't steal focus on reconnect" — `enableInput()` gained a `focusInput` flag, and the `connected` handler passes `false` on reconnect (`app.js:2458`). First released in **v0.8.11**, current is v0.8.14. Verified present in today's code.
>
> **But the fix only covers the `connected` event.** Reconnects replay missed events through the *normal* live handlers (`/ws?cursor=lastSeq`), and those paths still call `enableInput()` with default focus:
>
> 1. **`agentMessage` with quick replies — `app.js:2480`** ← most likely culprit. Scenario: you're in the Terminal tab → iframe backgrounded, websocket drops → agent finishes and calls send_message while you're disconnected → on reconnect the missed message is replayed → `enableInput(data.quick_replies)` runs with no flag → `chatInput.focus()` → iframe yanks focus from the Terminal. This exactly matches the complaint.
> 2. **`historyEnd` deferred replies — `app.js:2468`** — same missing flag, fires on reconnect when reply chips aren't already rendered.
> 3. **`draw` — `app.js:2493`** — same pattern.
>
> Also worth knowing: even with **no reconnect**, every live agent reply with quick replies focuses the textarea (path 1) — so a user working in Terminal gets focus stolen whenever the agent finishes. That can easily be perceived as "the reconnect bug again."
>
> One more possibility if a user reports it: they're on **< 0.8.11**, or a server binary built before the fix (client JS is go:embed'ed — stale binaries serve old JS). The Connected bubble shows the version.
>
> **Suggested fix:** don't focus from any network-driven event — e.g. only call `chatInput.focus()` in `enableInput` when `document.hasFocus()` is already true (or focus was already inside the chat). Want me to implement that?

[Quick replies]
- Yes, fix it (focus only if document.hasFocus())
- Just thread focusInput=false through replay paths
- No, just wanted the diagnosis

<small>took 2m 38s</small><br>
**AGENT**

> (My wait for your reply got dropped by a timeout — the diagnosis above still stands. How would you like to proceed?)

[Quick replies]
- Yes, fix it (focus only if document.hasFocus())
- Just thread focusInput=false through replay paths
- No, just wanted the diagnosis

**USER**

> Yes, fix it (focus only if document.hasFocus())

<small>took 24.5s</small><br>
**AGENT**

> On it — gating all network-driven focus on document.hasFocus() in enableInput, then rebuilding and testing.

<small>took 7m 52s</small><br>
**AGENT**

> **Done — fix implemented and verified.**
>
> **The change** (`client-dist/app.js`, in `enableInput`):
> ```js
> if (focusInput !== false && document.hasFocus()) chatInput.focus();
> ```
> Network-driven callers (replayed `agentMessage`/`draw` after reconnect, live agent replies, `historyEnd`) can no longer grab focus when you're not in the chat document — which is exactly the iframe-steals-focus-from-Terminal case. User-driven focusing still works because the document has focus then.
>
> **Verified live** on a clean demo server (port 4999, killed after):
> - Background/unfocused tab: after connect + `enableInput`, the textarea is enabled but **not** focused (old code focused it).
> - Focused document: `enableInput` with default args still focuses the textarea.
> - Rebuilt the linux binary; served JS confirmed to contain the gate.
>
> **Tests:** `make unit-test` passes. E2E showed 18 failures — but I baselined by stashing my change and rerunning: **the same spec families fail on unmodified main** (`ERR_CONNECTION_REFUSED` on spawned test servers, varies per run). It's pre-existing environment flake, not this change. I saved that to memory so future sessions baseline first.
>
> The change is uncommitted in the working tree (only `client-dist/app.js`). Note the **running** agent-chat servers keep the old behavior until restarted/re-released. Want me to commit it?

[Quick replies]
- Commit it
- Commit + bump & publish a release
- Let me review the diff first

**USER**

> git commit and push

<small>took 19.9s</small><br>
**AGENT**

> **Committed and pushed** ✅
>
> `7ace943` — `fix(client): never grab focus from an unfocused document` — on `main`, pushed to `github.com:choonkeat/agent-chat.git` (f34c598..7ace943). Only `client-dist/app.js` was staged.
>
> Reminder: released binaries (≤ 0.8.14) and any currently running server still have the old behavior — the fix reaches users on the next `make bump` + publish, and this session's own server after a restart. Want a release now?

[Quick replies]
- Yes, bump & publish a release
- Later — done for now

**USER**

> /npm-login-and-publish

<small>took 12.0s</small><br>
**AGENT**

> Current published version of `@choonkeat/agent-chat` is **0.8.14**. This release carries one change: the focus-steal fix (`7ace943`). Bump to **0.8.15**?

[Quick replies]
- Yes, 0.8.15
- No — different version (I'll type it)

**USER**

> Yes, 0.8.15

<small>took 13.9s</small><br>
**AGENT**

> Bumped to 0.8.15, committed and pushed (966b5cf). npm auth OK (choonkeat). Cross-compiling all platform binaries now — will ask for your OTP when ready.

<small>took 15.8s</small><br>
**AGENT**

> All 6 platform binaries built and stamped at 0.8.15. Ready to publish 7 packages (`@choonkeat/agent-chat` + 6 platform packages). Please send a **fresh 6-digit npm OTP** now.

[Quick replies]
- (type your OTP)

**USER**

> [redacted OTP]

<small>took 59.9s</small><br>
**AGENT**

> **Released 0.8.15** 🎉 All 7 packages verified live on the registry:
>
> - `@choonkeat/agent-chat@0.8.15` (main)
> - `@choonkeat/agent-chat-darwin-arm64` / `-darwin-x64` / `-linux-arm64` / `-linux-x64` / `-win32-arm64` / `-win32-x64` — all at 0.8.15
>
> Ships the focus fix (`7ace943`: never grab focus from an unfocused document) on top of 0.8.14. Release commit `966b5cf` is pushed to main. Anything else?

[Quick replies]
- Export chat logs
- All done, thanks

**USER**

> When we stream chat logs to agent chat dir, what is the filename convention? Will there be conflicts?

<small>took 52.8s</small><br>
**AGENT**

> **Convention:** `{YYYY-MM-DD}-{NN}-{slug}.md` inside `AGENT_CHAT_EXPORT_DIR` (e.g. `agent-chats/2026-07-19-01-focus-steal-on-reconnect-verify-fix.md`).
>
> - `NN` = per-day running index (2 digits, grows to 3 after 99). `nextDailyIndex` (tools.go:295) scans the dir for `{date}-NN-…` and returns max+1.
> - Every session starts as `…-untitled.md`; `set_chat_title` renames to the slugified title **keeping the original date+NN**, and rewrites the file in place.
> - Attachments go to `assets/` with **content-sha filenames**, so identical files dedupe and can't collide.
>
> **Conflict handling — the creation race is solved properly:**
> - The `NN` claim is atomic: the file is created with `O_CREATE|O_EXCL` (chatlogstream.go:118). If two sessions race between the scan and the create, the loser gets `EEXIST` and retries with `NN+1`. Two sessions can never clobber each other's log.
> - Rename collisions can't happen either: the rename keeps the session's uniquely-claimed `date-NN` prefix, so even two same-day sessions titled identically end up as `…-01-auth-fix.md` and `…-02-auth-fix.md`.
> - Restarts don't mint duplicates: the file header has a `session:` line, and a restarted/forked process **resumes** its own file instead of claiming a new NN.
>
> **Remaining (minor) edge cases:**
> 1. `SetTitle` writes the new path with a plain `os.WriteFile` — no `O_EXCL` — so a *manually created* file that happens to match `{date}-{NN}-{slug}.md` would be silently overwritten. Only reachable by hand-naming a file into a claimed NN slot.
> 2. Two concurrent sessions both regenerate `index.html` — last writer wins, but it's derived from the on-disk glob, so it self-heals on the next event.
> 3. If a fork ever shared the same `session:` id while the original was still alive, both would append to one file (resume-by-session is intentional for restarts; concurrent duplicate ids are assumed not to happen).
>
> Short answer: the convention is date + atomic per-day counter + slug, and normal concurrent-session conflicts are designed out.

[Quick replies]
- Thanks, all done
- Harden SetTitle with O_EXCL-style check

**USER**

> When title changes and hence file renames, then would new content append to old filename or new filename

<small>took 13.4s</small><br>
**AGENT**

> **New filename — the switch is atomic from the writer's point of view.**
>
> `SetTitle` (chatlogstream.go:248) does the whole swap while holding the stream's mutex:
> 1. closes the current file handle,
> 2. deletes the old `…-untitled.md` (or previous title),
> 3. writes a **full rewrite** of the entire chat (header + all history re-rendered) to the new `{date}-{NN}-{new-slug}.md`,
> 4. reopens the *new* path with `O_APPEND` and stores that handle in `s.f`.
>
> All subsequent appends go through `HandleEvent`, which takes the same mutex and writes to `s.f` — so after a rename, every new bubble lands in the new filename. There's no window where content can go to the old name: any event arriving mid-rename blocks on the mutex until the swap finishes. The old file no longer exists afterward, so no stale duplicate is left behind.
>
> One edge worth knowing: if the reopen at step 4 fails, `s.f` stays nil and `HandleEvent` silently drops events (chatlogstream.go:381) — the stream goes dark rather than writing to the wrong file. And because step 3 is a full rewrite from in-memory history (not a file move), nothing is lost even though the old file is deleted.

[Quick replies]
- Got it, thanks

**USER**

> Instead of `-untitled.md` make it `-untitled-{session uuid env if not blank}.cmd`
>
> > if the reopen at step 4 fails, s.f stays nil and HandleEvent silently drops events (chatlogstream.go:381) — the stream goes dark rather than writing to the wrong file.
>
> No robust way to survive file rename?

<small>took 1m 41s</small><br>
**AGENT**

> Found it — swe-swe sets a SESSION_UUID env var. Implementing: (1) provisional file becomes `{date}-{NN}-untitled-{SESSION_UUID}.md` when the env is non-blank (assuming `.cmd` in your message was a typo for `.md` — everything globs *.md); (2) making SetTitle rename fail-safe: build+open the new file first, only then swap and delete the old, so a failure leaves the stream appending to the old filename instead of going dark.

**USER**

> Why different files? Renaming files but keep writing to the same fd “just works” right?

<small>took 41.7s</small><br>
**AGENT**

> You're right that `os.Rename` keeps the fd valid — same inode, appends keep landing in the renamed file. The reason SetTitle can't be *just* a rename: **the title is baked into the file's content**, not only its name. The header comment (`title:` line), the `# H1`, and the byline all carry the old title, so a retitle is necessarily a **full rewrite** of every byte, from the in-memory event history. Rename alone would leave a file named `auth-bug-fix.md` whose header and H1 still say "Untitled".
>
> Given a full rewrite is unavoidable, there are two ways to order it:
>
> 1. **Rename + truncate in place, keep the same fd** — `os.Rename(old→new)`, then `f.Truncate(0)` and write the new render through the existing handle. Simpler, fd never changes. But there's a window where the file on disk is empty/partial: if the process dies between truncate and write-complete, the log is corrupted, and resume-after-restart only *appends* (it never re-renders), so the damage would stick.
>
> 2. **Write the new file fully, then swap** (what the code now does): the complete new content exists on disk *before* the old name is retired, so at every instant there's at least one complete, valid file. The cost is the fd swap you noticed — `os.WriteFile` creates a different inode, so the O_APPEND handle must be reopened on it; keeping the old fd would mean appending to a deleted inode.
>
> So "rename and keep writing just works" is true mechanically, but it optimizes the part that was never the problem — the expensive part is the content rewrite, and once you're rewriting, build-new-then-swap is the crash-safe order.
>
> Meanwhile, the two changes you asked for are implemented and unit-tested (all green):
> - Provisional name is now `{date}-{NN}-untitled-{SESSION_UUID}.md` when swe-swe's `SESSION_UUID` env is set (display title stays "Untitled"; retitling drops the suffix). I assumed `.cmd` in your message was a typo for `.md`.
> - `SetTitle` is now fail-safe: on any error the stream keeps appending to the old filename (and old metadata) instead of going dark; a later retry succeeds.
>
> Commit and push?

[Quick replies]
- Yes, commit + push
- Switch to rename+truncate instead
- Show me the diff first

**USER**

> I _think_ we need a mcp function to stop streaming. Otherwise, when sessions choose to wrap up, git add commit the chat log, .. the next agent reply would modify the file again causing a dirty git status. So, with such a mcp function, we can truly close out — and in terms of sequence of events, should we require a session title to “close out” ? Discuss

<small>took 1m 0s</small><br>
**AGENT**

> Agreed — there's a real gap here, and the existing plumbing nearly solves it. My take:
>
> **The core problem is self-reference:** the "I've committed the chat log ✅" reply is itself a chat event, so with streaming on, committing the log *always* dirties it one turn later. No commit ordering escapes that — you need a way to stop appending.
>
> **Proposal: `chatlog_close`** — the finalize twin of `chatlog_optout` (which stops *and deletes*; close stops *and keeps*). It would: flush + close the file, set the stream to stopped, regenerate `index.html` one final time, and **return the exact paths to commit** (the `.md`, its `assets/` files, `index.html`, viewer assets) so the agent can `git add` precisely by name.
>
> **The elegant part — reopening is already free.** `set_chat_title` re-arms a stopped stream with a **full rewrite from in-memory history** (that's how it revives after `chatlog_optout` today). So if the user says "actually, one more thing" after close-out, nothing is lost: the next `set_chat_title` (or a `chatlog_reopen`) backfills every message that was dropped while closed, and the file goes dirty *only when it genuinely has new content* — which is when you'd want to re-commit anyway. Closed ≠ data loss; it's just deferred rendering.
>
> **Should close require a title? Yes — and I'd bake it in:**
> - `chatlog_close(title?)`: if the file is still `-untitled…` and no `title` arg is given → **error** ("set a title first"). Otherwise it renames + closes in one call.
> - Rationale: (1) committing `…-untitled-{uuid}.md` forever is exactly what titles exist to prevent; (2) sequencing matters for git — retitling *after* commit shows up as delete+add churn, so the rename must happen at or before the close; (3) a hard error at the last gate is cheap — the agent fixes it in one call.
>
> **Recommended close-out sequence:**
> 1. deliver final answer, get the user's last word (still archived)
> 2. `chatlog_close(title: …)` — rename if needed, freeze, final index regen, returns paths
> 3. `git add <returned paths> && git commit`
> 4. "Committed ✅" send_message — deliberately *not* archived (meta-conversation, acceptable loss)
>
> This also slots cleanly into the `commit-session-chat-log` skill flow on the swe-swe side.
>
> One open question: should `chatlog_close` be idempotent-with-warning if called while already closed (I'd say yes), and should a *user* message arriving after close auto-reopen (I'd say **no** — auto-reopen would dirty the repo from a mere "thanks, bye"; explicit reopen keeps intent clear).
>
> Want me to build `chatlog_close` this way? (The SESSION_UUID-suffix + robust-rename work from earlier is still uncommitted — I'd commit that first, separately.)

[Quick replies]
- Yes, build chatlog_close as proposed
- Build it, but auto-reopen on new user messages
- Commit the earlier work first, then discuss more

**USER**

> Wouldn’t chatlog_close with title (if renamed) causes churn to the filename and disrupt what the user had git committed?
>
> Idempotent: yes
>
> Note: stop streaming only refer to the .md file streaming into cwd/agent-chats; the jsonl etc existing streaming should still continue
>
> Discuss

<small>took 36.5s</small><br>
**AGENT**

> Good catch — the churn risk is real, but only in one specific case, and the tool's policy can make that case explicit rather than accidental:
>
> **When does renaming disrupt a prior commit?** Only if the file was *already committed under some name* before close. In the normal single-cycle flow (title → close → commit) there's no churn: the rename happens before git ever sees the file. The churn cases are (a) someone committed the log mid-session and close passes a *different* title, or (b) reopen → close again with a new title. The server can't know git state (the export dir may not even be a repo, and I don't want agent-chat shelling out to git), so the fix is policy, not detection:
>
> **Refined `chatlog_close(title?)` semantics — rename only ever fills a blank:**
> - *untitled + title given* → rename (the first and only naming), freeze, final index regen, **return the paths to commit**
> - *untitled + no title* → error: "set a title first"
> - *already titled + no title, or same title* → freeze, return paths — **never renames**
> - *already titled + different title* → **error, refuses to rename.** Retitling an already-named file must be a deliberate, separate `set_chat_title` call — that makes filename churn a conscious choice with known git cost, never a side effect of closing.
> - *already closed* → idempotent success, returns the same paths (your "yes")
>
> Two softeners for the deliberate-retitle case: `set_chat_title` keeps the `{date}-{NN}` prefix, so only the slug changes, and git's rename detection (`git log --follow`, similarity index) keeps history connected — churn is one delete+add in a commit, not lost history.
>
> **On scope — fully agreed, and it's already how the code is shaped:** `chatlog_close` would touch only `chatLogStream` (the `.md` in `AGENT_CHAT_EXPORT_DIR`). The JSONL event log (`AGENT_CHAT_EVENT_LOG`) is a separate writer wired independently in main.go and keeps recording. That's actually what makes close safe: messages arriving while the `.md` is frozen still land in the JSONL, so a later reopen (`set_chat_title` re-arm) or a restart can always rebuild the complete `.md` from history. Close = stop *rendering*, never stop *recording*.
>
> Build it with these semantics?

[Quick replies]
- Yes, build it
- One tweak first (I'll describe)
- Commit earlier work first

**USER**

> Yes, build it

<small>took 40.4s</small><br>
**AGENT**

> Building chatlog_close with the agreed semantics. Committing the SESSION_UUID-suffix + robust-rename change first (locally) so each lands as its own clean commit.

<small>took 2m 50s</small><br>
**AGENT**

> **`chatlog_close` is built, tested, and smoke-verified live.** ✅
>
> **Semantics (as agreed):**
> - Freezes this session's `.md` only — the JSONL event log keeps recording (`CloseOut` touches only `chatLogStream`).
> - *Untitled + no title* → error; *untitled + title* → names it in the same call; *already titled* → **never renames** (matching title = no-op, different title = refusal pointing at `set_chat_title`).
> - Idempotent; re-running returns the same paths.
> - Returns the exact paths to `git add` (repo-relative): the `.md`, its session assets, `index.html`, `viewer.css/js`.
> - `set_chat_title` re-opens with a full-history rewrite, so messages arriving while frozen are backfilled — close is deferred rendering, never data loss. After `chatlog_optout` (file deleted), close errors instead.
>
> **Live smoke test** against a clean demo binary over stdio MCP:
> - provisional file correctly came up as `2026-07-19-01-untitled-deadbeef-1234.md` (the SESSION_UUID suffix working end-to-end)
> - `chatlog_close` with no title → `error: export is still untitled … pass title, or call set_chat_title first`
> - with `title: "Smoke Test Chat"` → renamed to `…-01-smoke-test-chat.md`, froze it, and returned the four commit paths.
>
> All unit tests pass (two new: full close/freeze/refuse/reopen-backfill lifecycle, and close-after-optout). README updated.
>
> **Git state:** the earlier SESSION_UUID + fail-safe-rename work is committed locally as `2864ff6` (not yet pushed). The `chatlog_close` feature is uncommitted on top. Shall I commit it and push both?

[Quick replies]
- Commit + push both
- Also bump & publish 0.8.16
- Show the diff first

**USER**

> Git commit

<small>took 13.3s</small><br>
**AGENT**

> **Committed** ✅ — two local commits on `main`, not yet pushed:
>
> - `810c4c3` feat(chatlog): chatlog_close — freeze the export for a clean git commit
> - `2864ff6` feat(chatlog): session-UUID untitled names + fail-safe retitle
>
> (origin/main is at `966b5cf`, the 0.8.15 release.) Say the word when you want them pushed and/or released as 0.8.16 — note the new tools only reach sessions once released and restarted.

[Quick replies]
- Push
- Push + bump & publish 0.8.16
- Hold for now

**USER**

> Push

<small>took 10.8s</small><br>
**AGENT**

> **Pushed** ✅ — `main` on origin now has both commits: `2864ff6` (SESSION_UUID untitled names + fail-safe retitle) and `810c4c3` (chatlog_close). Not yet released to npm — the new tools reach sessions once 0.8.16 is published and servers restart. Anything else?

[Quick replies]
- Bump & publish 0.8.16
- All done for now

**USER**

> /npm-login-and-publish

<small>took 26.5s</small><br>
**AGENT**

> Bumped to 0.8.16, committed and pushed (b49d6cc). npm auth OK. Cross-compiling all platform binaries — will ask for your OTP when ready.

<small>took 15.0s</small><br>
**AGENT**

> All 6 platform binaries built and stamped at **0.8.16**. Ready to publish the 7 packages — please send a **fresh 6-digit npm OTP**.

[Quick replies]
- (type your OTP)

**USER**

> [redacted OTP]

<small>took 1m 6s</small><br>
**AGENT**

> **Released 0.8.16** 🎉 All 7 packages verified live on the registry (win32-arm64 took ~10s of propagation lag):
>
> - `@choonkeat/agent-chat@0.8.16` + the 6 platform packages, all at 0.8.16
>
> Ships on top of 0.8.15:
> - `chatlog_close` — freeze the chat-log export for a clean git commit (`810c4c3`)
> - SESSION_UUID-tagged untitled filenames + fail-safe `set_chat_title` rename (`2864ff6`)
>
> Release commit `b49d6cc` pushed. Running sessions (including this one) get the new tools after their server restarts on 0.8.16. Anything else?

[Quick replies]
- All done, thanks
- Update the swe-swe commit-chat-log skill to use chatlog_close

