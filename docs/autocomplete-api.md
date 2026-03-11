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
| `--autocomplete-url` | Default URL for types without a per-trigger URL | `http://localhost:9000/completions` |
| `--autocomplete-triggers` | Comma-separated `CHAR=TYPE[=URL]` mappings | `/=slash-command=http://host/api,@=filepath` |

### Trigger mappings

Each mapping pairs a trigger character with a type string and an optional
provider URL:

```
--autocomplete-triggers '/=slash-command=http://localhost:9000/completions,@=filepath'
```

This produces:

| Character | Type | Provider |
|-----------|------|----------|
| `/` | `slash-command` | `http://localhost:9000/completions` |
| `@` | `filepath` | built-in (no URL) |

### Resolution order

When the server receives an autocomplete request for a given type:

1. **Per-trigger URL** — if the trigger mapping included `=URL`, use that URL.
2. **Global URL** — if `--autocomplete-url` is set, use it as fallback.
3. **Built-in handler** — if the type is `filepath`, use the built-in handler.
4. **Empty result** — return `[]`.

### Built-in filepath handler

The `filepath` type has a built-in handler that walks the working directory,
skips hidden directories (e.g. `.git`), collects up to 500 file paths that
fuzzy-match the query (case-insensitive, greedy left-to-right character
matching), scores them by match quality, and returns the top 50.

#### Scoring

Results are sorted by a quality score (lower is better). The score is the
character span of the fuzzy match — i.e. the distance from the first matched
character to the last. A contiguous substring match (e.g. query `task` in
`tasks/readme.md`) gets a bonus: its score is halved. This ensures that paths
containing the query as a substring rank above paths with scattered character
matches.

External providers implementing the same contract should apply similar
scoring to produce consistent results across built-in and custom handlers.

This handler is used when no external URL is configured for the `filepath`
type. To override it with an external provider:

```
--autocomplete-triggers '@=filepath=http://my-server/files'
```

## Proxy endpoint

### `POST /autocomplete`

The client never contacts providers directly. All requests go through this
proxy endpoint on the agent-chat server.

#### Request

```http
POST /autocomplete HTTP/1.1
Content-Type: application/json

{
  "type": "slash-command",
  "query": "bu"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Completion type from the trigger mapping |
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

The external provider receives the exact same JSON body that was sent to
`POST /autocomplete`:

```http
POST <provider-url> HTTP/1.1
Content-Type: application/json

{
  "type": "slash-command",
  "query": "bu"
}
```

The provider must return:

- **Status 200** with a JSON body that is an array of strings (e.g. `["a","b"]`).
  The proxy wraps the array into the structured `{results, info}` format
  before forwarding to the client.
- Non-200 status codes cause agent-chat to return **502** to the client.
- Malformed JSON responses are treated as an empty array.

Response size limit enforced by the proxy: **64 KB**.

### Example provider implementation (pseudo-code)

```python
@app.post("/completions")
def completions(request):
    body = request.json()
    type = body["type"]     # e.g. "slash-command"
    query = body["query"]   # e.g. "bu"

    candidates = lookup(type, query)
    return json_response(candidates)  # ["build", "bump-version"]
```

## Client behavior summary

These details are useful if you are building a custom client rather than using
the built-in chat UI.

- **Trigger detection**: A trigger character is recognized when it appears at
  position 0 in the input or immediately after whitespace.
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
