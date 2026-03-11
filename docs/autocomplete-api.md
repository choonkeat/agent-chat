# Autocomplete API

Agent-chat provides a trigger-based autocomplete system. When a user types a
trigger character (e.g. `/` or `@`), the client requests completions from the
server's proxy endpoint, which routes to an external provider or a built-in
handler.

## Defaults

With no flags, agent-chat registers `@` as a filepath trigger backed by a
built-in handler. Typing `@mai` in the chat input suggests file paths matching
"mai" (e.g. `main.go`). No external provider is required.

## Configuration

| Flag | Description | Example |
|------|-------------|---------|
| `--autocomplete-triggers` | Comma-separated `CHAR=URL` mappings | `/=http://host/api` |
| `--autocomplete-url` | _(legacy)_ Fallback URL for triggers without an explicit URL | `http://localhost:9000/completions` |

### Trigger mappings

Each mapping pairs a trigger character with a provider URL:

```
--autocomplete-triggers '/=http://localhost:9000/completions'
```

This produces:

| Character | Provider |
|-----------|----------|
| `/` | `http://localhost:9000/completions` |
| `@` | built-in filepath handler (default) |

To override the default `@` trigger with a custom provider:

```
--autocomplete-triggers '@=http://my-server/files,/=http://host/api'
```

### Legacy format

The old `CHAR=TYPE=URL` and `CHAR=TYPE` formats are still accepted for backward
compatibility. The type name is ignored; only the URL matters:

```
# Legacy (still works):
--autocomplete-triggers '/=slash-command=http://host/api'
--autocomplete-triggers '/=slash-command' --autocomplete-url http://host/api

# New (recommended):
--autocomplete-triggers '/=http://host/api'
```

### Resolution order

Internally, agent-chat maintains a flat map of trigger character → URL.
When a request arrives for a trigger character:

1. **Per-trigger URL** — if the trigger has an explicit URL, proxy to it.
2. **Built-in handler** — if the URL is `builtin:filepath`, use the built-in handler.
3. **Empty result** — return `[]`.

### Built-in filepath handler

The `@` trigger defaults to `builtin:filepath`, a built-in handler that walks
the working directory, skips hidden directories (e.g. `.git`), collects up to
500 file paths that fuzzy-match the query (case-insensitive, greedy
left-to-right character matching), scores them by match quality, and returns
the top 50.

#### Scoring

Results are sorted by a quality score (lower is better). The score is the
character span of the fuzzy match — i.e. the distance from the first matched
character to the last. A contiguous substring match (e.g. query `task` in
`tasks/readme.md`) gets a bonus: its score is halved. This ensures that paths
containing the query as a substring rank above paths with scattered character
matches.

External providers implementing the same contract should apply similar
scoring to produce consistent results across built-in and custom handlers.

## Proxy endpoint

### `POST /autocomplete`

The client never contacts providers directly. All requests go through this
proxy endpoint on the agent-chat server.

#### Request

```http
POST /autocomplete HTTP/1.1
Content-Type: application/json

{
  "trigger": "/",
  "query": "bu"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `trigger` | string | The trigger character (e.g. `"/"`, `"@"`) |
| `query` | string | Text the user typed after the trigger character |

Body size limit: **4 096 bytes**.

#### Response

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "results": ["build", "bump-version", "busy"],
  "info": "",
  "has_more": false
}
```

| Field | Type | Description |
|-------|------|-------------|
| `results` | string[] | Completion candidates |
| `info` | string | Optional message shown when results are empty (e.g. debug context) |
| `has_more` | bool | `true` when the server truncated results and more matches exist. Clients should not cache these results — re-query with a more specific query for better matches. Omitted when `false`. |

When the built-in filepath handler returns no results, `info` includes
the working directory and query to help diagnose the issue:

```json
{"results":[], "info":"No files matching \"xyz\" in /path/to/cwd"}
```

#### Status codes

| Code | Meaning |
|------|---------|
| 200 | Success (body is a JSON object with `results` array) |
| 405 | Method not allowed (only POST is accepted) |
| 400 | Request body could not be read or parsed |
| 502 | Upstream provider returned an error or is unreachable |

## Provider contract

The server proxies a JSON body containing only the query to the provider URL:

```http
POST <provider-url> HTTP/1.1
Content-Type: application/json

{
  "query": "bu"
}
```

The provider must return **Status 200** with one of these JSON formats (in
order of preference):

1. **Structured object** with `results` array and optional `has_more`:
   ```json
   {"results": ["build", "bump-version"], "has_more": true}
   ```
   The `has_more` flag tells the client not to cache results, ensuring each
   keystroke re-queries for better matches. Omit or set to `false` when the
   result set is complete.

2. **Array of items** with value/hint pairs: `[{"v":"build","h":"Run build"}]`

3. **Plain string array**: `["build", "bump-version"]`

For formats 2 and 3, `has_more` defaults to `false`.

- Non-200 status codes cause agent-chat to return **502** to the client.
- Malformed JSON responses are treated as an empty array.

Response size limit enforced by the proxy: **64 KB**.

### Example provider implementation (pseudo-code)

```python
@app.post("/completions")
def completions(request):
    body = request.json()
    query = body["query"]   # e.g. "bu"

    candidates, has_more = lookup(query, limit=50)
    return json_response({
        "results": candidates,  # ["build", "bump-version"]
        "has_more": has_more,   # true if results were truncated
    })
```

## Client behavior summary

These details are useful if you are building a custom client rather than using
the built-in chat UI.

- **Trigger detection**: A trigger character is recognized when it appears at
  position 0 in the input or immediately after whitespace.
- **Trigger config**: The server injects `AUTOCOMPLETE_TRIGGERS` into the page
  as a JSON array of trigger characters (e.g. `["@", "/"]`).
- **Debounce**: The client waits 200 ms after the last keystroke before
  sending a request.
- **Client-side cache**: If the user extends a previous query (e.g. `b` → `bu`),
  the client filters the cached results locally instead of making a new request.
  The cache is bypassed when the server indicated `has_more: true`, meaning
  results were truncated — in that case each keystroke re-queries the server
  (with debounce) for better matches.
- **Fuzzy matching**: Candidates are matched greedily left-to-right against the
  query characters (case-insensitive).
- **Selection**: When the user picks a candidate, the trigger character plus the
  selected value replace the text from the trigger position to the cursor.
