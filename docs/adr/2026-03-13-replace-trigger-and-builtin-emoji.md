# Replace Trigger and Built-In Emoji Autocomplete

**Date:** 2026-03-13
**Status:** Accepted

## Context

The autocomplete system always prepends the trigger character when inserting a
selection (e.g. typing `@mai` and selecting `main.go` produces `@main.go`).
This works for filepath and slash-command triggers but breaks for emoji
shortcodes, where `:heart` should produce `❤️`, not `:❤️`.

The question was whether the replacement behavior should be controlled by the
autocomplete provider (via the response payload) or by the environment operator
(via CLI config).

## Decision

### Provider-controlled `replace_trigger`

Add a `replace_trigger` boolean to the autocomplete response payload. When
`true`, the client omits the trigger character during insertion — only the
selected value is inserted, fully replacing the trigger and query text.

```json
{
  "results": [{"v": "❤️", "h": "heart"}],
  "replace_trigger": true,
  "has_more": false
}
```

The provider controls this because it understands the semantics of its results.
The environment operator (CLI config) is a routing layer and shouldn't need to
know about insertion behavior.

Backward compatible: existing providers don't send `replace_trigger`, so
behavior defaults to the current prepend-trigger behavior.

### Built-in emoji handler

Register `:` as a default trigger backed by `builtin:emoji`, alongside the
existing `@` → `builtin:filepath` default. The emoji handler:

- Contains 1,560 emoji entries with multiple keywords per emoji (e.g. `heart`,
  `love`, `red_heart` all match `❤️`)
- Fuzzy matches with scoring: exact > prefix > substring > fuzzy
- Returns emoji as the value (`v`) and shortcode as the hint (`h`)
- Always sets `replace_trigger: true`
- Empty query returns a curated set of 25 popular emojis

The data is compiled into the binary — no external files or providers needed.

## Alternatives Considered

- **CLI config flag** (`--autocomplete-triggers ':=http://url,replace'`) — couples
  the routing layer to provider-specific insertion behavior.
- **Closing delimiter detection** (`:heart:` as a complete shortcode) — more
  complex, requires the client to understand shortcode syntax.
- **Separate emoji system** outside autocomplete — duplicates trigger detection
  and dropdown UI logic.

## Consequences

- External providers can now control insertion behavior via `replace_trigger`.
- Emoji autocomplete works out of the box with no configuration.
- Users can override the `:` trigger with a custom provider URL if they want a
  different emoji set.
- The binary size increases slightly (~100KB) due to the embedded emoji data.
