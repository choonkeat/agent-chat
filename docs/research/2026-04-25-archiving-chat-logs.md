# Archiving exported chat logs

**Status:** research note, not a decision yet.

## The question

`export_chat_html` writes a self-contained HTML file (~0.5–2 MB,
depending on inlined images) to `./agent-chats/`.  Over a year of
daily exports that is roughly 365 × 2 MB ≈ **730 MB** of HTML to
manage.  How should we store this so a `git clone` of the main repo
stays fast and small?

## The trap to avoid

> "Just delete the old logs periodically."

This does not shrink the git repo.  In git, a delete is a one-line
diff in *that* commit — the deleted blob remains in history and is
still pulled by every clone.  To actually evict it you need
`git filter-repo` (or BFG), which rewrites history, requires a
force-push, and breaks every existing clone of the repo.

So `rm` buys you nothing storage-wise.  Pruning would cost the same
730 MB plus the deletion churn.

A second misconception worth flagging: HTML compresses well *in
isolation*, but git's pack-file delta compression only helps when
blobs are *similar*.  Different chats share little content, so
delta savings will be small.  Treat each export as roughly its
on-disk size in repo terms.

## Options considered

### 1. External static site, gitignore the chats *(recommended)*

`agent-chats/` is in `.gitignore`; the only thing committed in the
main repo is `agent-chats/INDEX.md` listing titles + URLs.  The
HTML files are deployed to whichever static host you prefer:

```bash
# Netlify (closest match to "convenient rsync-like command")
netlify deploy --dir=agent-chats --prod

# Vercel
vercel deploy ./agent-chats --prod

# AWS S3 (own the bucket, pay egress)
aws s3 sync ./agent-chats s3://your-bucket/ \
    --cache-control "public, max-age=86400"

# Cloudflare R2 (S3-compatible, zero egress)
aws s3 sync ./agent-chats s3://your-bucket/ \
    --endpoint-url https://<account>.r2.cloudflarestorage.com

# rclone (one tool against S3 / R2 / GCS / B2 / Azure / …)
rclone sync ./agent-chats remote:bucket --progress
```

Pros:
- Main repo stays tiny forever (only `INDEX.md` grows).
- Each export is already a self-contained HTML — perfect for
  static hosting; no build step needed.
- Atomic deploys, CDN, HTTPS come free with the managed hosts.

Cons:
- You depend on an external service for chat history.
- Without `INDEX.md` discipline, chats become unfindable.

### 2. Submodule-backed archive repo

Create a sibling repo (e.g. `agent-chats-archive`) and add it as a
submodule at `./agent-chats`.  Enable GitHub Pages on the archive
repo so it is browsable at `you.github.io/agent-chats-archive`.

```bash
gh repo create you/agent-chats-archive --public
git submodule add git@github.com:you/agent-chats-archive agent-chats
# Settings → Pages → branch: main on the archive repo

# day-to-day
cd agent-chats
git add 2026-04-25-…html && git commit -m "chat: …" && git push
```

Pros:
- Keeps git semantics — `git log`, `git blame`, PR review — on the
  chats themselves if that ever matters.
- Main repo only stores a 40-char SHA pointer.

Cons:
- Submodule UX is fiddly: `git clone` doesn't fetch submodules by
  default, `git pull` doesn't update them.  Contributors hit "why
  is `agent-chats/` empty after clone?" routinely.
- Every chat-update either creates a separate "bump submodule"
  commit in main (noise) or main drifts behind (stale pointer).
- The archive repo still grows ~730 MB/yr — bloat is *isolated*,
  not *eliminated*.
- Pages URL is on the archive repo's domain unless you add a
  custom domain / CNAME.

### 3. Git LFS

Track `agent-chats/*.html` via LFS so the bytes live in LFS storage
and the main repo carries 130-byte pointer files.

Pros:
- Single repo, no submodule setup.
- Fast clones for people without LFS need.

Cons:
- LFS bandwidth and storage are metered on GitHub (paid above
  modest free quotas).
- A clone without `git lfs install` fetches *pointer files*, not
  HTML — confusing failure mode.
- Doesn't gain you a public browsable URL on its own; you would
  still need a separate static-hosting step to view the chats in a
  browser.

## Approaches that don't help

- **Monthly zip archive committed in place** — saves nothing.  Git
  pack files already gzip blobs; zipping HTML before commit just
  means you can no longer grep or diff individual chats.
- **Periodic `git filter-repo` rewrites** — rewrites history,
  forces every developer to re-clone, sacrifices the audit trail
  for a one-time saving you could have avoided by not committing
  in the first place.

## Tentative recommendation

**Option 1 with Netlify or R2.**  The export tool already produces a
self-contained HTML, which is exactly the artifact a static host
wants.  An `INDEX.md` committed in main covers discoverability
without committing megabytes.

If the deciding factor is "I want chats accessible from `git log`",
fall back to **option 2** (submodule), accepting the UX papercuts.

LFS only makes sense if a future use case demands the chats live
inside the same repo as the code *and* you don't want a static
site — a narrow combination.

## Open questions

- Do we want chats to be public, or behind auth?  Netlify/Vercel
  have password-protected sites on paid tiers; S3 + signed URLs
  works for private; R2 needs Cloudflare Access for auth.
- Should `INDEX.md` be auto-generated by the export tool itself, or
  curated by hand?  Auto-generation removes a per-export step but
  loses room for human-written summaries.
- If we go with a static host, who owns the deploy credentials —
  is this a per-developer convenience (each dev deploys their own
  chats) or a shared archive (CI deploys on push)?
