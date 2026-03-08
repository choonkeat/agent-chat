# Autocomplete Providers

## Overview
Add trigger-based autocomplete to the chat input. When the user types a trigger character (e.g. `/`, `@`) the client calls a configurable external HTTP endpoint to fetch completions, and displays them in a dropdown.

## Design

### CLI Flags
```
--autocomplete-url http://localhost:9000/completions
--autocomplete-triggers '/=slash-command,@=filepath'
```

### HTTP Proxy Endpoint
- `POST /autocomplete` on our Go server
- Proxies request body as-is to `--autocomplete-url`
- Request: `{ "type": "slash-command", "query": "bu" }`
- Response: `["busy", "been up"]`
- If `--autocomplete-url` not set → return empty array

### Frontend Config Injection
- Inject trigger map via `<script>var AUTOCOMPLETE_TRIGGERS={"/":"slash-command","@":"filepath"};</script>`
- Browser always calls our `/autocomplete` proxy (no external URL exposed to client)

### Client Behavior
1. On keystroke, scan backwards from cursor for a trigger character
2. Trigger only activates after whitespace or at position 0
3. Debounce, then `POST /autocomplete` with `{ "type": "<mapped-type>", "query": "<text-after-trigger>" }`
4. Show dropdown with results, fuzzy-highlight query chars (prioritize closest-together matches)
5. **Select**: replace `{trigger}{query}` with `{trigger}{chosen}` in textarea
6. **Esc**: dismiss dropdown, leave text untouched
7. Arrow keys navigate dropdown, Enter/click selects

## Implementation Steps

### Step 1: Go backend — CLI flags + proxy endpoint
- [ ] Add `--autocomplete-url` and `--autocomplete-triggers` flags in `main.go`
- [ ] Parse triggers flag into map (e.g. `"/=slash-command,@=filepath"` → `map[string]string`)
- [ ] Add `POST /autocomplete` handler that proxies to the configured URL
- [ ] Inject `AUTOCOMPLETE_TRIGGERS` into the config script in index.html serving

### Step 2: Frontend — trigger detection + API calls
- [ ] Add keystroke listener that detects trigger characters
- [ ] Only trigger after whitespace or at input start
- [ ] Debounce (e.g. 200ms) before calling `POST /autocomplete`
- [ ] Track active trigger position for replacement

### Step 3: Frontend — dropdown UI
- [ ] Render dropdown below/above the cursor position
- [ ] Fuzzy-highlight matched characters in each option
- [ ] Keyboard navigation: arrow up/down, Enter to select, Esc to dismiss
- [ ] Click to select
- [ ] Dismiss on blur or when cursor moves before trigger position

### Step 4: Frontend — selection & replacement
- [ ] On select: replace from trigger position through current query with trigger+chosen
- [ ] On Esc: dismiss dropdown, leave text as-is
- [ ] After replacement, re-focus textarea and place cursor after inserted text

### Step 5: Testing
- [ ] Go test: proxy endpoint with mock upstream
- [ ] Manual test: trigger detection, dropdown, selection, dismissal
