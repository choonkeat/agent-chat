package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The `agent-chat migrate-chatlogs` subcommand moves a flat chat archive into
// the per-month layout described in chatlogpaths.go. It lives in the server
// binary rather than a separate tool because every machine that writes an
// archive already has this binary — there is nothing extra to distribute, and
// the mover can never drift from the writer.
//
// It is deliberately a *proposal* by default: it prints the `git mv` lines and
// exits. `-apply` runs them (through git, so history follows the file; plain
// rename for anything untracked) and regenerates index.html.

// legacyAssetNameRE matches an attachment basename left over from the flat
// layout: `{YYYY-MM-DD}-{NN}-…`. viewer.css / viewer.js never match, which is
// what keeps them in the root assets/ where index.html expects them.
var legacyAssetNameRE = regexp.MustCompile(`^((\d{4}-\d{2})-\d{2})-\d{2,3}-`)

// chatLogMove is one file the migration wants to relocate.
type chatLogMove struct {
	From string // absolute
	To   string // absolute
}

// mdAssetRefRE matches a markdown link into the sibling assets directory —
// `](./assets/name.png)` or `](assets/name.png)` — which is how both exported
// attachments and any hand-added screenshot are referenced.
var mdAssetRefRE = regexp.MustCompile(`\]\((?:\./)?assets/([^)\s]+)\)`)

// planChatLogMigration returns the moves that would bring root into the
// per-month layout, plus any warnings about files it deliberately left alone.
//
// Every legacy flat export goes to its `{YYYY-MM}/` directory, and the root
// `assets/` follows: an attachment moves to the month that references it.
// Basenames are preserved exactly and no file content is touched, so the
// `./assets/…` links inside already-committed markdown keep resolving.
func planChatLogMigration(root string) ([]chatLogMove, []string) {
	var moves []chatLogMove
	owners := map[string]map[string]bool{} // asset basename → months referencing it
	for _, ex := range scanChatExports(root) {
		if !ex.Legacy {
			continue
		}
		dst := chatMDPath(root, ex.Date, ex.Index, ex.Slug)
		if dst == ex.Path {
			continue // unparseable date: nowhere better to put it
		}
		moves = append(moves, chatLogMove{From: ex.Path, To: dst})
		for _, name := range referencedAssets(ex.Path) {
			claimAsset(owners, name, monthOf(ex.Date))
		}
	}
	assetMoves, warnings := planAssetMoves(root, owners)
	return append(moves, assetMoves...), warnings
}

// referencedAssets returns the assets/ basenames an export links to. Reading
// the markdown — rather than trusting the `{date}-{NN}-` naming convention —
// is what carries a hand-added screenshot along with the chat that shows it.
func referencedAssets(mdPath string) []string {
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range mdAssetRefRE.FindAllStringSubmatch(string(data), -1) {
		if name := path.Base(m[1]); name != "." && name != "/" {
			out = append(out, name)
		}
	}
	return out
}

func claimAsset(owners map[string]map[string]bool, name, month string) {
	if month == "" {
		return
	}
	if owners[name] == nil {
		owners[name] = map[string]bool{}
	}
	owners[name][month] = true
}

// planAssetMoves routes the root assets/ directory. An attachment claimed by
// exactly one month goes there; one claimed by several is left where it is and
// reported, since moving it would break every chat but one. An unclaimed file
// with a `{YYYY-MM-DD}-{NN}-` name still goes to its own month — that is an
// orphan whose chat was deleted by chatlog_optout. Anything else (viewer.css,
// viewer.js, a stray file nothing links to) stays at the root.
func planAssetMoves(root string, owners map[string]map[string]bool) ([]chatLogMove, []string) {
	assets := viewerAssetsDir(root)
	entries, err := os.ReadDir(assets)
	if err != nil {
		return nil, nil
	}
	var moves []chatLogMove
	var warnings []string
	for _, de := range entries {
		if de.IsDir() || de.Name() == "viewer.css" || de.Name() == "viewer.js" {
			continue
		}
		months := sortedKeys(owners[de.Name()])
		if len(months) > 1 {
			warnings = append(warnings, fmt.Sprintf(
				"assets/%s is referenced from %s — left in the archive root; move or copy it by hand",
				de.Name(), strings.Join(months, " and ")))
			continue
		}
		month := ""
		if len(months) == 1 {
			month = months[0]
		} else if m := legacyAssetNameRE.FindStringSubmatch(de.Name()); m != nil {
			month = m[2]
		}
		if month == "" {
			continue
		}
		moves = append(moves, chatLogMove{
			From: filepath.Join(assets, de.Name()),
			To:   filepath.Join(root, month, "assets", de.Name()),
		})
	}
	return moves, warnings
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// runMigrateChatLogs is the subcommand entry point; it returns the process
// exit code.
func runMigrateChatLogs(argv []string) int {
	fs := flag.NewFlagSet("migrate-chatlogs", flag.ExitOnError)
	dir := fs.String("dir", "agent-chats", "chat archive directory to migrate")
	apply := fs.Bool("apply", false, "perform the moves (default: print them and exit)")
	indexOnly := fs.Bool("index-only", false, "move nothing; just regenerate index.html from the chats on disk")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: agent-chat migrate-chatlogs [-dir agent-chats] [-apply]\n\n"+
			"Move a flat chat archive into per-month directories:\n"+
			"  agent-chats/2026-08-15-01-title.md  →  agent-chats/2026-08/15-01-title.md\n"+
			"  agent-chats/assets/2026-08-15-01-1-{sha}.png  →  agent-chats/2026-08/assets/…\n\n"+
			"index.html and assets/viewer.{css,js} stay in the archive root.\n"+
			"Without -apply nothing is touched; the git mv commands are printed.\n"+
			"-index-only moves nothing and rebuilds index.html from what is on disk,\n"+
			"which also heals an index.html mangled by a merge conflict.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	root, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-chat: %v\n", err)
		return 1
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "agent-chat: %s is not a directory\n", root)
		return 1
	}

	if *indexOnly {
		if err := regenerateIndexHTML(root); err != nil {
			fmt.Fprintf(os.Stderr, "agent-chat: regenerate index.html: %v\n", err)
			return 1
		}
		fmt.Printf("%s/index.html regenerated from the chats on disk.\n", *dir)
		return 0
	}

	moves, warnings := planChatLogMigration(root)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if len(moves) == 0 {
		fmt.Printf("Nothing to migrate: %s is already filed by month.\n", *dir)
		return 0
	}

	cwd, _ := os.Getwd()
	if !*apply {
		fmt.Printf("# %d file(s) to move — re-run with -apply to do it:\n", len(moves))
		for _, mv := range moves {
			fmt.Printf("git mv %s %s\n", shellPath(cwd, mv.From), shellPath(cwd, mv.To))
		}
		return 0
	}

	for _, mv := range moves {
		if err := os.MkdirAll(filepath.Dir(mv.To), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "agent-chat: %v\n", err)
			return 1
		}
		if err := moveFile(mv.From, mv.To); err != nil {
			fmt.Fprintf(os.Stderr, "agent-chat: %v\n", err)
			return 1
		}
		fmt.Printf("moved %s → %s\n", shellPath(cwd, mv.From), shellPath(cwd, mv.To))
	}
	if err := regenerateIndexHTML(root); err != nil {
		fmt.Fprintf(os.Stderr, "agent-chat: regenerate index.html: %v\n", err)
		return 1
	}
	fmt.Printf("\n%d file(s) moved; %s/index.html regenerated. Review `git status`, then commit.\n", len(moves), *dir)
	return 0
}

// moveFile relocates one file with `git mv` when git tracks it (so the rename
// is staged and history follows), and a plain rename otherwise — an untracked
// file is one `git mv` would refuse.
func moveFile(from, to string) error {
	if gitTracks(from) {
		cmd := exec.Command("git", "mv", from, to)
		cmd.Dir = filepath.Dir(from)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git mv %s: %v: %s", filepath.Base(from), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return os.Rename(from, to)
}

// gitTracks reports whether git has from in its index.
func gitTracks(from string) bool {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", "--", filepath.Base(from))
	cmd.Dir = filepath.Dir(from)
	return cmd.Run() == nil
}

// shellPath renders an absolute path relative to cwd when that is shorter and
// stays inside it, so the printed commands are copy-pasteable.
func shellPath(cwd, abs string) string {
	if cwd == "" {
		return abs
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

// legacyChatExportCount counts the exports still sitting flat in root. It is
// the nudge chatlog_close uses to mention the migration at the one moment the
// user is already looking at the archive with a commit in mind.
func legacyChatExportCount(root string) int {
	n := 0
	for _, ex := range scanChatExports(root) {
		if ex.Legacy {
			n++
		}
	}
	return n
}
