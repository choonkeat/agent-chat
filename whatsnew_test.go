package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleChangelog = `# Changelog

Preamble.

## [1.2.0] — 2026-01-03

### Changes
- three

## [1.1.0] — 2026-01-02

- two

## [1.0.0] — 2026-01-01

- one
`

func TestLatestVersionIn(t *testing.T) {
	if got := latestVersionIn(sampleChangelog); got != "1.2.0" {
		t.Fatalf("latest = %q, want 1.2.0", got)
	}
	if got := latestVersionIn("# Changelog\n\nnothing yet\n"); got != "" {
		t.Fatalf("latest of empty changelog = %q, want empty", got)
	}
}

func TestRecentReleases(t *testing.T) {
	got := recentReleases(sampleChangelog, 2)
	if !strings.HasPrefix(got, "## [1.2.0]") {
		t.Fatalf("should start at the newest heading, got %q", got)
	}
	if !strings.Contains(got, "## [1.1.0]") || strings.Contains(got, "1.0.0") {
		t.Fatalf("should hold exactly two releases, got %q", got)
	}
	if strings.Contains(got, "Preamble") {
		t.Fatalf("preamble leaked: %q", got)
	}
	if all := recentReleases(sampleChangelog, 10); !strings.Contains(all, "## [1.0.0]") {
		t.Fatalf("n beyond the count should return every release, got %q", all)
	}
}

// The embedded CHANGELOG is the real one: its newest heading must parse, or the
// unread dot silently never shows.
func TestEmbeddedChangelogHasLatest(t *testing.T) {
	if latestChangelogVersion() == "" {
		t.Fatal("embedded CHANGELOG.md has no '## [x.y.z]' heading")
	}
	rec := httptest.NewRecorder()
	handleChangelog(rec, httptest.NewRequest("GET", "/api/changelog", nil))
	var body struct{ Latest, Markdown string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Latest != latestChangelogVersion() || !strings.HasPrefix(body.Markdown, "## [") {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestUnwrapLines(t *testing.T) {
	in := "## [1.0.0] — x\n\n### Changes\n- **Bold.** Start with `a\n  b` here\n  and end.\n  - nested\n    more\n- next\n\nPara one\nstill para.\n\n```\ncode\nkept\n```\n"
	want := "## [1.0.0] — x\n\n### Changes\n- **Bold.** Start with `a b` here and end.\n  - nested more\n- next\n\nPara one still para.\n\n```\ncode\nkept\n```\n"
	if got := unwrapLines(in); got != want {
		t.Fatalf("unwrapLines:\n got %q\nwant %q", got, want)
	}
}
