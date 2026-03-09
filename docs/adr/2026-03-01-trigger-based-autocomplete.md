# Trigger-Based Autocomplete with Per-Trigger URLs

**Date:** 2026-03-01
**Status:** Accepted

## Context

Users need autocomplete for different kinds of input: file paths, slash
commands, mentions, etc. Each trigger character (e.g. `@`, `/`) may need a
different data source — some built-in, some from external providers.

## Decision

Support configurable trigger characters with per-trigger provider URLs and a
built-in `@filepath` handler:

```
--autocomplete-triggers '/=slash-command=http://host:port,@=filepath'
```

**Resolution order** for each trigger:
1. Per-trigger URL (if configured) — proxy request to external provider
2. Global `--autocomplete-url` (if configured) — fallback external provider
3. Built-in handler (currently only `filepath`) — server-side file scanning
4. Empty result

**Client-side features:**
- 200ms debounce on keystrokes
- Cache: if new query extends cached query, filter locally instead of fetching
- Fuzzy matching with match quality scoring (contiguous substring bonus)
- Loading and "no results" states in dropdown

## Alternatives Considered

- **Hardcoded autocomplete types** — inflexible; can't add new triggers without
  code changes.
- **Client-side only** — can't access server filesystem for file paths.
- **Always fetch from server** — wasteful for progressive refinement of same
  query prefix.

## Consequences

- Extensible: new trigger types added via CLI flag, no code changes needed.
- Built-in filepath autocomplete works out of the box.
- Client-side cache reduces server roundtrips during typing.
- Fuzzy scoring ranks contiguous matches above scattered ones.
