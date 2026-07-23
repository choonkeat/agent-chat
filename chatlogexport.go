package main

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed chatlog-viewer/index.html chatlog-viewer/assets/viewer.css chatlog-viewer/assets/viewer.js
var chatLogViewerFS embed.FS

// chatExportMeta is the data baked into the system header at the top of an
// exported markdown file.
type chatExportMeta struct {
	Title   string
	Date    string // YYYY-MM-DD
	Index   string // 2-digit zero-padded
	Slug    string
	Session string // session UUID (streaming exports; lets a restarted process find its file)
	Agent   string
	Version string // agent-chat version + commit
}

// renderChatMarkdown produces script-style markdown for a chat export. Each
// turn is delimited by a `**USER**` or `**AGENT**` marker on its own line; the
// body follows as a `> ` blockquote. Before each agent turn (except the first)
// a `<small>took NN.Ns</small><br>` line records the elapsed time since the
// previous bubble. Quick replies sent with an agent turn are listed as a
// trailing `[Quick replies]` bullet block. Images attached to either a user
// or an agent turn render inline within that turn's blockquote, wrapped in a
// flex `<div>` so md-serve / our viewer tile them three-up while GitHub's
// sanitizer (which strips inline styles) gracefully degrades to one-per-row.
func renderChatMarkdown(events []Event, meta chatExportMeta, imageMap map[string]string) string {
	var b strings.Builder

	b.WriteString("<!-- agent-chat export\n")
	fmt.Fprintf(&b, "title: %s\n", meta.Title)
	fmt.Fprintf(&b, "date: %s\n", meta.Date)
	fmt.Fprintf(&b, "index: %s\n", meta.Index)
	fmt.Fprintf(&b, "slug: %s\n", meta.Slug)
	if meta.Session != "" {
		fmt.Fprintf(&b, "session: %s\n", meta.Session)
	}
	if meta.Agent != "" {
		fmt.Fprintf(&b, "agent: %s\n", meta.Agent)
	}
	if meta.Version != "" {
		fmt.Fprintf(&b, "version: %s\n", meta.Version)
	}
	b.WriteString("-->\n\n")

	fmt.Fprintf(&b, "# %s\n\n", meta.Title)

	bylineBits := []string{meta.Date, meta.Index}
	if meta.Agent != "" {
		bylineBits = append(bylineBits, meta.Agent)
	}
	if meta.Version != "" {
		bylineBits = append(bylineBits, "agent-chat "+meta.Version)
	}
	fmt.Fprintf(&b, "_%s_\n\n", strings.Join(bylineBits, " · "))

	var st renderState
	for _, e := range events {
		b.WriteString(renderChatBubble(e, &st, imageMap))
	}

	return b.String()
}

// renderState carries the fold state renderChatBubble threads between bubbles:
// lastTs is the timestamp of the previous rendered bubble, used to emit the
// `<small>took Ns</small>` elapsed line before an agent turn.
type renderState struct {
	lastTs int64
}

// renderChatBubble renders a single event as markdown, updating st. Events
// that produce no output (hidden types like toolMarker, or turns whose body
// and attachments are all empty) return "" and leave st untouched. Both the
// batch exporter (renderChatMarkdown) and the streaming writer fold this over
// their events, so their outputs are identical by construction.
func renderChatBubble(e Event, st *renderState, imageMap map[string]string) string {
	var b strings.Builder
	switch e.Type {
	case "userMessage":
		body := strings.TrimSpace(e.Text)
		imgBlock := imageBlock(e.Files, imageMap)
		if body == "" && imgBlock == "" {
			return ""
		}
		b.WriteString("**USER**\n\n")
		if body != "" {
			b.WriteString(blockquoteText(body))
			b.WriteString("\n")
		}
		if imgBlock != "" {
			if body != "" {
				b.WriteString(">\n")
			}
			b.WriteString(imgBlock)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		if e.Timestamp > 0 {
			st.lastTs = e.Timestamp
		}
	case "agentMessage", "verbalReply":
		body := strings.TrimSpace(e.Text)
		imgBlock := imageBlock(e.Files, imageMap)
		if body == "" && imgBlock == "" {
			return ""
		}
		// Mirror client-dist/app.js:323-333: any previous bubble (user or
		// agent) sets lastTs; if a delta is computable, emit it.
		if st.lastTs > 0 && e.Timestamp > st.lastTs {
			fmt.Fprintf(&b, "<small>took %s</small><br>\n", formatElapsed(e.Timestamp-st.lastTs))
		}
		b.WriteString("**AGENT**\n\n")
		if body != "" {
			b.WriteString(blockquoteText(body))
			b.WriteString("\n")
		}
		if imgBlock != "" {
			if body != "" {
				b.WriteString(">\n")
			}
			b.WriteString(imgBlock)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		if qr := quickRepliesBlock(e.QuickReplies); qr != "" {
			b.WriteString(qr)
		}
		if e.Timestamp > 0 {
			st.lastTs = e.Timestamp
		}
	}
	return b.String()
}

// blockquoteText prefixes every line of s with `> `, matching CommonMark
// blockquote semantics. A line that is already `>`-prefixed nests deeper
// (e.g. `> foo` becomes `> > foo`), preserving the author's intent.
func blockquoteText(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		if l == "" {
			out[i] = ">"
		} else {
			out[i] = "> " + l
		}
	}
	return strings.Join(out, "\n")
}

// quickRepliesBlock formats a list of quick replies as the trailing bullet
// block emitted at the end of an agent turn. Returns "" if the slice is empty
// or contains only blanks.
func quickRepliesBlock(replies []string) string {
	var nonEmpty []string
	for _, r := range replies {
		if strings.TrimSpace(r) != "" {
			nonEmpty = append(nonEmpty, r)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Quick replies]\n")
	for _, r := range nonEmpty {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	b.WriteString("\n")
	return b.String()
}

// imageBlock renders a turn's attached files as an inline flex `<div>` of
// `<a><img></a>` tags, each line prefixed with `> ` so it lives inside the
// turn's blockquote (used for both user and agent turns). Non-image
// attachments are emitted as plain links instead of `<img>`. Returns "" if no
// displayable attachments.
func imageBlock(files []FileRef, imageMap map[string]string) string {
	if len(files) == 0 {
		return ""
	}
	var imgs, others []string
	for _, f := range files {
		rel := imageMap[f.Path]
		if rel == "" {
			continue
		}
		if isImage(f) {
			// Flex constraints go on the <a> (the direct flex item) so each
			// link-wrapped thumbnail occupies a third of the row. Without
			// this the <a> would shrink to its content and stack 1 per row.
			imgs = append(imgs, fmt.Sprintf(
				`<a href="%s" style="flex:0 1 calc(33%% - 8px);max-width:calc(33%% - 8px);"><img src="%s" alt="%s" style="width:100%%;height:auto;display:block;border-radius:6px;"></a>`,
				rel, rel, html.EscapeString(f.Name)))
		} else {
			others = append(others, fmt.Sprintf("[%s](%s)", strings.ReplaceAll(f.Name, "]", ""), rel))
		}
	}
	if len(imgs) == 0 && len(others) == 0 {
		return ""
	}
	var lines []string
	for _, link := range others {
		lines = append(lines, "> "+link)
	}
	if len(imgs) > 0 {
		if len(others) > 0 {
			lines = append(lines, ">")
		}
		lines = append(lines, `> <div style="display:flex;flex-wrap:wrap;gap:8px;">`)
		for _, img := range imgs {
			lines = append(lines, "> "+img)
		}
		lines = append(lines, `> </div>`)
	}
	return strings.Join(lines, "\n")
}

// isImage returns true if f looks like an image based on MIME type or
// extension. Used to decide whether to render a `<img>` tag (with flex layout)
// or a plain markdown link.
func isImage(f FileRef) bool {
	if strings.HasPrefix(f.Type, "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(f.Name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif", ".heic":
		return true
	}
	return false
}

// formatElapsed mirrors the JS-side formatter (client-dist/app.js:314): under
// a second -> "Nms", under a minute -> "N.Ns", otherwise "Nm Ns".
func formatElapsed(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := float64(ms) / 1000
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs) / 60
	rem := int(secs) % 60
	return fmt.Sprintf("%dm %ds", mins, rem)
}

// writeImageAttachments copies files attached to user *and* agent turns from
// their server-side upload paths to assetsDir/{date}-{NN}-N-{sha12}.{ext}. The
// content digest before the extension guarantees distinct content never shares a
// filename even if the numbering ever repeats across exports. Returns a
// map from each source path to the relative URL the .md should reference.
// Both parties' attachments are embedded — an agent screenshot posted via
// send_message/send_progress is archived the same way a user upload is. Only
// these turn types are scanned; hidden bookkeeping events (e.g. toolMarker)
// never carry user-facing files. Non-image files are also copied
// (renderChatMarkdown emits them as plain links rather than `<img>` tags, but
// the relative-path link still points to a real file).
//
// An attachment whose source file has since been moved or deleted (uploads are
// transient scratch files) is skipped with a warning rather than failing the
// whole export — a stale reference from one turn must not prevent archiving the
// rest of the conversation. renderChatMarkdown omits any attachment missing
// from the returned map, so the .md simply won't reference the skipped file.
// The collected warnings are returned so the caller can surface them.
func writeImageAttachments(events []Event, assetsDir, date, idx string) (map[string]string, []string, error) {
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("mkdir assets: %w", err)
	}
	out := map[string]string{}
	var warnings []string
	n := 0
	for _, e := range events {
		switch e.Type {
		case "userMessage", "agentMessage", "verbalReply":
		default:
			continue
		}
		w, err := writeEventAttachments(e, assetsDir, date, idx, &n, out)
		warnings = append(warnings, w...)
		if err != nil {
			return nil, nil, err
		}
	}
	return out, warnings, nil
}

// writeEventAttachments copies a single event's attachments into assetsDir
// (which must already exist), advancing *n for each new asset and recording
// source-path → relative-URL mappings in out. Paths already present in out are
// skipped, so a shared map dedups across events. Missing source files produce
// warnings, not errors (see writeImageAttachments). This is the per-event
// primitive both the batch exporter and the streaming writer use.
func writeEventAttachments(e Event, assetsDir, date, idx string, n *int, out map[string]string) ([]string, error) {
	var warnings []string
	for _, f := range e.Files {
		if f.Path == "" {
			continue
		}
		if _, ok := out[f.Path]; ok {
			continue
		}
		ext := filepath.Ext(f.Name)
		if ext == "" && f.Type != "" {
			if exts, _ := mime.ExtensionsByType(f.Type); len(exts) > 0 {
				ext = exts[0]
			}
		}
		*n++
		// Copy under a provisional numbered name, then rename to include a
		// content digest before the extension so assets never collide even
		// if the numbering ever repeats: {date}-{NN}-{N}-{sha12}.{ext}.
		staging := filepath.Join(assetsDir, fmt.Sprintf("%s-%s-%d.partial%s", date, idx, *n, ext))
		sum, err := copyFileSum(f.Path, staging)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Source vanished between the upload and the export. Warn
				// and skip rather than aborting the whole export.
				warnings = append(warnings, fmt.Sprintf("skipped missing attachment %q (%s)", f.Name, f.Path))
				continue
			}
			return warnings, fmt.Errorf("copy %s → %s: %w", f.Path, staging, err)
		}
		dstName := fmt.Sprintf("%s-%s-%d-%s%s", date, idx, *n, sum, ext)
		dst := filepath.Join(assetsDir, dstName)
		if err := os.Rename(staging, dst); err != nil {
			os.Remove(staging)
			return warnings, fmt.Errorf("rename %s → %s: %w", staging, dst, err)
		}
		out[f.Path] = "./assets/" + dstName
	}
	return warnings, nil
}

// copyFileSum copies src to dst and returns a short hex sha256 of the bytes it
// wrote. The hash is streamed off the same read used for the copy (io.TeeReader)
// so the source is read exactly once. Callers use the returned digest as a
// filename suffix so two assets with identical numbering but different content
// can never clobber each other (and identical content yields an identical name,
// which is fine — the second copy is a harmless rewrite).
func copyFileSum(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(out, io.TeeReader(in, h)); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// ensureViewerAssets writes the embedded viewer.css and viewer.js into dir,
// overwriting any existing copies. These files are owned by agent-chat, not the
// user: every export refreshes them so bundled fixes always reach every
// archive. (A user wanting custom styling should edit the embedded source and
// rebuild, not patch the served copy — it will be clobbered on the next
// export.) index.html is handled separately by regenerateIndexHTML, which
// derives the manifest from the export files on disk.
func ensureViewerAssets(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	for _, name := range []string{"viewer.css", "viewer.js"} {
		data, err := chatLogViewerFS.ReadFile("chatlog-viewer/assets/" + name)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// manifestEntry is rendered as one line of the inline MANIFEST array in
// index.html.
type manifestEntry struct {
	MD    string
	Date  string
	Index string
	Title string
}

func (e manifestEntry) jsLine() string {
	return fmt.Sprintf(
		`  { md: %s, date: %s, idx: %s, title: %s },`,
		jsString(e.MD), jsString(e.Date), jsString(e.Index), jsString(e.Title),
	)
}

// jsString renders a Go string as a JS single-quoted string with appropriate
// escaping for `\` and `'`.
func jsString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return `'` + s + `'`
}

// manifestOpenRE matches the line `const MANIFEST = [` (followed by newline)
// in the embedded index.html template. regenerateIndexHTML inserts the derived
// manifest entries immediately after this line.
var manifestOpenRE = regexp.MustCompile(`(?m)^[ \t]*const[ \t]+MANIFEST[ \t]*=[ \t]*\[[ \t]*\n`)

// mdExportNameRE parses an exported chat filename: {YYYY-MM-DD}-{NN}-{slug}.md
// with NN 2 or 3 digits (matching nextDailyIndex). Files that don't match are
// not exports and are excluded from the manifest.
var mdExportNameRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-(\d{2,3})-(.+)\.md$`)

// chatHeaderRE matches one `key: value` line inside the leading
// `<!-- agent-chat export` header comment.
var chatHeaderRE = regexp.MustCompile(`^([a-z]+):\s*(.*)$`)

// readChatHeader parses the `<!-- agent-chat export ... -->` comment at the
// top of an exported .md file into key/value pairs. Returns nil if the file
// doesn't start with the header (e.g. a hand-written or truncated file).
func readChatHeader(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "<!-- agent-chat export" {
		return nil
	}
	fields := map[string]string{}
	for i := 0; sc.Scan() && i < 32; i++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "-->" {
			return fields
		}
		if m := chatHeaderRE.FindStringSubmatch(line); m != nil {
			fields[m[1]] = m[2]
		}
	}
	return fields
}

// indexReferencesMD reports whether dir/index.html already carries a manifest
// entry for the given .md basename. It is the "is this export already
// published?" test that keeps index.html — a tracked file — from being dirtied
// on behalf of an export that is still private to the running session: a
// rename only has to be reflected in the index if the old name was in there.
// A missing or unreadable index.html answers false.
func indexReferencesMD(dir, mdBase string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "'./"+mdBase+"'")
}

// regenerateIndexHTML rewrites dir/index.html from scratch: the embedded
// template plus a MANIFEST derived entirely from the `*.md` export files
// present in dir (newest first; title read from each file's header comment,
// falling back to humanTitle(slug)). Because the output is a pure function of
// the directory contents it is idempotent, and a corrupted index.html (e.g.
// git merge markers) is healed by simply being rewritten.
//
// index.html is tracked by git, so callers must only invoke this at moments
// the export set changes in a *committable* way: chatlog_close, chatlog_optout
// (which may remove an already-committed entry), export_chat_md, and a
// set_chat_title that renames an export already present in the manifest.
// Notably NOT on every appended bubble — regenerating live would leave the
// working tree permanently dirty with manifest entries pointing at untracked,
// still-renameable `untitled-{uuid}.md` files.
func regenerateIndexHTML(dir string) error {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}
	type sortableEntry struct {
		manifestEntry
		idxNum int
	}
	var entries []sortableEntry
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		m := mdExportNameRE.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		date, idx, slug := m[1], m[2], m[3]
		// A provisional `untitled` / `untitled-{uuid}` export is by definition
		// not commit-ready — chatlog_close refuses to close one — and its
		// filename still changes under set_chat_title. Listing it would put a
		// soon-to-be-dead link into a tracked file, so it stays out of the
		// manifest until it has a real title.
		if isProvisionalSlug(slug) {
			continue
		}
		idxNum, err := strconv.Atoi(idx)
		if err != nil {
			continue
		}
		title := readChatHeader(filepath.Join(dir, de.Name()))["title"]
		if title == "" {
			title = humanTitle(slug)
		}
		entries = append(entries, sortableEntry{
			manifestEntry: manifestEntry{MD: "./" + de.Name(), Date: date, Index: idx, Title: title},
			idxNum:        idxNum,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Date != entries[j].Date {
			return entries[i].Date > entries[j].Date
		}
		if entries[i].idxNum != entries[j].idxNum {
			return entries[i].idxNum > entries[j].idxNum
		}
		return entries[i].MD > entries[j].MD
	})

	tmpl, err := chatLogViewerFS.ReadFile("chatlog-viewer/index.html")
	if err != nil {
		return fmt.Errorf("read embedded index.html: %w", err)
	}
	loc := manifestOpenRE.FindIndex(tmpl)
	if loc == nil {
		return fmt.Errorf("manifest opening line not found in embedded index.html template")
	}
	var lines strings.Builder
	for _, e := range entries {
		lines.WriteString(e.jsLine())
		lines.WriteByte('\n')
	}
	out := make([]byte, 0, len(tmpl)+lines.Len())
	out = append(out, tmpl[:loc[1]]...)
	out = append(out, []byte(lines.String())...)
	out = append(out, tmpl[loc[1]:]...)
	return os.WriteFile(filepath.Join(dir, "index.html"), out, 0644)
}

// runChatMarkdownExport is the orchestrator the MCP tool calls. It writes the
// .md file, copies image attachments, ensures viewer assets exist, and
// upserts index.html. Returns the absolute path of the .md file and any
// non-fatal warnings (e.g. attachments whose source files had gone missing).
func runChatMarkdownExport(rootDir, slug string, events []Event, agent string, version string, now time.Time) (string, []string, error) {
	date := now.Format("2006-01-02")
	idx := fmt.Sprintf("%02d", nextDailyIndex(rootDir, date))
	mdPath := filepath.Join(rootDir, fmt.Sprintf("%s-%s-%s.md", date, idx, slug))
	assetsDir := filepath.Join(rootDir, "assets")

	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return "", nil, fmt.Errorf("mkdir %s: %w", rootDir, err)
	}
	if err := ensureViewerAssets(assetsDir); err != nil {
		return "", nil, err
	}
	imageMap, warnings, err := writeImageAttachments(events, assetsDir, date, idx)
	if err != nil {
		return "", nil, err
	}

	meta := chatExportMeta{
		Title:   humanTitle(slug),
		Date:    date,
		Index:   idx,
		Slug:    slug,
		Agent:   agent,
		Version: version,
	}
	md := renderChatMarkdown(events, meta, imageMap)
	if err := os.WriteFile(mdPath, []byte(md), 0644); err != nil {
		return "", nil, fmt.Errorf("write %s: %w", mdPath, err)
	}

	if err := regenerateIndexHTML(rootDir); err != nil {
		return "", nil, err
	}
	return mdPath, warnings, nil
}

// humanTitle turns a kebab-case slug into a Title Case string for display.
func humanTitle(slug string) string {
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
