package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChatMDPathFilesByMonth: a new export lands in {YYYY-MM}/{DD}-{NN}-slug.md
// and its attachments go beside it, while the viewer's own files stay at the
// archive root.
func TestChatMDPathFilesByMonth(t *testing.T) {
	root := "/archive"
	md := chatMDPath(root, "2026-08-15", "01", "some-title")
	if want := "/archive/2026-08/15-01-some-title.md"; md != want {
		t.Errorf("chatMDPath = %q, want %q", md, want)
	}
	if got, want := exportAssetsDir(md), "/archive/2026-08/assets"; got != want {
		t.Errorf("exportAssetsDir = %q, want %q", got, want)
	}
	if got, want := viewerAssetsDir(root), "/archive/assets"; got != want {
		t.Errorf("viewerAssetsDir = %q, want %q", got, want)
	}
	// An attachment keeps its full-date basename even inside a month dir, so
	// migrating an old export never has to rewrite committed markdown.
	if got, want := chatAssetPrefix("2026-08-15", "01"), "2026-08-15-01-"; got != want {
		t.Errorf("chatAssetPrefix = %q, want %q", got, want)
	}
	// A date that isn't one degrades to the flat root rather than inventing a
	// junk directory.
	if got := chatMDPath(root, "not-a-date", "01", "x"); got != "/archive/not-a-date-01-x.md" {
		t.Errorf("malformed date should stay flat, got %q", got)
	}
}

// TestScanChatExportsBothLayouts: an archive mid-migration is read whole —
// month-filed and legacy flat exports alike, with non-exports ignored.
func TestScanChatExportsBothLayouts(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "2026-08", "assets"))
	mustMkdir(t, filepath.Join(root, "assets"))
	writeMd(t, root, "2026-07-30-02-legacy.md", "# legacy\n")
	writeMd(t, filepath.Join(root, "2026-08"), "15-01-fresh.md", "# fresh\n")
	writeMd(t, root, "notes.md", "# not an export\n")
	writeMd(t, filepath.Join(root, "assets"), "viewer.js", "// not an export\n")

	got := map[string]chatExportFile{}
	for _, ex := range scanChatExports(root) {
		got[ex.Rel] = ex
	}
	if len(got) != 2 {
		t.Fatalf("scanChatExports returned %d entries, want 2: %v", len(got), got)
	}
	fresh, ok := got["2026-08/15-01-fresh.md"]
	if !ok {
		t.Fatalf("month-filed export missing: %v", got)
	}
	if fresh.Date != "2026-08-15" || fresh.Index != "01" || fresh.Slug != "fresh" || fresh.Legacy {
		t.Errorf("month-filed export parsed wrong: %+v", fresh)
	}
	legacy, ok := got["2026-07-30-02-legacy.md"]
	if !ok {
		t.Fatalf("legacy export missing: %v", got)
	}
	if legacy.Date != "2026-07-30" || legacy.Index != "02" || !legacy.Legacy {
		t.Errorf("legacy export parsed wrong: %+v", legacy)
	}
}

// TestNextDailyIndexSpansBothLayouts: an un-migrated flat file for today still
// reserves its NN, so a session started mid-migration cannot mint a duplicate.
func TestNextDailyIndexSpansBothLayouts(t *testing.T) {
	root := t.TempDir()
	writeMd(t, root, "2026-08-15-03-legacy.md", "x")
	if got := nextDailyIndex(root, "2026-08-15"); got != 4 {
		t.Errorf("nextDailyIndex with only a legacy flat file = %d, want 4", got)
	}
	mustMkdir(t, filepath.Join(root, "2026-08"))
	writeMd(t, filepath.Join(root, "2026-08"), "15-07-fresh.md", "x")
	if got := nextDailyIndex(root, "2026-08-15"); got != 8 {
		t.Errorf("nextDailyIndex across both layouts = %d, want 8", got)
	}
	if got := nextDailyIndex(root, "2026-08-16"); got != 1 {
		t.Errorf("nextDailyIndex for an unused date = %d, want 1", got)
	}
}

// TestRenamedMDPathStaysPut: set_chat_title renames in place. A legacy export
// resumed after a restart must not be silently relocated by a retitle —
// migration is migrateChatLogs' job alone.
func TestRenamedMDPathStaysPut(t *testing.T) {
	root := "/archive"
	legacy := "/archive/2026-08-15-01-untitled.md"
	if got, want := renamedMDPath(legacy, root, "2026-08-15", "01", "real"), "/archive/2026-08-15-01-real.md"; got != want {
		t.Errorf("legacy retitle = %q, want %q", got, want)
	}
	monthly := "/archive/2026-08/15-01-untitled.md"
	if got, want := renamedMDPath(monthly, root, "2026-08-15", "01", "real"), "/archive/2026-08/15-01-real.md"; got != want {
		t.Errorf("month-filed retitle = %q, want %q", got, want)
	}
}

// TestRegenerateIndexHTMLMonthLayout: the manifest links a month-filed export
// through its subdirectory, and lists a not-yet-migrated flat one alongside it.
func TestRegenerateIndexHTMLMonthLayout(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "2026-08"))
	writeMd(t, filepath.Join(root, "2026-08"), "15-01-fresh-chat.md",
		"<!-- agent-chat export\ntitle: Fresh Chat\ndate: 2026-08-15\nindex: 01\nslug: fresh-chat\n-->\n\n# Fresh Chat\n")
	writeMd(t, root, "2026-07-30-02-old-chat.md",
		"<!-- agent-chat export\ntitle: Old Chat\ndate: 2026-07-30\nindex: 02\nslug: old-chat\n-->\n\n# Old Chat\n")

	if err := regenerateIndexHTML(root); err != nil {
		t.Fatalf("regenerateIndexHTML: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`{ md: './2026-08/15-01-fresh-chat.md', date: '2026-08-15', idx: '01', title: 'Fresh Chat' },`,
		`{ md: './2026-07-30-02-old-chat.md', date: '2026-07-30', idx: '02', title: 'Old Chat' },`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing manifest line %q\n---\n%s", want, html)
		}
	}
	if i, j := strings.Index(html, "2026-08/15-01"), strings.Index(html, "2026-07-30-02"); !(i < j) {
		t.Errorf("manifest not newest-first: month-filed=%d legacy=%d", i, j)
	}
	// indexReferencesMD has to agree with the spelling the manifest actually
	// uses, or set_chat_title would stop refreshing a published index.
	if !indexReferencesMD(root, filepath.Join(root, "2026-08", "15-01-fresh-chat.md")) {
		t.Error("indexReferencesMD missed a month-filed export it just published")
	}
	if indexReferencesMD(root, filepath.Join(root, "2026-08", "15-99-absent.md")) {
		t.Error("indexReferencesMD claims an unpublished export is in the index")
	}
}

// TestPlanChatLogMigration: the plan moves flat exports and their attachments
// into month directories, leaves the viewer's files and index.html at the
// root, and preserves attachment basenames so committed `./assets/…` links
// keep resolving.
func TestPlanChatLogMigration(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	mustMkdir(t, assets)
	writeMd(t, root, "2026-07-30-02-old-chat.md",
		"![shot](./assets/2026-07-30-02-1-abc123def456.png)\n![hand](assets/demo-hand-added.png)\n")
	writeMd(t, assets, "2026-07-30-02-1-abc123def456.png", "png")
	writeMd(t, assets, "viewer.css", "/* viewer */")
	writeMd(t, assets, "viewer.js", "// viewer")
	writeMd(t, root, "index.html", "<html></html>")

	// A hand-added screenshot that only the markdown knows about must travel
	// with the chat that shows it, exactly like a generated attachment.
	writeMd(t, assets, "demo-hand-added.png", "png")

	moves, warnings := planChatLogMigration(root)
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	got := map[string]string{}
	for _, mv := range moves {
		got[rel(t, root, mv.From)] = rel(t, root, mv.To)
	}
	want := map[string]string{
		"2026-07-30-02-old-chat.md":               "2026-07/30-02-old-chat.md",
		"assets/2026-07-30-02-1-abc123def456.png": "2026-07/assets/2026-07-30-02-1-abc123def456.png",
		"assets/demo-hand-added.png":              "2026-07/assets/demo-hand-added.png",
	}
	if len(got) != len(want) {
		t.Fatalf("plan has %d moves, want %d: %v", len(got), len(want), got)
	}
	for from, to := range want {
		if got[from] != to {
			t.Errorf("plan moves %q → %q, want %q", from, got[from], to)
		}
	}

	// Running the plan makes the archive migration-clean and idempotent.
	for _, mv := range moves {
		mustMkdir(t, filepath.Dir(mv.To))
		if err := os.Rename(mv.From, mv.To); err != nil {
			t.Fatal(err)
		}
	}
	if n := legacyChatExportCount(root); n != 0 {
		t.Errorf("after migrating, %d legacy exports remain", n)
	}
	if again, _ := planChatLogMigration(root); len(again) != 0 {
		t.Errorf("migration is not idempotent, still plans %v", again)
	}
	// The markdown was never rewritten, so its relative link must still land
	// on the attachment — now in the same month directory.
	md, err := os.ReadFile(filepath.Join(root, "2026-07", "30-02-old-chat.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "./assets/2026-07-30-02-1-abc123def456.png") {
		t.Errorf("migration rewrote the markdown: %s", md)
	}
	if _, err := os.Stat(filepath.Join(root, "2026-07", "assets", "2026-07-30-02-1-abc123def456.png")); err != nil {
		t.Errorf("attachment did not land beside its chat: %v", err)
	}
	for _, keep := range []string{"index.html", "assets/viewer.css", "assets/viewer.js"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(keep))); err != nil {
			t.Errorf("%s should stay at the archive root: %v", keep, err)
		}
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
}

func rel(t *testing.T, root, abs string) string {
	t.Helper()
	r, err := filepath.Rel(root, abs)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(r)
}

// TestPlanChatLogMigrationSharedAsset: one screenshot referenced by chats in
// two different months has no single right home, so it stays at the root and
// is reported instead of silently breaking one of the two chats.
func TestPlanChatLogMigrationSharedAsset(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	mustMkdir(t, assets)
	writeMd(t, assets, "shared.png", "png")
	writeMd(t, root, "2026-07-30-01-july.md", "![s](./assets/shared.png)\n")
	writeMd(t, root, "2026-08-01-01-august.md", "![s](./assets/shared.png)\n")

	moves, warnings := planChatLogMigration(root)
	for _, mv := range moves {
		if strings.Contains(mv.From, "shared.png") {
			t.Errorf("shared asset should not move, plan has %s → %s", mv.From, mv.To)
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "shared.png") {
		t.Errorf("want one warning naming shared.png, got %v", warnings)
	}
}
