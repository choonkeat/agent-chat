package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// The "What's new" panel reads the CHANGELOG built into this binary, not the
// one on GitHub: it works offline, always describes the version actually
// running, and needs no cross-origin fetch from inside swe-swe's iframe.
//
//go:embed CHANGELOG.md
var changelogMD string

// whatsNewReleases is how many releases the panel shows; the rest are one
// "Full changelog" link away.
const whatsNewReleases = 3

var releaseHeading = regexp.MustCompile(`(?m)^## \[([^\]]+)\]`)

// latestChangelogVersion is the version of the newest release in the embedded
// CHANGELOG ("0.20.0"), or "" when it has none. The unread dot compares this —
// not the binary's -ldflags version — so a dev build still has a version, and
// the dot never lights up for a release with nothing written about it.
func latestChangelogVersion() string {
	return latestVersionIn(changelogMD)
}

func latestVersionIn(md string) string {
	m := releaseHeading.FindStringSubmatch(md)
	if m == nil {
		return ""
	}
	return m[1]
}

// recentReleases returns the first n release sections of md, headings
// included, without the file's title and preamble.
func recentReleases(md string, n int) string {
	idx := releaseHeading.FindAllStringIndex(md, n+1)
	if len(idx) == 0 {
		return ""
	}
	end := len(md)
	if len(idx) > n {
		end = idx[n][0]
	}
	return unwrapLines(strings.TrimSpace(md[idx[0][0]:end])) + "\n"
}

var blockStart = regexp.MustCompile(`^\s*(#|[-*+] |\d+[.)] |>|\||` + "```" + `)`)

// unwrapLines joins the CHANGELOG's hard-wrapped lines back into one line per
// paragraph or list item. The chat's markdown keeps single newlines as line
// breaks (right for chat messages), which in a narrow panel turns ~75-column
// source wrapping into ragged half-lines and splits code spans in two. Lines
// inside code fences, and lines that start a block of their own, are kept.
func unwrapLines(md string) string {
	var out []string
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		n := len(out)
		joinable := !inFence && trimmed != "" && n > 0 && !blockStart.MatchString(line)
		if joinable {
			prev := strings.TrimSpace(out[n-1])
			if prev != "" && !strings.HasPrefix(prev, "#") && !strings.HasPrefix(prev, "```") {
				out[n-1] = strings.TrimRight(out[n-1], " ") + " " + trimmed
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func handleChangelog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"latest":   latestChangelogVersion(),
		"markdown": recentReleases(changelogMD, whatsNewReleases),
	})
}
