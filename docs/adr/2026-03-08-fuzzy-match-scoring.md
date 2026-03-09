# Fuzzy Matching with Match Quality Scoring

**Date:** 2026-03-08
**Status:** Accepted

## Context

Autocomplete results need ranking. A naive fuzzy match (does each query
character appear in order?) returns too many low-quality matches. "task" should
rank `tasks/readme.md` above `t_a_s_k.txt` even though both match.

## Decision

Use greedy left-to-right character matching with a span-based score. The score
equals the distance from first matched character to last (lower is better).
Contiguous substring matches get a 50% bonus (score halved).

```
Query: "task"
  "tasks/readme.md"  → span 4, contiguous → score 2
  "t_a_s_k.txt"      → span 7, scattered  → score 7
```

Scoring runs both client-side (for cache filtering) and server-side (for
filepath results). Results are sorted by score ascending.

## Alternatives Considered

- **Levenshtein distance** — expensive for long paths; penalizes length
  differences rather than match quality.
- **Exact prefix match** — too strict; misses useful partial matches.
- **No scoring (filter only)** — results appear in arbitrary order.

## Consequences

- Intuitive ranking: exact and contiguous matches appear first.
- Simple algorithm: O(n) per candidate, no complex data structures.
- Consistent behavior between client cache and server results.
- Easy to tune: the contiguous bonus multiplier is a single constant.
