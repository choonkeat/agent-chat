# Proposal: link a code span whose entire content is a real workspace path

Filed 2026-08-01. **Shipped 2026-08-01.** Follow-up to
`tasks/2026-08-01-autolink-workspace-paths.md` (phases 1-3 shipped, `62f31e8`).

Implemented as proposed, option (a). Server needed nothing — confirmed by two
new cases in `workspacepaths_test.go`: extraction is markdown-blind, so a
backticked path was already in `file_paths`. The client change is one line in
the code-span stash plus two small helpers factored out of `autolinkFilePaths`
(`filePathAnchor`, `filePathEntryFor`). `e2e/autolink-workspace-paths.spec.cjs`
is now 12 tests: the old "never linked" assertion split into four — span links,
span with extra words stays literal, span holding a non-existent path stays
literal, and fenced blocks stay literal.

Today `` `docs/research` `` in backticks renders as plain code and does not
link, while bare `docs/research` does. That asymmetry was deliberate — code
spans are literal per CommonMark, and stashing them was the cheap way to keep
the autolink out of code examples — but it fires on the most natural way to
write a path in chat, and it bit within minutes of the feature shipping.

**Rule:** if a single-backtick span's whole content is one path the server
already listed in `file_paths`, render it as a Files-pane link instead of a
`<code>`. Nothing else about code spans changes.

## Scope

- **Inline code spans only** (single backticks). Fenced ``` blocks stay
  literal — they hold examples and command lines, and they remain the escape
  hatch for showing a path without linking it.
- **Whole-content match only.** `` `see docs/research` `` stays plain code;
  only `` `docs/research` `` converts. Partial rewriting inside a span would
  re-open exactly the hole the stash was built to close.
- The path must already be in the bubble's `file_paths` — i.e. it exists on
  disk and resolves under the workspace root. No new trust surface.

## Where the change goes

1. **Server: likely nothing.** `extractWorkspacePaths` (`workspacepaths.go:62`)
   tokenizes raw message text with no markdown awareness, so a backticked path
   should already be in `file_paths`. Confirm with a unit case before assuming
   it — that is the first thing to check.
2. **Client: the code-span stash**, `client-dist/app.js:391`. The
   `` /`([^`\n]+)`/g `` replace currently always pushes `<code>content</code>`.
   Add: if `content` (trimmed) equals some `entry.raw` in `filePaths`, push the
   anchor instead. `renderMarkdown` already receives `filePaths`.
3. Reuse the anchor shape from `autolinkFilePaths`
   (`client-dist/app.js:358-367`) — same `data-files-path` + absolute href, so
   click routing and cmd-click keep working with no new handler.

The stash placeholder is restored *after* `autolinkFilePaths` runs, so an
anchor emitted at step 2 is invisible to the bare-path rule and cannot be
double-wrapped. That ordering is already load-bearing; do not move it.

## Open decision: how it should look

- **(a) Plain link** — `@docs/research`, identical to every other autolinked
  path. Consistent; loses the monospace.
- **(b) Monospace link** — `<a …><code>docs/research</code></a>`. Keeps the
  author's formatting intent, but now two visually different things are the
  same kind of link.

Recommend (a): one appearance for one behaviour. Decide before writing tests.

## Test to invert

`e2e/autolink-workspace-paths.spec.cjs` currently asserts that a bubble
containing `` `client-dist/app.js` `` renders **no** anchors. That assertion
becomes its opposite. Replace it with a fenced-code-block case, so the
"a path can still be shown literally" guarantee stays covered.

Also add: a code span holding a path that does *not* exist stays a `<code>`;
a code span with a path plus surrounding words stays a `<code>`.

## Cost

About 20 minutes: one client rule, one unit case, two E2E assertions.
`make test` to verify.
