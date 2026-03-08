# Autocomplete API

Agent-chat provides a trigger-based autocomplete system. When a user types a
trigger character (e.g. `/` or `@`), the client requests completions from an
external provider via a built-in proxy endpoint.

## Configuration

| Flag | Description | Example |
|------|-------------|---------|
| `--autocomplete-url` | URL of the external completion provider | `http://localhost:9000/completions` |
| `--autocomplete-triggers` | Comma-separated `CHAR=TYPE` mappings | `/=slash-command,@=filepath` |

If `--autocomplete-url` is not set, the proxy returns an empty array and the
feature is effectively disabled.

### Trigger mappings

Each mapping pairs a single trigger character with a type string:

```
--autocomplete-triggers '/=slash-command,@=filepath'
```

This produces the trigger table:

| Character | Type |
|-----------|------|
| `/` | `slash-command` |
| `@` | `filepath` |

The type string is sent to the provider so it knows what kind of completions
to return.

## Proxy endpoint

### `POST /autocomplete`

The client never contacts the provider directly. All requests go through this
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

["build", "bump-version", "busy"]
```

The response is a JSON array of strings — the completion candidates.

#### Status codes

| Code | Meaning |
|------|---------|
| 200 | Success (body is a JSON string array) |
| 405 | Method not allowed (only POST is accepted) |
| 400 | Request body could not be read |
| 502 | Upstream provider returned an error or is unreachable |

When `--autocomplete-url` is not configured, the endpoint returns `200` with
an empty array `[]`.

## Provider contract

The external provider receives the exact same JSON body that was sent to
`POST /autocomplete`:

```http
POST <autocomplete-url> HTTP/1.1
Content-Type: application/json

{
  "type": "slash-command",
  "query": "bu"
}
```

The provider must return:

- **Status 200** with a JSON body that is an array of strings.
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
- **Fuzzy matching**: Candidates are matched greedily left-to-right against the
  query characters (case-insensitive).
- **Selection**: When the user picks a candidate, the trigger character plus the
  selected value replace the text from the trigger position to the cursor.
