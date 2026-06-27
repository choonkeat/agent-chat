<!-- agent-chat export
title: Parent Url Relative Links And Autocomplete Fix
date: 2026-06-27
index: 01
slug: parent-url-relative-links-and-autocomplete-fix
agent: claude
version: 0.8.1 (100bf8e)
-->

# Parent Url Relative Links And Autocomplete Fix

_2026-06-27 · 01 · claude · agent-chat 0.8.1 (100bf8e)_

**USER**

> Since agent-chat runs in iframe when in swe-swe, is there a way we can know the parent iframe’s url? see our recent fix wrt relative link — i need to make relative links to be based off parent window url; not my own’s

<small>took 5m 16s</small><br>
**AGENT**

> Sorry — that was me kicking off `make e2e-test`, which spins up fresh browser contexts/pages over CDP per test (this spec file has ~15 tests now). That's the window storm. I've stopped; nothing destructive happened.
>
> Here's what I actually changed (code is written, build done — only the test run was the noisy part):
>
> **The problem:** relative markdown links resolve against agent-chat's own iframe URL, but you want them resolved against the *parent* (swe-swe) window URL.
>
> **The catch:** if the iframe is cross-origin to the parent, `window.parent.location.href` throws — the browser blocks it. So there's no single guaranteed way; I layered best-effort sources in `client-dist/app.js`:
> 1. `window.parent.location.href` — full URL, works only if same-origin
> 2. a URL the parent posts to us (`{type:'agent-chat-parent-url', url}`) — full URL, but needs swe-swe to answer our `agent-chat-request-parent-url` postMessage
> 3. `document.referrer` — full URL if referrer policy allows
> 4. `window.location.ancestorOrigins` — origin only, reliable cross-origin
>
> Then a `resolveAgainstParent(url)` helper rewrites relative `[text](url)` and `![](url)` hrefs/srcs via `new URL(url, parentBaseUrl)`. Absolute URLs pass through untouched; with no parent base it's a no-op (old behaviour).
>
> **Key question for you:** is agent-chat same-origin with the swe-swe parent, or a different origin/port? That decides whether option 1 just works, or whether we need the swe-swe side to reply to the postMessage (option 2) for full-path accuracy. Cross-origin without parent cooperation only gives us the parent *origin*, which resolves `/leading/slash` links but not `bare/relative` ones.
>
> How do you want to verify — should I run the unit-style renderMarkdown tests in a way that doesn't open windows, or skip the E2E run for now?

[Quick replies]
- It's cross-origin — wire the swe-swe postMessage reply too
- It's same-origin, option 1 is enough
- Skip E2E for now, just show me the diff
- Run tests but don't open visible windows

**USER**

> there’s no guarantee of same host, let’s just go with swe-swe passing the url prefix via a url query string ok? Unit test that, once it works, leave a <swe-swe-repo>/tasks (or <swe-swe-repo>/task? Forgot if plural) file to have the agent make the necessary change to pass the url to agent-chat window — discuss
>
> Do we need to clean up our chrome windows? Memory is precious

<small>took 3m 24s</small><br>
**AGENT**

> Done. Went with the query-string approach.
>
> **Chrome cleanup:** nothing to clean — checked `ps`, the only chromiums are the swe-swe-owned MCP browsers (ports 6000–6002, running since Jun13–17). The interrupted Playwright runner already closed its per-test contexts when it died; no stray test servers. Memory's fine.
>
> **Agent-chat side (this repo) — shipped:**
> - `client-dist/app.js`: reads `parent_url` from the iframe's query string into `parentBaseUrl`; new `resolveAgainstParent(url)` rewrites relative `[text](url)` link hrefs **and** `![alt](url)` image srcs via `new URL(url, parentBaseUrl)`. Absolute/`//host` URLs pass through; no `parent_url` → no-op (old behaviour preserved).
> - `e2e/markdown-images.spec.cjs`: +6 tests — leading-slash, no-leading-slash, image src, absolute-unchanged, no-base-fallback, and one that actually loads `?parent_url=…` and verifies the wiring. **15/15 pass.** Binary rebuilt (linux-x64) so the embedded client matches.
> - Ran serialized (`--workers=1`, just this spec) so no window storm.
>
> **Task file for the swe-swe change:** `tasks/2026-06-27-parent-url-for-relative-links.md`. Core ask for that agent:
>
> > When building the agent-chat iframe `src`, append `parent_url` = URL-encoded top-level `window.location.href`:
> > ```js
> > base.searchParams.set('parent_url', window.location.href);
> > ```
>
> **Two things I flagged in the task file worth your call now:**
> 1. **What to pass** — I recommend the full `window.location.href` (lets `new URL()` handle both `/leading-slash` *and* bare `relative/path` links). If you'd rather pin just the origin or a fixed workspace-root prefix, say so and I'll adjust the task.
> 2. **SPA staleness** — the param is captured when the iframe src is built; if swe-swe navigates without recreating the iframe, it goes stale. I deliberately did NOT add a postMessage update channel (you chose query-string for simplicity). Fine as v1?
>
> Note: this session's running server still serves the old embedded client until it's restarted — I won't restart it (that'd kill this session). Want me to leave it, or is a restart handled elsewhere?

[Quick replies]
- Full href + v1 staleness is fine — leave as is
- Pin just the origin instead
- Pin a fixed workspace-root prefix
- Add the postMessage update channel too
- How do I restart the server to see it live?

**USER**

> Full href + v1 staleness is fine — leave as is

<small>took 4m 8s</small><br>
**AGENT**

> All wrapped up ✅
>
> - **agent-chat side (shipped):** `client-dist/app.js` reads `parent_url` from the iframe query string and resolves relative link/image URLs against it; binary rebuilt. 15/15 tests pass in `e2e/markdown-images.spec.cjs`.
> - **swe-swe side (handoff):** `tasks/2026-06-27-parent-url-for-relative-links.md` — pass full `window.location.href` as `parent_url`, query-string only.
>
> Changes are uncommitted on `main`. Want me to commit them, or anything else?

[Quick replies]
- Commit the changes
- Leave uncommitted
- Run the full make test suite
- Nothing else, thanks

**USER**

> git commit. make build. Run full test suite

**USER**

> Stop the e2e with parent url iframe is making chrome crazy — fix our test?

**USER**

> What is the absolute path of the parent url task file? I cannot find it

**USER**

> Stop

<small>took 1m 48s</small><br>
**AGENT**

> The E2E run already finished on its own (it errored out), so nothing is spawning windows right now — I've stopped.
>
> **Task file absolute path:**
> `tasks/2026-06-27-parent-url-for-relative-links.md` (in the agent-chat repo)
> It's committed in `1d1480e`. If it's not visible on disk for you: `git show 1d1480e:tasks/2026-06-27-parent-url-for-relative-links.md`.
>
> **Full-suite result:** 42 passed, **1 failed** — the failure is unrelated to my change: `autocomplete.spec.cjs › typing :zzzznotanemoji shows no results` (emoji autocomplete). All 7 markdown/`parent_url` tests passed. `make build` succeeded for all platforms; commit is in.
>
> **The window storm** isn't specific to my parent_url test — the whole E2E harness opens a fresh browser **context + page per test** over CDP into the shared visible Chrome you're watching (~43 in parallel). My tests just `page.goto(...)` like every other spec; none create an iframe.
>
> Options to calm it (your call — I've touched nothing):
> 1. **`--workers=1`** for E2E → one window at a time, no test-logic change. Simplest.
> 2. **Reuse a single shared page** instead of newContext-per-test → far fewer windows, but the spec's comment says per-test contexts were deliberate to kill cross-test flake. Trade-off.
> 3. **Leave as-is**, just run targeted specs serialized when needed.
>
> How do you want to proceed — and should I look at that pre-existing autocomplete failure separately?

[Quick replies]
- Go with option 1 (--workers=1)
- Option 2 — shared page
- Option 3 — leave tests, run targeted
- Investigate the autocomplete failure
- Nothing for now

**USER**

> Move the task file into <swe-swe-repo>/tasks not here!

<small>took 1m 2s</small><br>
**AGENT**

> ✅ Moved. The task file now lives in the **swe-swe** repo where its agent will find it:
> `<swe-swe-repo>/tasks/2026-06-27-parent-url-for-relative-links.md` (currently untracked in the swe-swe repo).
>
> Removed it from the agent-chat repo and amended commit `1f50a33` — that commit is now just `client-dist/app.js` + `e2e/markdown-images.spec.cjs` (its body points to the swe-swe task for the handoff). Verified the file is gone from agent-chat's `tasks/`.
>
> Three things still open — your call on each:
>
> 1. **Commit the task file in the swe-swe repo?** It's untracked at `<swe-swe-repo>`. Want me to `git -C <swe-swe-repo> add/commit` it, or leave it untracked for you/the swe-swe agent?
> 2. **The E2E window storm** — still undecided. Cheapest calm-down is running E2E with `--workers=1` (one window at a time, no test changes). Want that as the default?
> 3. **Pre-existing test failure** — `autocomplete.spec.cjs › typing :zzzznotanemoji shows no results`. Unrelated to my change. Investigate, or leave it?

[Quick replies]
- Commit the swe-swe task file too
- Leave swe-swe file untracked
- Set E2E to --workers=1
- Investigate the autocomplete failure
- Nothing else for now

**USER**

> Investigate the autocomplete failure

<small>took 3m 1s</small><br>
**AGENT**

> Investigated — root cause found, and it's **unrelated to my parent_url change** (that only touches `renderMarkdown`).
>
> **It's a flaky test — a race between client-cache population and keystrokes.**
>
> `typeAndWait` types `:zzzznotanemoji` one char at a time (50ms apart):
> - **Server path (passes):** if keystrokes outrun the `:z` fetch, the final query hits the server, which returns 0 results *with* `info: "No emoji matching \"zzzznotanemoji\""` (main.go:690) — status contains the query. ✓
> - **Cache path (fails — what we hit):** if the `:z` fetch resolves first, it seeds the client cache; later keystrokes filter client-side (app.js:1211). When the filter empties, the client shows a **generic** `"No results"` (app.js:999) — never includes the query, never asks the server. The failure snapshot confirmed "No results". ✗
>
> The assertion `statusText.toContain('zzzznotanemoji')` (line 309) only holds on the server path. Server behaviour itself is correct + covered by a Go unit test (main_test.go:702). So this E2E has been latently flaky since the emoji feature landed.
>
> **Recommended fix — one line, also a real UX win:** in the cache-filter branch, only short-circuit when there are actual matches; if filtering empties, fall through to the server fetch so the helpful *"No emoji matching X"* always shows:
> ```js
> acSortByQuery(filtered, query);
> if (filtered.length > 0) {        // only short-circuit on real matches
>   if (acTriggerPos >= 0) acShow(filtered, query);
>   return;
> }
> // else fall through to the server fetch for the informative status
> ```
> Makes the test deterministic, improves the no-match UX, and doesn't affect the "cache filter matches hint labels" test (that path has matches).
>
> Want me to apply this fix (separate commit, since it's a different subsystem from the parent_url work)? I'd rebuild and run the autocomplete spec with `--workers=1` to confirm — minimal windows.

[Quick replies]
- Apply the fix (separate commit)
- Just fix the test assertion instead
- Leave it for now, don't change
- Apply fix but don't commit yet

**USER**

> Apply the fix (separate commit)

<small>took 3m 25s</small><br>
**AGENT**

> Fix applied and committed ✅ — and noted your preference (I'll only ever reply via send_message from now on).
>
> **Autocomplete flaky-test fix — committed `bb11e76`** (`client-dist/app.js`, separate commit from the parent_url work):
> - `acFetch` now only short-circuits the client cache-filter path when it has actual matches; if filtering empties, it falls through to the server fetch so the provider's informative status (*"No emoji matching X"* / *"No files matching X in DIR"*) always shows.
> - Binary rebuilt. Verified: ran the full autocomplete spec with `--workers=1 --repeat-each=3` → **33/33 passed**, including the previously-flaky `:zzzznotanemoji` test all 3 times.
>
> **Current state of the branch:**
> - `bb11e76` fix(autocomplete): re-fetch when cache filter empties
> - `1f50a33` feat(chat): resolve relative markdown links against parent window URL
> - (swe-swe handoff task untracked at `<swe-swe-repo>/tasks/2026-06-27-parent-url-for-relative-links.md`)
>
> Still open from earlier:
> 1. **Commit the swe-swe task file?** It's untracked in `<swe-swe-repo>`.
> 2. **Set E2E default to `--workers=1`** to avoid the window storm?
>
> Want either of those, or are we done here?

[Quick replies]
- Commit the swe-swe task file
- Set E2E to --workers=1
- Both of the above
- We're done, thanks
- Leave swe-swe file untracked

**USER**

> Leave swe-swe file untracked

<small>took 7.8s</small><br>
**AGENT**

> Got it — leaving the swe-swe task file untracked at `<swe-swe-repo>/tasks/2026-06-27-parent-url-for-relative-links.md` for you/the swe-swe agent.
>
> **Final state:**
> - agent-chat `main`: `1f50a33` (parent_url relative-link resolution) + `bb11e76` (autocomplete flaky-fix). Both committed, binary rebuilt, tests green.
> - swe-swe handoff: file in place, untracked, not committed.
> - E2E left as-is (no `--workers=1` change). Heads-up: running the full `make e2e-test` will still open a window per test — run targeted specs with `--workers=1` when you want it calm.
>
> Nothing else pending on my side. Anything more, or are we done?

[Quick replies]
- We're done, thanks
- Set E2E to --workers=1 after all
- Push the agent-chat commits
- One more thing…

**USER**

> export chat logs and git commit

