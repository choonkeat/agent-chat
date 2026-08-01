package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
)

// FilePath is one run of text that names a file or directory really existing
// under the workspace root.
//
// Raw is the exact substring to find in the message text — the search key, not
// the display text. The client composes what it shows from Path and Dir
// instead ("@" + Path, plus a trailing "/" for a directory), so `./docs/adr`,
// `docs/adr/` and `@docs/adr` all render identically as `@docs/adr/`.
type FilePath struct {
	Raw  string `json:"raw"`
	Path string `json:"path"`
	Dir  bool   `json:"dir,omitempty"`
}

// workspacePathRoot is the absolute, symlink-resolved directory paths are
// resolved against — set once at startup from workspaceRootPath(). Empty
// disables the whole feature, which is the default in tests and the fallback
// when the working directory cannot be resolved.
var workspacePathRoot string

// workspacePathsEnabled is off until a browser proves there is somewhere for a
// link to go, by connecting with files=1 (which it sends only when the embedder
// gave it files_url). Standalone agent-chat therefore never runs the extractor
// at all: with no Files pane, an autolink would point nowhere.
//
// Set by EventBus.EnableFilePaths, which documents the latching rule and why
// bubbles written before the switch are left alone.
var workspacePathsEnabled atomic.Bool

// annotateFilePaths returns event with FilePaths filled in from its Text.
//
// It is deliberately called on the copy kept in the in-memory event log and
// broadcast to browsers, never on the copy handed to writeToLog: the JSONL
// archive records what was said, and a stored answer would be wrong the moment
// a file moved.
func annotateFilePaths(event Event) Event {
	if !workspacePathsEnabled.Load() || workspacePathRoot == "" || event.Text == "" {
		return event
	}
	event.FilePaths = extractWorkspacePaths(event.Text, workspacePathRoot)
	return event
}

// workspacePathCandidateCap bounds how many candidate tokens one message may
// have checked. Measured against this repo's agent-chats/ archive, a bubble
// averages 0.5 candidates; the cap only exists so a pathological message can't
// turn into an unbounded run of syscalls.
var workspacePathCandidateCap = 200

// workspacePathToken matches a run of path-shaped characters. ':' is
// deliberately absent, so a URL splits at its scheme and the remainder starts
// with "//" — which pathTokenCandidate rejects outright.
var workspacePathToken = regexp.MustCompile(`[A-Za-z0-9_.@~/-]+`)

// extractWorkspacePaths returns, in first-seen order and deduplicated by Raw,
// the tokens in text that name something existing under root. root must be an
// absolute, symlink-resolved directory; an empty root disables the feature.
//
// Existence is the whole filter: of 188 unique path-shaped tokens in this
// repo's own chat archive only 31 exist, and the 157 rejects (Ctrl/Cmd,
// origin/main, 7/7, and/or, @choonkeat/agent-chat) are indistinguishable from
// real paths by shape alone.
func extractWorkspacePaths(text, root string) []FilePath {
	if root == "" || text == "" {
		return nil
	}

	var out []FilePath
	seen := make(map[string]bool)
	checked := 0

	for _, raw := range workspacePathToken.FindAllString(text, -1) {
		if checked >= workspacePathCandidateCap {
			break
		}
		token, ok := pathTokenCandidate(raw)
		if !ok || seen[token] {
			continue
		}
		seen[token] = true
		checked++
		if p, ok := resolveWorkspacePath(token, root); ok {
			out = append(out, p)
		}
	}
	return out
}

// pathTokenCandidate trims a matched run down to the substring worth checking
// and reports whether it qualifies at all: it must contain a '/' or lead with
// '@' (the character the filepath autocompleter leaves in the sent text).
func pathTokenCandidate(raw string) (string, bool) {
	// Sentence punctuation that the character class swallowed: "origin/main."
	// and "app.js--" are the path plus trailing noise, never a real name.
	token := strings.TrimRight(raw, ".-~@")
	if token == "" {
		return "", false
	}
	// "//host/path" is what's left of an http:// or file:// URL once the scheme
	// splits off, and a protocol-relative URL looks the same.
	if strings.HasPrefix(token, "//") {
		return "", false
	}
	if !strings.Contains(token, "/") && !strings.HasPrefix(token, "@") {
		return "", false
	}
	return token, true
}

// resolveWorkspacePath stats a candidate and returns it workspace-relative,
// dropping anything that resolves outside root. The containment check runs
// twice: once lexically, before any syscall (so "/etc" costs nothing), and
// again on the symlink-resolved path (so a link pointing out of the workspace
// can't smuggle a file the Files pane could never serve).
func resolveWorkspacePath(token, root string) (FilePath, bool) {
	clean := strings.TrimPrefix(token, "@")
	if clean == "" {
		return FilePath{}, false
	}

	abs := clean
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)

	rel, ok := workspaceRelative(abs, root)
	if !ok {
		return FilePath{}, false
	}

	fi, err := os.Stat(abs)
	if err != nil {
		return FilePath{}, false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return FilePath{}, false
	}
	if _, ok := workspaceRelative(resolved, root); !ok {
		return FilePath{}, false
	}

	return FilePath{Raw: token, Path: rel, Dir: fi.IsDir()}, true
}

// workspaceRelative reports whether abs sits strictly under root, and its path
// relative to it. The root itself returns false: linking a bubble's "./" to the
// workspace root is noise, not navigation.
func workspaceRelative(abs, root string) (string, bool) {
	// The trailing separator makes the prefix stop at a path boundary, so a
	// workspace at /worktrees/try123 can't claim /worktrees/try123-old.
	prefix := root + string(filepath.Separator)
	if !strings.HasPrefix(abs, prefix) {
		return "", false
	}
	rel := abs[len(prefix):]
	if rel == "" {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
