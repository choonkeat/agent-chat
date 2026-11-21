<!-- agent-chat export
title: What S New Panel With Unread Dot
date: 2026-11-21
index: 01
slug: what-s-new-panel-with-unread-dot
session: 4df6836862f307c7
agent: claude
version: 0.20.0 (b3f5d83)
-->

# What S New Panel With Unread Dot

_2026-11-21 · 01 · claude · agent-chat 0.20.0 (b3f5d83)_

**USER**

> At the top right of agent chat, there’s download and message icon. I’m thinking of a what’s new (content pulled from changelog on github?). There’s an unread indicator unless user has opened it. When we release new npm version, it becomes unread etc. So localStorage to keep track perhaps (but we are usually used inside iframe of swe-swe)

<small>took 10.2s</small><br>
**AGENT**

> Looking at the header icons, CHANGELOG, and how iframe storage works today to sketch a "What's new" design.

<small>took 18.6s</small><br>
**AGENT**

> **Pick one: OK to build it as below?** About 2 hours including an E2E test.
>
> The iframe problem is already solved in this codebase. Theme and message style are stored in **cookies**, not localStorage (`client-dist/app.js:3617`). Each session runs on its own port, and localStorage is per port, so it would reset every session. Cookies ignore the port, so they follow you across sessions inside swe-swe. "Last seen version" would work the same way.
>
> **Plan (5 steps)**
> 1. **Content:** build `CHANGELOG.md` into the binary (`go:embed`) and serve it at `api/changelog`. Don't fetch it from GitHub: the embedded copy works offline, always matches the version you're running, and has no rate limits or CORS problems.
> 2. **Button:** a ✨ icon to the left of download in the header, with a dot when there's something you haven't read.
> 3. **Unread rule:** a cookie `agent-chat-seen-version`. The dot shows when the running version is newer than the cookie. Opening the panel sets the cookie and clears the dot.
> 4. **Panel:** slides down like Settings and renders the newest 3 releases as markdown, with "Full changelog →" linking to GitHub.
> 5. **Test:** E2E. Fresh cookie with an older version → dot shows → open the panel → dot gone → reload → still gone.
>
> **One choice for you:** on a brand-new browser with no cookie, do you want
> - **A)** the dot (the user sees what's new right away), or
> - **B)** no dot: set the cookie quietly so only *future* releases light it up?
>
> I'd pick **B**: new users shouldn't get a notification about changes they never saw before.

[Quick replies]
- Build it, option B
- Build it, option A
- Change something first

**USER**

> Build it, option B

<small>took 5.1s</small><br>
**AGENT**

> Step 0 of 5: starting build, option B. Reading the server routes and settings-panel code first.

<small>took 23.1s</small><br>
**AGENT**

> Step 1 of 5: writing the Go side. It builds CHANGELOG.md into the binary, adds an `api/changelog` route, and puts the latest version into the page.

<small>took 46.8s</small><br>
**AGENT**

> Steps 1 and 2 of 5 done: the Go route, the header button and dot, and the panel's HTML and CSS. Next: the JS for the unread cookie and the panel.

<small>took 1m 8s</small><br>
**AGENT**

> Steps 1–4 of 5 done; unit tests pass. Step 5: running the new E2E spec (3 tests, about 1 minute).

<small>took 1m 32s</small><br>
**AGENT**

> **The "What's new" button is built, but not committed yet.** Screenshots above: the red dot, then the open panel.
>
> **What it does**
> 1. A ✨ button sits left of download. A red dot shows when the running release is newer than the one this browser last opened the panel on.
> 2. Clicking it opens a panel with the newest 3 releases from `CHANGELOG.md` (built into the binary, so it works offline), plus a "Full changelog →" link to GitHub. Opening it clears the dot.
> 3. The "last seen" release is stored in the cookie `agent-chat-seen-version`, which follows you across sessions and ports inside swe-swe.
> 4. Option B: a brand-new browser gets no dot. Only releases after its first visit light it up.
> 5. Only one of Settings and What's new is open at a time.
>
> The dot compares against the newest `## [x.y.z]` heading in the CHANGELOG, not the binary's version. So `make bump` plus a new CHANGELOG entry is what lights it up for users.
>
> **Tests:** `make unit-test` passes, with new Go tests in `whatsnew_test.go`. The new `e2e/whats-new.spec.cjs` passes 3/3. I haven't run the full `make test` (about 30 minutes).
>
> **Files:** `whatsnew.go`, `whatsnew_test.go`, `main.go` (route plus `WHATS_NEW_LATEST` in the page), `client-dist/index.html`, `client-dist/app.js`, `client-dist/style.css`, `e2e/whats-new.spec.cjs`.
>
> **Next step:** say "commit" and I'll stage those 7 files by name and commit.
>
> [whats-new-dot.png #1]
> [whats-new-panel.png #2]

[Quick replies]
- Commit
- Commit, then run full make test
- Change something first

**USER**

> Content comes from github @CHANGELOG.md?

<small>took 6.3s</small><br>
**AGENT**

> Answering: where the panel's content comes from.

<small>took 4.0s</small><br>
**AGENT**

> **No. It's this repo's `CHANGELOG.md`, built into the binary when it's compiled** (`//go:embed CHANGELOG.md` in `whatsnew.go`). Only the "Full changelog →" link goes to GitHub.
>
> What that means in practice:
> 1. The panel shows the release notes for the version you are **running**, after you've upgraded.
> 2. It works offline and makes no request to GitHub, so there are no rate limits and no CORS problems inside swe-swe's iframe.
> 3. It **cannot** tell you that a newer version exists. That would need a GitHub (or npm) fetch.
>
> If you also want "a new version is out, upgrade" alerts, that's a separate feature: fetch the CHANGELOG from GitHub raw and compare it to the running version. About 1 hour. It would sit alongside this panel, not replace it.
>
> **Next step:** keep it as is and commit, or add the GitHub check too?

[Quick replies]
- Keep as is, commit
- Add the GitHub new-version check too
- Switch content to GitHub instead

<small>took 2m 7s</small><br>
**AGENT**

> **Waiting on your pick:** keep the panel as is and commit, or add a GitHub check for new versions too?
>
> Where things stand: the What's new panel is built and tested but not committed. Its content is this repo's `CHANGELOG.md`, built into the binary, not fetched from GitHub. That means it can't tell you a newer version exists; a GitHub check would add that (about 1 hour).

[Quick replies]
- Keep as is, commit
- Add the GitHub new-version check too
- Switch content to GitHub instead

**USER**

> Keep as is, commit

<small>took 3.7s</small><br>
**AGENT**

> Committing the 7 What's new files, staged by name.

<small>took 10.3s</small><br>
**AGENT**

> **Committed `484c0c8`** (feat(ui): What's new panel with an unread dot). It's on local `main` and not pushed.
>
> 1. I staged the 7 files by name. `agent-chats/2026-11/` is still untracked, as before.
> 2. The dot lights for users on the next release whose CHANGELOG entry is newer than v0.20.0.
>
> **Next step:** run the full `make test` (about 30 minutes) before the next release?

[Quick replies]
- Run full make test
- Not now

**USER**

> Run full make test

<small>took 3.5s</small><br>
**AGENT**

> Starting the full `make test`: unit tests plus about 170 serial E2E tests, around 30 minutes. I'll post progress along the way.

<small>took 16.8s</small><br>
**AGENT**

> `make test` is underway: building for all platforms, then unit tests. E2E comes after that. No failures so far.

<small>took 3m 58s</small><br>
**AGENT**

> E2E: 58 of 267 done (267 counts retries), 0 failures so far.

<small>took 4m 0s</small><br>
**AGENT**

> E2E: 83 of 267, 0 failures.

<small>took 4m 3s</small><br>
**AGENT**

> E2E: 108 of 267, 0 real failures.

<small>took 3m 57s</small><br>
**AGENT**

> E2E: 129 of 267, 0 real failures.

<small>took 3m 59s</small><br>
**AGENT**

> E2E: 149 of 267, 0 real failures.

<small>took 4m 0s</small><br>
**AGENT**

> E2E: 165 of 267, 0 real failures.

<small>took 3m 59s</small><br>
**AGENT**

> E2E: 177 of 267, 0 real failures.

<small>took 2m 6s</small><br>
**AGENT**

> E2E: past 177 of 267, 0 real failures. Still running, about 15 more minutes; I'll post the final result here.

<small>took 3m 58s</small><br>
**AGENT**

> E2E: 193 of 267, 0 failures.

<small>took 3m 59s</small><br>
**AGENT**

> E2E: 235 of 267, 0 failures. Almost done.

<small>took 3m 35s</small><br>
**AGENT**

> **`make test` passed (exit 0): 265 passed, 1 flaky, 1 skipped in 41 minutes.**
>
> 1. Unit tests: pass.
> 2. E2E: the 3 new `whats-new` tests pass.
> 3. Flaky: `markdown-nested-lists.spec.cjs:96`. It passed on retry and sits in markdown rendering, which this change doesn't touch.
> 4. Skipped: the known 2-server test that is skipped while tests are pinned to one port.
>
> Commit `484c0c8` is ready on local `main`, not pushed.
>
> **Next step:** push `main`, or keep it local until the next release?

[Quick replies]
- Push main
- Keep local

**USER**

> Push main

<small>took 5.1s</small><br>
**AGENT**

> Pushing `main`.

<small>took 13.3s</small><br>
**AGENT**

> **Pushed: `origin/main` is now at `484c0c8`** (`b3f5d83..484c0c8`).
>
> All 3 steps are done: built, tested (`make test` passed), and pushed. The What's new panel ships with the next npm release. Its dot appears for users once that release has a CHANGELOG entry newer than v0.20.0.
>
> **Next step:** nothing is needed until you cut the next release with `/npm-login-and-publish`.

[Quick replies]
- Thanks

**USER**

> /commit-log-then-end

<small>took 3.2s</small><br>
**AGENT**

> Running /commit-log-then-end: saving this session's chat log, then ending the session.

