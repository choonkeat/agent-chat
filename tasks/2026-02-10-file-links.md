# Clickable File Links in Chat

## Goal

When file paths (e.g. `PLAN.md`, `main.go`, `/repos/agent-chat/workspace/tools.go`) appear in chat bubbles — whether from the user or agent — they become clickable links that open the file content in a new browser tab, served by the same chat web server.

## How It Works

### Detection (Client-Side)

In the markdown renderer (`renderMarkdown` in `app.js`), detect file path patterns in message text:
- Relative paths: `main.go`, `tools.go`, `client-dist/app.js`, `./tasks/foo.md`
- Absolute paths: `/repos/agent-chat/workspace/main.go`
- Backtick-wrapped paths: `` `PLAN.md` ``, `` `./tasks/foo.md` ``

Heuristic: match patterns that look like file paths (contain `.` extension or `/` separators) and are not URLs (no `http://`). Could use a regex like:
```
(?:^|[\s`])((\.{0,2}/)?[\w\-./]+\.\w+)
```

Convert detected paths to clickable links:
```html
<a href="/file?path=PLAN.md" target="_blank">PLAN.md</a>
```

### Serving Files (Server-Side)

Add a new HTTP route `/file` to the Go server:

```go
mux.HandleFunc("/file", handleFileView)
```

**`handleFileView` handler:**
1. Read `path` query parameter
2. Resolve relative paths against the working directory (cwd of the server)
3. Security: validate the resolved path is within the workspace (prevent path traversal)
4. Read the file content
5. Serve as a simple HTML page with:
   - File path as `<title>` and heading
   - Content in a `<pre><code>` block with syntax highlighting (optional)
   - Dark theme matching the chat UI
   - Or: serve raw text with `Content-Type: text/plain` for simplicity

### Path Resolution

- Relative paths resolve from the server's working directory
- Absolute paths used as-is (but validated to be within allowed directories)
- If file doesn't exist, show a 404 page

### Security

- **Path traversal prevention**: Resolve symlinks and check that the final path is within the workspace root
- **No write access**: Read-only file serving
- **Allowed extensions**: Optionally restrict to common code/text file extensions

## Implementation

### Go Changes

**main.go**: Add `/file` route and handler:
```go
func handleFileView(w http.ResponseWriter, r *http.Request) {
    filePath := r.URL.Query().Get("path")
    // resolve, validate, read, serve
}
```

### Client Changes

**app.js**: Extend `renderMarkdown()` to detect file paths and wrap them in `<a>` tags pointing to `/file?path=...`.

**style.css**: Style file links (possibly with a file icon or distinct color to differentiate from regular URLs).

## Open Questions

- Should we do syntax highlighting? Could use a lightweight library like Prism.js or highlight.js, or keep it simple with `<pre>` and monospace text.
- Should clicking a path that matches a line number pattern (e.g. `main.go:42`) scroll/highlight that line?
- Should we support directory listing if a directory path is clicked?
