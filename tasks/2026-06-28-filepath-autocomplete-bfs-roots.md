# Filepath autocomplete: BFS, absolute roots, and dotfiles

## Goal

Improve the built-in `@` filepath autocompleter (`builtin:filepath`) so it:

1. **Includes hidden files/dirs** (dotfiles) — no special-casing, they complete
   like anything else.
2. **Supports absolute paths**, but confined to an **allowlist of roots** rather
   than the whole filesystem.
3. Stays **sane from a shallow query** (e.g. a bare `@/`) by walking
   **breadth-first** instead of depth-first, so the candidate cap fills with the
   *shallowest* matches rather than diving into the first alphabetical subtree.

**Client stays unchanged.** The existing client cache (`app.js`, `acCache`)
already short-circuits to local filtering *only* when `!hasMore` (the returned
set was below the cap and is therefore the complete match set). All we must do
server-side is keep `has_more` honest. No `app.js` edits, no new client logic.

## Background (current behavior — read before changing)

In `main.go`:

- `handleAutocomplete` → `builtin:filepath` branch: if the query starts with `/`
  it sets `root = "/"` (walks the **entire** filesystem), else `root = filepathRoot`.
- `builtinFilepathComplete(root, query)` uses `filepath.WalkDir` (**lexical DFS**),
  prunes hidden **dirs** unless the query contains the literal substring `"/."`,
  collects up to 500 fuzzy candidates, returns the top 50 by `fuzzyScorePath`,
  and sets `hasMore = walkCapped || len(candidates) > limit`.

Problems this task fixes:

- **Hidden dirs are unreachable** in the common case: the `"/."` guard only fires
  on a slash-then-dot, so `@.github/…` (query `.github/…`, no `/.`) prunes
  `.github` and returns nothing. Contents of `.claude/`, `.swe-swe/`, `.github/`
  are effectively invisible.
- **Absolute queries walk from `/`** — slow, reads `/proc` `/sys` `/dev`, and the
  DFS 500-cap fills alphabetically (`/bin…`) before reaching the intended path.
- **DFS ordering** means a shallow query buries shallow matches under the first
  subtree it dives into.

## Design decisions (confirmed with user 2026-06-28)

- **No hidden guard.** Remove the `skipHidden` / `"/."` logic entirely. Dotfiles
  and dotdirs are traversed and completed like any other entry.
- **BFS, not DFS.** Replace `filepath.WalkDir` with a queue over `os.ReadDir`:
  visit level 1 (all children of the start dir), then level 2, etc. Collect
  fuzzy matches as encountered; stop at the candidate cap (~500) or when the
  queue drains. Final ranking is still by `fuzzyScorePath` (BFS only governs
  *which* candidates survive the cap, not display order).
- **`has_more` honesty drives everything.** `has_more = capped || collected > 50`.
  When the BFS drains the tree without hitting the cap, `has_more=false` and the
  client narrows locally on subsequent keystrokes; when capped, `has_more=true`
  and the client re-fetches. This is the *only* coupling to the client and it
  already exists — do not change the client.
- **Roots allowlist** via a new `--filepath-roots` flag (comma-separated).
  **Default = the current working directory _plus_ `/repos`, `/workspace`,
  `/worktrees`.** (cwd is included so a freshly-launched instance can complete
  its own tree even if it is outside the three swe-swe roots; de-dupe if cwd is
  already under one of them.) Absolute queries are confined to entries under one
  of these roots. An absolute query under no allowed root returns no results.
- **`@/` lists the roots.** A bare `/` (or a partial that is a prefix of one or
  more roots, e.g. `/re`) seeds the BFS frontier with the matching allowed roots
  rather than `os.ReadDir("/")`. This enforces the allowlist *and* avoids
  `/proc` `/sys` `/dev` with no denylist needed.
- **Anchor for performance.** For an absolute query whose literal directory
  prefix exists (e.g. `/repos/agent-chat/wo` → `/repos/agent-chat`), start the
  BFS at that deepest existing prefix so sibling roots/repos aren't read. A typo
  in a middle segment simply yields a shallower anchor (graceful degradation).
  Relative queries anchor under `filepathRoot` (cwd) as today.
- **No fixed depth cap.** The candidate cap + BFS ordering keep things bounded;
  do NOT add a `segments+1` depth limit (it would break deep scattered fuzzy
  matches like `task → cmd/templates/host/Dockerfile`). BFS-until-cap preserves
  that recall when slots are available.
- **Safety backstop.** A visited-entry cap (~20k `os.ReadDir` entries) so a
  pathological tree can't hang a request; trips `has_more=true`.
- **Display semantics preserved.** Relative queries yield paths as today
  (relative to `filepathRoot`); absolute queries yield absolute paths. The
  selected value the client inserts must match what the user typed (relative vs
  absolute).

## TDD steps

Work test-first. After **every** step run `make test` (per CLAUDE.md — never
`go test`/`go vet` directly). Red → green → refactor.

### Step 1 — Roots config + default includes cwd
- **Test (red):** add `TestFilepathRootsDefault` asserting the parsed default
  roots contain the cwd and `/repos`, `/workspace`, `/worktrees`, de-duped (cwd
  not double-listed if already under a root). Add `TestFilepathRootsFlagParse`
  for a custom comma-separated `--filepath-roots`.
- **Impl (green):** add a `filepathRoots []string` var + `--filepath-roots`
  flag, default `cwd,/repos,/workspace,/worktrees` (compute cwd at startup).
  Add a helper `parseFilepathRoots(string, cwd string) []string`.

### Step 2 — BFS replaces DFS (relative, dotfiles included)
- **Test (red):**
  - `TestBuiltinFilepathBFSShallowFirst`: a temp tree where a shallow file and a
    deep file both fuzzy-match a query; with the cap lowered (or a large tree),
    assert the shallow match is collected (BFS), not dropped in favor of the deep
    one. (Make the cap injectable or use a tree big enough to force ordering.)
  - `TestBuiltinFilepathIncludesDotfiles`: temp tree with `.github/workflows/ci.yml`
    and `.claude/settings.json`; query `github/ci` (and `.claude/set`) returns the
    dotted paths. Replaces the old "should skip hidden dirs" expectation —
    **update `TestBuiltinFilepathComplete`** to assert `.git/config` *is* reachable
    now (or remove the skip assertion), reflecting the new no-guard contract.
- **Impl (green):** rewrite `builtinFilepathComplete` to BFS over `os.ReadDir`
  from the anchor; drop the `skipHidden` logic; keep `fuzzyScorePath` ranking,
  the 500 candidate cap, top-50, and `has_more = capped || collected > 50`; add
  the ~20k visited-entry backstop.
- **Keep green:** `TestFuzzyScorePath`, `TestFuzzyMatchPath`,
  `TestBuiltinFilepathCompleteScoring` (deep `task → …/Dockerfile` must still
  appear — BFS-until-cap, no depth limit).

### Step 3 — Absolute queries: allowlist + anchor + `@/` lists roots
- **Test (red):**
  - `TestBuiltinFilepathAbsoluteAllowed`: temp roots; an absolute query under an
    allowed root returns matches; an absolute query under a *disallowed* root
    returns empty.
  - `TestBuiltinFilepathAbsoluteAnchor`: query `<root>/<subdir>/<partial>` only
    reads under `<root>/<subdir>` (assert sibling dirs are not returned).
  - `TestBuiltinFilepathSlashListsRoots`: query `/` returns the allowed roots;
    `/<prefix>` returns only roots matching the prefix.
- **Impl (green):** in the `builtin:filepath` branch (and/or
  `builtinFilepathComplete`), compute the BFS frontier:
  - absolute + under a root → anchor = deepest existing dir prefix (within the
    root);
  - absolute that is a prefix of root(s) (incl. bare `/`) → frontier = matching
    allowed roots;
  - absolute under no root → empty;
  - relative → anchor under `filepathRoot` as today.

### Step 4 — Full sweep
- `make test` green (unit + e2e). For e2e, warm the lazy CDP endpoint first via
  an MCP `browser_navigate` (see CLAUDE.md / `e2e/global-setup.cjs`) or the
  specs fail at connect with `ECONNREFUSED`.
- Confirm **no `client-dist/` changes** — this is a server-only change; the
  binary need not be restarted for tests, and restarting the live session server
  would kill the running chat (do not do it).

## Acceptance

- Dotfiles/dotdirs complete (`@.github/…`, `@.claude/…`) — no `/.` workaround.
- Absolute queries work but only under `--filepath-roots`
  (default cwd + `/repos`, `/workspace`, `/worktrees`); `@/` lists those roots;
  nothing outside them (no `/proc` `/sys` `/dev`).
- A bare/shallow query returns shallow matches first (BFS), capped at 50 with a
  correct `has_more`.
- Deep scattered fuzzy matches still surface when slots are free (no depth cap).
- Client unchanged; cache behavior identical (local narrow only when `!has_more`).
- `make test` green.

## Out of scope

- Any `client-dist/app.js` / cache changes.
- Per-keystroke server-side memoization (the client cache already covers it).
- A mode/depth picker UI.
