package main

import (
	"embed"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
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
	Agent   string
	Version string // agent-chat version + commit
}

// renderChatMarkdown produces the variant-C markdown content for a chat export.
// Each user/agent event becomes one 2-cell <table>. User turns put content in
// the right cell (left empty, 20%/80%). Agent turns put content in the left
// cell (right empty, 80%/20%). A 1-cell system row at the top carries the
// title + metadata.
//
// imageMap maps original file paths to relative URLs (e.g. "./assets/2026-04-30-01-1.png")
// so user-attached images render with the rewritten path.
func renderChatMarkdown(events []Event, meta chatExportMeta, imageMap map[string]string) string {
	var b strings.Builder

	// Hidden metadata block — invisible in any markdown renderer; preserved
	// for tooling that wants to read it programmatically.
	b.WriteString("<!-- agent-chat export\n")
	fmt.Fprintf(&b, "title: %s\n", meta.Title)
	fmt.Fprintf(&b, "date: %s\n", meta.Date)
	fmt.Fprintf(&b, "index: %s\n", meta.Index)
	fmt.Fprintf(&b, "slug: %s\n", meta.Slug)
	if meta.Agent != "" {
		fmt.Fprintf(&b, "agent: %s\n", meta.Agent)
	}
	if meta.Version != "" {
		fmt.Fprintf(&b, "version: %s\n", meta.Version)
	}
	b.WriteString("-->\n\n")

	// Visible system row at top: bold title + small muted metadata line.
	b.WriteString(`<table style="border-collapse:collapse;width:100%;border:0"><tr>` + "\n")
	b.WriteString(`<td style="border:0;text-align:center;color:#888;font-size:0.85em">` + "\n\n")
	fmt.Fprintf(&b, "**%s**\n\n", meta.Title)
	headerBits := []string{meta.Date, meta.Index}
	if meta.Agent != "" {
		headerBits = append(headerBits, meta.Agent)
	}
	if meta.Version != "" {
		headerBits = append(headerBits, "agent-chat "+meta.Version)
	}
	fmt.Fprintf(&b, "%s\n\n", strings.Join(headerBits, " · "))
	b.WriteString("</td>\n</tr></table>\n\n")

	for _, e := range events {
		switch e.Type {
		case "userMessage":
			body := e.Text
			for _, f := range e.Files {
				rel := imageMap[f.Path]
				if rel == "" {
					continue
				}
				if body != "" {
					body += "\n\n"
				}
				body += fmt.Sprintf("![%s](%s)", strings.ReplaceAll(f.Name, "]", ""), rel)
			}
			if strings.TrimSpace(body) == "" {
				continue
			}
			writeUserTurn(&b, body)
		case "agentMessage":
			if strings.TrimSpace(e.Text) == "" {
				continue
			}
			writeAgentTurn(&b, e.Text)
		}
	}

	return b.String()
}

func writeUserTurn(b *strings.Builder, body string) {
	b.WriteString(`<table style="border-collapse:collapse;width:100%;border:0"><tr>` + "\n")
	b.WriteString(`<td style="border:0;width:20%">&nbsp;</td>` + "\n")
	b.WriteString(`<td style="border:0;width:80%">` + "\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n\n</td>\n</tr></table>\n\n")
}

func writeAgentTurn(b *strings.Builder, body string) {
	b.WriteString(`<table style="border-collapse:collapse;width:100%;border:0"><tr>` + "\n")
	b.WriteString(`<td style="border:0;width:80%">` + "\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n\n</td>\n")
	b.WriteString(`<td style="border:0;width:20%">&nbsp;</td>` + "\n")
	b.WriteString("</tr></table>\n\n")
}

// writeImageAttachments copies user-uploaded images from their server-side
// upload paths to assetsDir/{date}-{NN}-N.{ext}. Returns a map from each
// source path to the relative URL the .md should reference.
func writeImageAttachments(events []Event, assetsDir, date, idx string) (map[string]string, error) {
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir assets: %w", err)
	}
	out := map[string]string{}
	n := 0
	for _, e := range events {
		if e.Type != "userMessage" {
			continue
		}
		for _, f := range e.Files {
			if f.Path == "" {
				continue
			}
			if _, ok := out[f.Path]; ok {
				continue // dedupe — same upload referenced twice
			}
			ext := filepath.Ext(f.Name)
			if ext == "" && f.Type != "" {
				if exts, _ := mime.ExtensionsByType(f.Type); len(exts) > 0 {
					ext = exts[0]
				}
			}
			n++
			dstName := fmt.Sprintf("%s-%s-%d%s", date, idx, n, ext)
			dst := filepath.Join(assetsDir, dstName)
			if err := copyFile(f.Path, dst); err != nil {
				return nil, fmt.Errorf("copy %s → %s: %w", f.Path, dst, err)
			}
			out[f.Path] = "./assets/" + dstName
		}
	}
	return out, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// ensureViewerAssets writes viewer.css and viewer.js to dir if either is
// missing. Idempotent — won't overwrite existing files (so users can patch
// the served versions if they want).
func ensureViewerAssets(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	for _, name := range []string{"viewer.css", "viewer.js"} {
		dst := filepath.Join(dir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		data, err := chatLogViewerFS.ReadFile("chatlog-viewer/assets/" + name)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", name, err)
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
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

// manifestOpenRE matches the line `const MANIFEST = [` (followed by newline).
// New entries are inserted immediately after this line so the array stays in
// descending order (newest first).
var manifestOpenRE = regexp.MustCompile(`(?m)^[ \t]*const[ \t]+MANIFEST[ \t]*=[ \t]*\[[ \t]*\n`)

// upsertIndexHTML writes the embedded index.html template if the file does
// not exist, then inserts a new manifest entry as the first element of the
// MANIFEST array (descending order — newest first).
func upsertIndexHTML(path string, entry manifestEntry) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		tmpl, err := chatLogViewerFS.ReadFile("chatlog-viewer/index.html")
		if err != nil {
			return fmt.Errorf("read embedded index.html: %w", err)
		}
		if err := os.WriteFile(path, tmpl, 0644); err != nil {
			return fmt.Errorf("write index.html: %w", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read index.html: %w", err)
	}
	loc := manifestOpenRE.FindIndex(data)
	if loc == nil {
		return fmt.Errorf("manifest opening line not found in %s — cannot prepend entry", path)
	}
	// Insert immediately after the `const MANIFEST = [\n` line.
	insertAt := loc[1]
	newLine := entry.jsLine() + "\n"
	out := make([]byte, 0, len(data)+len(newLine))
	out = append(out, data[:insertAt]...)
	out = append(out, []byte(newLine)...)
	out = append(out, data[insertAt:]...)
	return os.WriteFile(path, out, 0644)
}

// runChatMarkdownExport is the orchestrator the MCP tool calls. It writes the
// .md file, copies image attachments, ensures viewer assets exist, and
// upserts index.html. Returns the absolute path of the .md file.
func runChatMarkdownExport(rootDir, slug string, events []Event, agent string, version string, now time.Time) (string, error) {
	date := now.Format("2006-01-02")
	idx := fmt.Sprintf("%02d", nextDailyIndex(rootDir, date))
	mdPath := filepath.Join(rootDir, fmt.Sprintf("%s-%s-%s.md", date, idx, slug))
	assetsDir := filepath.Join(rootDir, "assets")

	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", rootDir, err)
	}
	if err := ensureViewerAssets(assetsDir); err != nil {
		return "", err
	}
	imageMap, err := writeImageAttachments(events, assetsDir, date, idx)
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("write %s: %w", mdPath, err)
	}

	indexPath := filepath.Join(rootDir, "index.html")
	mdRel := "./" + filepath.Base(mdPath)
	if err := upsertIndexHTML(indexPath, manifestEntry{
		MD: mdRel, Date: date, Index: idx, Title: meta.Title,
	}); err != nil {
		return "", err
	}
	return mdPath, nil
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
