package main

import (
	"os"
	"path/filepath"
	"testing"
)

// readAsset is a tiny helper that reads a viewer asset from dir and fails the
// test on error.
func readAsset(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// embeddedAsset returns the canonical embedded body for name.
func embeddedAsset(t *testing.T, name string) []byte {
	t.Helper()
	b, err := chatLogViewerFS.ReadFile("chatlog-viewer/assets/" + name)
	if err != nil {
		t.Fatalf("read embedded %s: %v", name, err)
	}
	return b
}

// TestEnsureViewerAssets_WritesEmbeddedWhenMissing checks that a fresh export
// dir gets both assets, byte-for-byte equal to the embedded source.
func TestEnsureViewerAssets_WritesEmbeddedWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := ensureViewerAssets(dir); err != nil {
		t.Fatalf("ensureViewerAssets: %v", err)
	}
	for _, name := range []string{"viewer.css", "viewer.js"} {
		if got := readAsset(t, dir, name); string(got) != string(embeddedAsset(t, name)) {
			t.Errorf("%s: written content differs from embedded source", name)
		}
	}
}

// TestEnsureViewerAssets_OverwritesExisting verifies that these files are
// agent-chat-owned: any pre-existing copy (a stale older version, or a user's
// hand-edit) is overwritten with the current embedded version on every export.
func TestEnsureViewerAssets_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "viewer.css"), []byte(".bubble{color:hotpink}\n"))
	mustWrite(t, filepath.Join(dir, "viewer.js"), []byte("window.viewer = { custom: true };\n"))

	if err := ensureViewerAssets(dir); err != nil {
		t.Fatalf("ensureViewerAssets: %v", err)
	}
	for _, name := range []string{"viewer.css", "viewer.js"} {
		if got := readAsset(t, dir, name); string(got) != string(embeddedAsset(t, name)) {
			t.Errorf("%s: existing copy was not overwritten with embedded version", name)
		}
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
