package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newPathsWorkspace builds a throwaway workspace whose shape mirrors the parts
// of this repo that show up in real chat text, and returns its symlink-resolved
// root (macOS /var -> /private/var would otherwise fail the containment check).
func newPathsWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	dirs := []string{
		"client-dist",
		"docs/adr",
		"npm-platforms/linux-x64/bin",
		"agent-chats",
		"e2e",
		"prompts",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	files := []string{
		"client-dist/app.js",
		"client-dist/style.css",
		"agent-chats/index.html",
		"npm-platforms/linux-x64/bin/agent-chat",
		"prompts/agent-reply.tmpl",
		"e2e/fork-button.spec.cjs",
		"main.go",
		"Makefile",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	return root
}

// paths flattens a result to "path" / "path(dir)" strings for terse assertions.
func paths(got []FilePath) []string {
	out := make([]string, 0, len(got))
	for _, p := range got {
		if p.Dir {
			out = append(out, p.Path+"(dir)")
			continue
		}
		out = append(out, p.Path)
	}
	return out
}

func TestExtractWorkspacePaths_FindsRealPaths(t *testing.T) {
	root := newPathsWorkspace(t)

	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "bare relative file",
			text: "the rule lives in client-dist/app.js today",
			want: []string{"client-dist/app.js"},
		},
		{
			name: "directory gets the dir flag",
			text: "see docs/adr for the writeups",
			want: []string{"docs/adr(dir)"},
		},
		{
			name: "trailing slash already present",
			text: "see docs/adr/ for the writeups",
			want: []string{"docs/adr(dir)"},
		},
		{
			name: "leading @ is stripped, not required",
			text: "look at @client-dist/app.js",
			want: []string{"client-dist/app.js"},
		},
		{
			name: "@ with no slash still qualifies",
			text: "run @Makefile",
			want: []string{"Makefile"},
		},
		{
			name: "bare filename with no slash and no @ is ignored",
			text: "run Makefile and main.go",
			want: nil,
		},
		{
			name: "trailing sentence punctuation is trimmed",
			text: "it is in client-dist/app.js, next to client-dist/style.css.",
			want: []string{"client-dist/app.js", "client-dist/style.css"},
		},
		{
			name: "deep path",
			text: "npm-platforms/linux-x64/bin/agent-chat is the binary",
			want: []string{"npm-platforms/linux-x64/bin/agent-chat"},
		},
		{
			name: "several on one line, first-seen order, deduplicated",
			text: "client-dist/app.js then docs/adr then client-dist/app.js again",
			want: []string{"client-dist/app.js", "docs/adr(dir)"},
		},
		{
			name: "inside a markdown link target",
			text: "see [the app](client-dist/app.js) for details",
			want: []string{"client-dist/app.js"},
		},
		{
			// Extraction is markdown-blind on purpose: the client decides what
			// to do with a backticked path, and it can only do that if the
			// path is in the list to begin with.
			name: "inside a code span",
			text: "see `client-dist/app.js` for details",
			want: []string{"client-dist/app.js"},
		},
		{
			name: "inside a fenced code block",
			text: "```\nclient-dist/app.js\n```",
			want: []string{"client-dist/app.js"},
		},
		{
			name: "leading ./ is normalised away",
			text: "./client-dist/app.js is the file",
			want: []string{"client-dist/app.js"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := paths(extractWorkspacePaths(tc.text, root))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("extractWorkspacePaths(%q)\n got %v\nwant %v", tc.text, got, tc.want)
			}
		})
	}
}

// Raw is the substring the client searches for, so it must be exactly what is
// in the text minus trailing sentence punctuation — never the display form.
func TestExtractWorkspacePaths_RawIsTheSearchKey(t *testing.T) {
	root := newPathsWorkspace(t)

	tests := []struct {
		text string
		raw  string
		path string
	}{
		{"see client-dist/app.js here", "client-dist/app.js", "client-dist/app.js"},
		{"see @client-dist/app.js here", "@client-dist/app.js", "client-dist/app.js"},
		{"see ./client-dist/app.js here", "./client-dist/app.js", "client-dist/app.js"},
		{"ends the sentence at client-dist/app.js.", "client-dist/app.js", "client-dist/app.js"},
		{"the docs/adr/ directory", "docs/adr/", "docs/adr"},
	}

	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			got := extractWorkspacePaths(tc.text, root)
			if len(got) != 1 {
				t.Fatalf("got %d results (%v), want 1", len(got), paths(got))
			}
			if got[0].Raw != tc.raw {
				t.Errorf("Raw = %q, want %q", got[0].Raw, tc.raw)
			}
			if got[0].Path != tc.path {
				t.Errorf("Path = %q, want %q", got[0].Path, tc.path)
			}
			if !strings.Contains(tc.text, got[0].Raw) {
				t.Errorf("Raw %q is not a substring of the text", got[0].Raw)
			}
		})
	}
}

// The 157-of-188 rejection rate measured against agent-chats/ is the whole
// reason this feature is viable; every string here is one that really occurs.
func TestExtractWorkspacePaths_RejectsNonPaths(t *testing.T) {
	root := newPathsWorkspace(t)

	rejects := []string{
		"press Ctrl/Cmd to open it",
		"rebase onto origin/main first",
		"suite is 7/7 green",
		"either and/or both",
		"the bump/publish flow",
		"an Allow/Deny prompt",
		"x64/arm64 builds",
		"whichever of you/the agent gets there",
		"published as @choonkeat/agent-chat",
		"a ping/pong keepalive",
		"hold Shift/Alt while clicking",
		"see https://github.com/choonkeat/agent-chat for source",
		"http://localhost:4001/ws?cursor=3 is the socket",
		"file:///etc/passwd is not ours",
		"//cdn.example.com/lib.js protocol-relative",
		"client-dist/nope.js was deleted",
		"docs/adr/2099-01-01-not-written-yet.md",
	}

	for _, text := range rejects {
		t.Run(text, func(t *testing.T) {
			if got := extractWorkspacePaths(text, root); len(got) != 0 {
				t.Errorf("extractWorkspacePaths(%q) = %v, want none", text, paths(got))
			}
		})
	}
}

// Statting attacker-supplied strings is only a stat, but anything that resolves
// outside the workspace must never reach the client: the embedder's Files pane
// is rooted at the workspace and cannot serve it.
func TestExtractWorkspacePaths_ConfinedToWorkspace(t *testing.T) {
	root := newPathsWorkspace(t)

	outside := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(outside); err == nil {
		outside = resolved
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	t.Run("absolute path under the root is kept, relative to it", func(t *testing.T) {
		text := "open " + filepath.Join(root, "client-dist/app.js")
		want := []string{"client-dist/app.js"}
		if got := paths(extractWorkspacePaths(text, root)); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("absolute path outside the root is dropped", func(t *testing.T) {
		text := "open " + filepath.Join(outside, "secret.txt")
		if got := extractWorkspacePaths(text, root); len(got) != 0 {
			t.Errorf("got %v, want none", paths(got))
		}
	})

	t.Run("real system paths are dropped", func(t *testing.T) {
		// /etc and /proc both occur in the real archive and both exist.
		if runtime.GOOS == "windows" {
			t.Skip("posix paths")
		}
		if got := extractWorkspacePaths("check /etc and /proc", root); len(got) != 0 {
			t.Errorf("got %v, want none", paths(got))
		}
	})

	t.Run("dot-dot traversal out of the root is dropped", func(t *testing.T) {
		text := "open ../" + filepath.Base(outside) + "/secret.txt"
		if got := extractWorkspacePaths(text, root); len(got) != 0 {
			t.Errorf("got %v, want none", paths(got))
		}
	})

	t.Run("dot-dot that stays inside the root is kept", func(t *testing.T) {
		text := "open docs/../client-dist/app.js"
		want := []string{"client-dist/app.js"}
		if got := paths(extractWorkspacePaths(text, root)); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("symlink pointing out of the root is dropped", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need privileges on windows")
		}
		if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}
		if got := extractWorkspacePaths("open escape/secret.txt", root); len(got) != 0 {
			t.Errorf("got %v, want none", paths(got))
		}
	})

	t.Run("the root itself is not a link", func(t *testing.T) {
		if got := extractWorkspacePaths("everything under ./ is ours", root); len(got) != 0 {
			t.Errorf("got %v, want none", paths(got))
		}
	})
}

// A pathological message must not turn into an unbounded syscall run.
func TestExtractWorkspacePaths_CandidateCap(t *testing.T) {
	root := newPathsWorkspace(t)

	var b strings.Builder
	for i := 0; i < workspacePathCandidateCap*3; i++ {
		b.WriteString("client-dist/app.js x/y ")
	}
	got := extractWorkspacePaths(b.String(), root)
	if len(got) > workspacePathCandidateCap {
		t.Errorf("returned %d paths, cap is %d", len(got), workspacePathCandidateCap)
	}
	if len(got) != 1 {
		t.Errorf("dedup should collapse to 1 unique path, got %v", paths(got))
	}
}

func TestExtractWorkspacePaths_EmptyRootDisablesFeature(t *testing.T) {
	if got := extractWorkspacePaths("client-dist/app.js", ""); len(got) != 0 {
		t.Errorf("got %v, want none when root is empty", paths(got))
	}
}
