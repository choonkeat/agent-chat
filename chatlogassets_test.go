package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withAssetMode sets the boot-time asset mode for one test.
func withAssetMode(t *testing.T, m assetMode) {
	t.Helper()
	prev := chatAssetMode
	chatAssetMode = m
	t.Cleanup(func() { chatAssetMode = prev })
}

// writePNG writes a w×h PNG to path; opaque=false gives it a transparent pixel.
// Pixel noise keeps the PNG large enough that shrinking actually wins.
func writePNG(t *testing.T, path string, w, h int, opaque bool) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewPCG(1, 2))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(rng.IntN(256)), uint8(y), uint8(x), 255})
		}
	}
	if !opaque {
		img.SetNRGBA(0, 0, color.NRGBA{})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, buf.Bytes())
}

func TestParseAssetMode(t *testing.T) {
	for in, want := range map[string]assetMode{"": assetsNone, "none": assetsNone, "SMALL": assetsSmall, " original ": assetsOriginal} {
		if got, ok := parseAssetMode(in); !ok || got != want {
			t.Errorf("parseAssetMode(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	if got, ok := parseAssetMode("tiny"); ok || got != assetsNone {
		t.Errorf("parseAssetMode(tiny) = %q, %v; want none, false", got, ok)
	}
}

// Mode none: images become numbered placeholders (identical names stay
// distinguishable), nothing binary lands in assets/, but pasted text is kept.
func TestRunChatMarkdownExportAssetsNone(t *testing.T) {
	dir := t.TempDir()
	up := t.TempDir()
	shot := filepath.Join(up, "a.png")
	shot2 := filepath.Join(up, "b.png")
	paste := filepath.Join(up, "paste.txt")
	writePNG(t, shot, 10, 10, true)
	writePNG(t, shot2, 10, 10, true)
	mustWrite(t, paste, []byte("long pasted text"))
	events := []Event{
		{Type: "userMessage", Text: "see", Timestamp: 1000, Files: []FileRef{
			{Name: "image.png", Path: shot, Type: "image/png"},
			{Name: "image.png", Path: shot2, Type: "image/png"},
			{Name: "paste.txt", Path: paste, Type: "text/plain"},
		}},
	}
	mdPath, _, err := runChatMarkdownExport(dir, "none-chat", events, "claude", "v1", assetsNone, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(mdPath)
	for _, want := range []string{"> [image.png #1]", "> [image.png #2]", "[paste.txt](./assets/"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("md missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(string(md), "<img") || strings.Contains(string(md), placeholderPrefix) {
		t.Errorf("md must have no <img> and no raw placeholder marker:\n%s", md)
	}
	files, _ := filepath.Glob(filepath.Join(exportAssetsDir(mdPath), "1970-*"))
	if len(files) != 1 || !strings.HasSuffix(files[0], ".txt") {
		t.Errorf("assets/ = %v; want only the .txt", files)
	}
}

// Mode small: a big opaque PNG becomes a ≤1280px JPEG; a transparent one
// stays PNG; both are smaller than the source.
func TestRunChatMarkdownExportAssetsSmall(t *testing.T) {
	dir := t.TempDir()
	up := t.TempDir()
	opaque := filepath.Join(up, "o.png")
	clear := filepath.Join(up, "c.png")
	writePNG(t, opaque, 1206, 2622, true)
	writePNG(t, clear, 1206, 2622, false)
	events := []Event{
		{Type: "agentMessage", Text: "shots", Timestamp: 1000, Files: []FileRef{
			{Name: "o.png", Path: opaque, Type: "image/png"},
			{Name: "c.png", Path: clear, Type: "image/png"},
		}},
	}
	mdPath, _, err := runChatMarkdownExport(dir, "small-chat", events, "claude", "v1", assetsSmall, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	assets := exportAssetsDir(mdPath)
	check := func(pattern, wantFormat string, src string) {
		t.Helper()
		m, _ := filepath.Glob(filepath.Join(assets, pattern))
		if len(m) != 1 {
			t.Fatalf("glob %s = %v", pattern, m)
		}
		f, _ := os.Open(m[0])
		defer f.Close()
		cfg, format, err := image.DecodeConfig(f)
		if err != nil || format != wantFormat || max(cfg.Width, cfg.Height) != smallAssetMaxEdge {
			t.Errorf("%s: format %s %dx%d err %v; want %s with long edge %d", m[0], format, cfg.Width, cfg.Height, err, wantFormat, smallAssetMaxEdge)
		}
		si, _ := os.Stat(src)
		di, _ := os.Stat(m[0])
		if di.Size() >= si.Size() {
			t.Errorf("%s: %d bytes, not smaller than source %d", m[0], di.Size(), si.Size())
		}
		md, _ := os.ReadFile(mdPath)
		if !strings.Contains(string(md), "./assets/"+filepath.Base(m[0])) {
			t.Errorf("md does not link %s", filepath.Base(m[0]))
		}
	}
	check("*-1-*.jpg", "jpeg", opaque)
	check("*-2-*.png", "png", clear)
}

// Mode small never makes a file bigger: an already-small image is copied as-is.
func TestShrinkImageKeepsSmallOriginal(t *testing.T) {
	src := filepath.Join(t.TempDir(), "s.jpg")
	var buf bytes.Buffer
	jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 8)), &jpeg.Options{Quality: 50})
	mustWrite(t, src, buf.Bytes())
	if _, _, ok := shrinkImage(src); ok {
		t.Error("shrinkImage re-encoded a tiny JPEG; want the original kept")
	}
}

// Streaming in mode none, then resuming: the placeholder ordinal survives the
// restart and the full rewrite matches the live output.
func TestChatLogStreamAssetsNoneResume(t *testing.T) {
	withAssetMode(t, assetsNone)
	dir := t.TempDir()
	up := t.TempDir()
	shot := filepath.Join(up, "s.png")
	writePNG(t, shot, 10, 10, true)
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	history := []Event{
		{Type: "userMessage", Text: "look", Timestamp: 1000, Files: []FileRef{{Name: "image.png", Path: shot, Type: "image/png"}}},
	}
	s1, err := newChatLogStream(dir, "sess-none", "", "claude", "v1", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	s1.HandleEvent(history[0])
	s1.Close()
	live, _ := os.ReadFile(s1.MDPath())
	if !strings.Contains(string(live), "> [image.png #1]") {
		t.Fatalf("live stream missing placeholder:\n%s", live)
	}

	s2, err := newChatLogStream(dir, "sess-none", "", "claude", "v1", history, now)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := s2.imageMap[shot]; got != placeholderPrefix+"[image.png #1]" {
		t.Errorf("resumed imageMap = %q; want placeholder #1", got)
	}
	if err := s2.SetTitle("renamed", history); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(s2.MDPath())
	if !strings.Contains(string(again), "> [image.png #1]") {
		t.Errorf("full rewrite lost the placeholder:\n%s", again)
	}
}
