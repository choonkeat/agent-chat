# mcp-apps-sample (test only, never shipped)

A tiny MCP server that implements the **MCP Apps** extension
(SEP-1865, spec `2026-01-26`) so agent-chat's MCP Apps support can be tested
without depending on anyone else's server.

Not released: it lives under `testdata/`, which `go build ./...`, `go vet ./...`
and `make test` skip, and the npm package only ships `bin/`.

## What it serves

| Thing | Value |
|---|---|
| Tool `show_calendar` | Returns a text table + `structuredContent` grid. Has `_meta.ui.resourceUri` when UI is on. |
| Tool `book_slot {person, day}` | Marks a free slot `pending`; errors if taken. Visibility `["model","app"]`. State is in memory, per process. |
| Resource `ui://mcp-apps-sample/calendar.html` | MIME `text/html;profile=mcp-app`, `_meta.ui.prefersBorder: true`. Plain HTML/JS, no libraries. |

The page speaks the View side of the spec over `postMessage`: `ui/initialize`
→ `ui/notifications/initialized`, renders on `ui/notifications/tool-result`,
reports `ui/notifications/size-changed`, answers `ui/resource-teardown`.
Clicking a green **free** cell sends `tools/call book_slot`; if the host
answers with an error (it can't relay tool calls), the page falls back to
`ui/message` ("Please book Bob on Tue 24 (book_slot).") so the agent does it.

## `--ui` modes

The spec says servers SHOULD hide UI unless the client's `initialize` carries
`capabilities.extensions["io.modelcontextprotocol/ui"].mimeTypes` containing
`text/html;profile=mcp-app`.

| Flag | Behaviour |
|---|---|
| `--ui=auto` (default) | Spec behaviour: UI only if the client advertises it. Otherwise no `_meta.ui`, no `resources` capability, `resources/*` → `-32601`. |
| `--ui=always` | UI for every client (a lenient server). |
| `--ui=never` | Text only. |

Use `auto` to find out whether a given client (Claude Code, `mcp-cli-proxy`, …)
advertises the capability: the server logs `initialize: mode=auto ui=true|false`
to stderr.

## Run

```sh
go run ./testdata/mcp-apps-sample --ui=auto
```

Register with Claude Code (from this repo root):

```sh
claude mcp add mcp-apps-sample -- go run ./testdata/mcp-apps-sample --ui=auto
```

Quick check by hand:

```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"extensions":{"io.modelcontextprotocol/ui":{"mimeTypes":["text/html;profile=mcp-app"]}}},"clientInfo":{"name":"t","version":"1"}}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"ui://mcp-apps-sample/calendar.html"}}' \
| go run ./testdata/mcp-apps-sample
```

Opening `calendar.html` directly in a browser shows demo data and a note that
no host is present.
