package main

// uploadlimit.go -- per-file size cap for the paperclip / paste upload.
//
// AGENT_CHAT_UPLOAD_MAX_MB caps a single uploaded file. Empty, 0, or an
// unparseable value means no limit. swe-swe sets it from its own
// SWE_SWE_UPLOAD_MAX_MB, so a host's terminal and chat uploads share one cap.
// The value is also inlined into the page (UPLOAD_MAX_BYTES) so a too-large
// file is refused before any bytes are sent.

import (
	"fmt"
	"strconv"
	"strings"
)

const uploadMaxEnvKey = "AGENT_CHAT_UPLOAD_MAX_MB"

// uploadMaxBytes is the active cap; 0 = no limit. Set once at startup.
var uploadMaxBytes int64

// parseUploadMaxBytes converts a megabyte count to bytes. "" and "0" mean no
// limit (0). Decimals are allowed ("0.5").
func parseUploadMaxBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	mb, err := strconv.ParseFloat(s, 64)
	if err != nil || mb < 0 || mb != mb {
		return 0, fmt.Errorf("%s=%q is not a number of megabytes", uploadMaxEnvKey, s)
	}
	return int64(mb * 1024 * 1024), nil
}

// formatUploadSize renders a byte count like the client does ("65.2 MB").
func formatUploadSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
	}
}

// uploadTooLargeText is the refusal shown on the file chip.
func uploadTooLargeText(size, max int64) string {
	if size > 0 {
		return fmt.Sprintf("too large (%s); limit is %s", formatUploadSize(size), formatUploadSize(max))
	}
	return fmt.Sprintf("too large; limit is %s", formatUploadSize(max))
}
