package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// assetMode decides what happens to a turn's attachments when a chat log is
// exported (streaming or export_chat_md). Screenshots dominate archive size —
// full-resolution phone PNGs run 0.5–1.5 MB each and filled a git-LFS quota in
// months — so the default copies none of them.
//
//   - none:     images and binary files become `[name #N]` placeholders; small
//     text attachments (pasted long text) are still copied, since they are part
//     of the conversation itself.
//   - small:    PNG/JPEG images are downscaled to at most smallAssetMaxEdge on
//     the long edge and re-encoded (JPEG, or PNG when transparent); everything
//     else is copied as-is.
//   - original: every attachment is copied byte-for-byte.
type assetMode string

const (
	assetsNone     assetMode = "none"
	assetsSmall    assetMode = "small"
	assetsOriginal assetMode = "original"
)

// chatAssetMode is the boot-time default from AGENT_CHAT_EXPORT_ASSETS, used by
// the streaming export and by export_chat_md when its `assets` param is unset.
var chatAssetMode = assetsNone

// parseAssetMode maps AGENT_CHAT_EXPORT_ASSETS (or export_chat_md's `assets`)
// to a mode. Empty means none; an unknown value is a misconfiguration that
// falls back to none with ok=false so the caller can warn.
func parseAssetMode(s string) (mode assetMode, ok bool) {
	switch assetMode(strings.ToLower(strings.TrimSpace(s))) {
	case "", assetsNone:
		return assetsNone, true
	case assetsSmall:
		return assetsSmall, true
	case assetsOriginal:
		return assetsOriginal, true
	}
	return assetsNone, false
}

// assetModeFromEnv reads AGENT_CHAT_EXPORT_ASSETS, logging an unknown value.
func assetModeFromEnv(v string) assetMode {
	mode, ok := parseAssetMode(v)
	if !ok {
		log.Printf("Warning: AGENT_CHAT_EXPORT_ASSETS=%q is not one of none|small|original — using none", v)
	}
	return mode
}

// placeholderPrefix marks an imageMap value as placeholder text instead of a
// ./assets/ URL. imageMap is threaded through the batch exporter, the streaming
// writer and its resume path, so carrying placeholders in it keeps all three
// rendering the same turn identically.
const placeholderPrefix = "placeholder:"

// assetPlaceholder is the imageMap value for an attachment that was not copied.
// The ordinal shares the asset counter, so `#N` is stable across a streaming
// resume and pasted screenshots all named image.png stay distinguishable.
func assetPlaceholder(f FileRef, n int) string {
	name := strings.NewReplacer("[", "", "]", "").Replace(f.Name)
	if name == "" {
		name = "attachment"
	}
	return fmt.Sprintf("%s[%s #%d]", placeholderPrefix, name, n)
}

// keepsAsset reports whether mode copies f into assets/ at all (in some form).
func keepsAsset(mode assetMode, f FileRef) bool {
	if mode == assetsNone {
		return isTextAttachment(f)
	}
	return true
}

// isTextAttachment is true for plain-text files (e.g. pasted long text turned
// into an attachment) — small, and part of the conversation, so even mode none
// archives them.
func isTextAttachment(f FileRef) bool {
	t := strings.ToLower(f.Type)
	if strings.HasPrefix(t, "text/") {
		return true
	}
	switch t {
	case "application/json", "application/xml", "application/yaml", "application/x-yaml":
		return true
	}
	switch strings.ToLower(filepath.Ext(f.Name)) {
	case ".txt", ".md", ".log", ".json", ".csv", ".tsv", ".yaml", ".yml", ".xml", ".diff", ".patch":
		return true
	}
	return false
}

// smallAssetMaxEdge caps the long edge of an image exported in mode small:
// readable when clicked through, and roughly 100 KB as JPEG.
const smallAssetMaxEdge = 1280

// shrinkImage returns a smaller encoding of the PNG/JPEG at src and the
// extension for it, or ok=false when the original should be copied instead:
// an undecodable or other format (GIF animation, WebP, SVG, HEIC), or a
// re-encode that would not actually be smaller.
func shrinkImage(src string) (data []byte, ext string, ok bool) {
	raw, err := os.ReadFile(src)
	if err != nil {
		return nil, "", false
	}
	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, "", false
	}
	img = downscale(img, smallAssetMaxEdge)
	var buf bytes.Buffer
	if isOpaque(img) {
		err, ext = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}), ".jpg"
	} else {
		err, ext = png.Encode(&buf, img), ".png"
	}
	if err != nil || buf.Len() >= len(raw) {
		return nil, "", false
	}
	return buf.Bytes(), ext, true
}

// isOpaque reports whether img has no transparent pixels, so JPEG (which has
// no alpha) can encode it without turning transparent areas black.
func isOpaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}

// downscale shrinks img so its long edge is at most maxEdge, averaging each
// destination pixel over the source box it covers (screenshots are text, which
// nearest-neighbour would shred). Images already small enough are returned as-is.
func downscale(img image.Image, maxEdge int) image.Image {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	long := max(sw, sh)
	if long <= maxEdge {
		return img
	}
	dw := max(1, sw*maxEdge/long)
	dh := max(1, sh*maxEdge/long)
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		y0, y1 := b.Min.Y+y*sh/dh, b.Min.Y+(y+1)*sh/dh
		for x := 0; x < dw; x++ {
			x0, x1 := b.Min.X+x*sw/dw, b.Min.X+(x+1)*sw/dw
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(img.At(sx, sy)).(color.NRGBA)
					r += uint64(c.R)
					g += uint64(c.G)
					bl += uint64(c.B)
					a += uint64(c.A)
					n++
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{uint8(r / n), uint8(g / n), uint8(bl / n), uint8(a / n)})
		}
	}
	return dst
}

// writeBytesSum writes data to dst (via a temp file and rename, like
// copyFileSum) and returns the same short sha256 hex digest.
func writeBytesSum(data []byte, dst string) (string, error) {
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12], nil
}

// shrinkForMode returns the downscaled bytes to write for f in mode small, or
// ok=false when f should be copied byte-for-byte.
func shrinkForMode(mode assetMode, f FileRef) (data []byte, ext string, ok bool) {
	if mode != assetsSmall || !isImage(f) {
		return nil, "", false
	}
	return shrinkImage(f.Path)
}
