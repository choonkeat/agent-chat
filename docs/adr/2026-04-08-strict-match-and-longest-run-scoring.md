# Strict In-One-Field Matching and Longest-Run Scoring

**Date:** 2026-04-08
**Status:** Accepted (supersedes parts of [2026-03-08-fuzzy-match-scoring](2026-03-08-fuzzy-match-scoring.md))

## Context

Two related problems surfaced after `2026-03-08-fuzzy-match-scoring` shipped:

1. **Cross-field matches were confusing.** The client matcher (`acFuzzyMatch`)
   ran over the concatenation `value + ' ' + hint`, so a query could be
   satisfied by characters spanning both fields. Users would see a row in
   the dropdown and be unable to spot why it matched, because no single
   field contained the query as a subsequence. Highlighted output across
   two spans made the cause harder to read, not easier.

2. **The scoring formula favored "early position" over "tight runs".** The
   existing emoji handler used `score = 3 + span + first` and the filepath
   handler used `score = (last-first+1)[/2 if contiguous] + first`. Both
   add a positional term, so a sparse-but-early match could outrank a
   tight-but-late one. Real fuzzy finders (fzf, VSCode, Helm) do the
   opposite — they reward tight clusters of matched characters because
   the user typed the chars together and expects them to land together.
   Worse, the two builtin providers used different formulas, so they were
   inconsistent with each other.

3. **The published autocomplete contract claimed a tiered pipeline that
   didn't exist.** `docs/autocomplete-api.md` said "Providers and the
   client apply the same three-stage pipeline" while in fact only one
   provider scored anything, the client did no sort, and pass-through
   providers (e.g. swe-swe slash commands) were ranked by the upstream
   in any order they liked.

## Decision

Adopt a unified ranking pipeline across both builtin providers and the
client matcher.

### Match (step 1) — strict in-one-field

A candidate qualifies if the query is a subsequence of `value` **OR** a
subsequence of `hint` (case-insensitive, greedy left-to-right). The match
must be satisfied by **one field alone**; cross-field subsequence matches
are rejected. The client uses the same rule, so its cache-filter pass
agrees with the provider's filter step.

### Sort (step 3) — tier + longest-run + span + length

All value-match tiers rank strictly above all hint-match tiers, so a hint
hit never outranks a real value hit. Within a tier, ranking is:

1. `longestRun` — the longest block of query characters that landed on
   consecutive positions in the matched field, **descending**. Tight runs
   beat sparse runs.
2. `span` — `last - first` of matched positions, **ascending**. Tighter
   spans break further ties.
3. Field length, **ascending**. Shorter wins as the final tiebreaker.

The composite is encoded into a single integer in `fuzzyScorePath` so the
existing call site can sort with a plain `int` comparator.

### Highlighter

`acHighlightCombined` mirrors the strict match rule: highlight chars in
the value if value alone satisfies the query, otherwise in the hint. Never
highlight across both spans.

## Alternatives Considered

- **Keep early-position bonus.** Defensible — early matches feel "right"
  for prefix-shaped queries. Rejected because it disagreed with the
  fuzzy-finder convention everyone else has converged on, and because we
  had two different formulas that couldn't be reconciled cleanly.
- **Allow cross-field matches with smarter highlighting.** Rejected:
  any heuristic for "where to put the highlight" produces output the
  user can't predict.
- **Remove hint matching entirely.** Rejected: hint matching is the
  point of having hints — users want to find a slash command by typing
  words from its description.

## Consequences

- The dropdown shows fewer "phantom" matches; every visible row has a
  visible reason it matched.
- For most realistic queries (`heart`, `dom`, `task`) the ranking is
  unchanged. The only cases that shift are sparse multi-char queries
  where early-position and longest-run disagree.
- The published `docs/autocomplete-api.md` contract is now actually
  implemented, not aspirational. Pass-through providers (e.g. swe-swe
  slash commands) are still free to return arbitrary order, but they
  lose the value-above-hint guarantee unless they implement the tiers
  themselves.
- The unit test `TestFuzzyScorePath` lost its early-position assertion
  and gained a longest-run one. The e2e test that was named
  `early-position bonus ranks earlier matches higher` was renamed to
  reflect what it actually exercises now (primary-vs-secondary keyword
  tiebreaker).
- The earlier ADR `2026-03-08-fuzzy-match-scoring` is partially
  superseded — the within-fuzzy scoring formula has changed. The
  high-level decision (greedy left-to-right matching with quality-based
  ranking) still stands.
