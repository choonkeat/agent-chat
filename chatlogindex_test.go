package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMd(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestRegenerateIndexHTML: the MANIFEST is derived entirely from a glob of
// *.md files — every well-formed export listed newest first, titles read from
// the header comment with humanTitle(slug) as fallback — and regeneration is
// idempotent (second run is byte-identical).
func TestRegenerateIndexHTML(t *testing.T) {
	dir := t.TempDir()
	writeMd(t, dir, "2026-07-17-01-alpha-chat.md",
		"<!-- agent-chat export\ntitle: Alpha's Custom Title\ndate: 2026-07-17\nindex: 01\nslug: alpha-chat\n-->\n\n# Alpha's Custom Title\n")
	writeMd(t, dir, "2026-07-18-01-beta.md",
		"<!-- agent-chat export\ntitle: Beta\ndate: 2026-07-18\nindex: 01\nslug: beta\n-->\n\n# Beta\n")
	// No header comment at all → title falls back to humanTitle("second-untitled").
	writeMd(t, dir, "2026-07-18-02-second-untitled.md", "# raw\n")
	// Does not match the {date}-{NN}-{slug}.md pattern → skipped.
	writeMd(t, dir, "notes.md", "# not an export\n")

	if err := regenerateIndexHTML(dir); err != nil {
		t.Fatalf("regenerateIndexHTML: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(first)

	for _, want := range []string{
		`{ md: './2026-07-18-02-second-untitled.md', date: '2026-07-18', idx: '02', title: 'Second Untitled' },`,
		`{ md: './2026-07-18-01-beta.md', date: '2026-07-18', idx: '01', title: 'Beta' },`,
		`{ md: './2026-07-17-01-alpha-chat.md', date: '2026-07-17', idx: '01', title: 'Alpha\'s Custom Title' },`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing manifest line %q\n---\n%s", want, html)
		}
	}
	if strings.Contains(html, "notes.md") {
		t.Errorf("index.html lists non-export notes.md\n---\n%s", html)
	}
	// Newest first: 18-02, then 18-01, then 17-01.
	i2 := strings.Index(html, "2026-07-18-02-second-untitled.md")
	i1 := strings.Index(html, "2026-07-18-01-beta.md")
	i0 := strings.Index(html, "2026-07-17-01-alpha-chat.md")
	if !(i2 < i1 && i1 < i0) {
		t.Errorf("manifest not newest-first: positions 18-02=%d 18-01=%d 17-01=%d", i2, i1, i0)
	}

	if err := regenerateIndexHTML(dir); err != nil {
		t.Fatalf("second regenerateIndexHTML: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != html {
		t.Errorf("regeneration is not idempotent:\n--- first:\n%s\n--- second:\n%s", html, second)
	}
}

// TestRegenerateIndexHTMLHealsConflict: an index.html corrupted by git merge
// markers (or anything else) is fully rewritten from the embedded template,
// not patched around.
func TestRegenerateIndexHTMLHealsConflict(t *testing.T) {
	dir := t.TempDir()
	writeMd(t, dir, "2026-07-18-01-solo.md",
		"<!-- agent-chat export\ntitle: Solo\ndate: 2026-07-18\nindex: 01\nslug: solo\n-->\n\n# Solo\n")

	conflicted := "<<<<<<< HEAD\nconst MANIFEST = [\n  { md: './old.md' },\n=======\ngarbage\n>>>>>>> theirs\n"
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(conflicted), 0644); err != nil {
		t.Fatal(err)
	}

	if err := regenerateIndexHTML(dir); err != nil {
		t.Fatalf("regenerateIndexHTML on conflicted file: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, bad := range []string{"<<<<<<<", ">>>>>>>", "old.md", "garbage"} {
		if strings.Contains(html, bad) {
			t.Errorf("healed index.html still contains %q\n---\n%s", bad, html)
		}
	}
	if !strings.Contains(html, `{ md: './2026-07-18-01-solo.md', date: '2026-07-18', idx: '01', title: 'Solo' },`) {
		t.Errorf("healed index.html missing manifest entry\n---\n%s", html)
	}
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Errorf("healed index.html not rebuilt from template\n---\n%s", html)
	}
}
