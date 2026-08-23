package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Chat exports are filed one directory per month:
//
//	agent-chats/
//	  index.html                     ← landing page, always at the root
//	  assets/viewer.css, viewer.js   ← agent-chat-owned, one copy for the archive
//	  2026-08/
//	    15-01-some-title.md
//	    assets/2026-08-15-01-1-{sha12}.png
//
// A year of daily chats is then 12 directories instead of one flat pile of
// several hundred files.
//
// Two rules keep the split from breaking anything:
//
//  1. Attachment *basenames* keep their full `{YYYY-MM-DD}-{NN}-{n}-{sha12}`
//     form even though they now sit in a month directory. The link inside the
//     .md stays `./assets/{basename}`, so migrating an old export is a pure
//     `git mv` — no rewriting of committed markdown.
//  2. viewer.css / viewer.js stay in the root `assets/`. They are rewritten on
//     every export; a per-month copy would mean twelve churning duplicates a
//     year and stale copies rotting in old months. index.html sits beside
//     them, so its `./assets/viewer.css` link is unaffected.
//
// The flat layout is still read everywhere (scanChatExports returns both), so
// an archive that has not been migrated keeps working — see migrateChatLogs.

// monthDirRE matches a month directory name, "YYYY-MM".
var monthDirRE = regexp.MustCompile(`^\d{4}-\d{2}$`)

// mdMonthNameRE parses an exported chat filename inside a month directory:
// {DD}-{NN}-{slug}.md with NN 2 or 3 digits (matching nextDailyIndex).
var mdMonthNameRE = regexp.MustCompile(`^(\d{2})-(\d{2,3})-(.+)\.md$`)

// chatLogLayout selects where a NEW export is written. Reading is always
// layout-blind — scanChatExports returns both shapes no matter what this is set
// to — so an archive holding a mixture is listed, resumed and numbered
// correctly either way.
//
// The default is deliberately layoutFlat, and the rollout is staged: a release
// that only *understands* month directories goes out first, and the default
// flips to layoutMonth only once installed copies have caught up. Until then a
// version that wrote month directories would produce an archive that older
// copies cannot see -- and an older copy regenerating index.html drops every
// file it cannot see out of the listing.
type chatLogLayout int

const (
	// layoutFlat writes agent-chats/{YYYY-MM-DD}-{NN}-{slug}.md.
	layoutFlat chatLogLayout = iota
	// layoutMonth writes agent-chats/{YYYY-MM}/{DD}-{NN}-{slug}.md.
	layoutMonth
)

// chatLogLayoutSetting is resolved once at startup from -chatlog-layout /
// AGENT_CHAT_CHATLOG_LAYOUT (see parseChatLogLayout).
var chatLogLayoutSetting = layoutFlat

// parseChatLogLayout resolves the layout from the flag value (empty when the
// flag was not given) falling back to the env var, then to layoutFlat. An
// unrecognised value is an error rather than a silent default: writing chats
// somewhere other than intended is not something to discover months later.
func parseChatLogLayout(flagVal, envVal string) (chatLogLayout, error) {
	v := strings.TrimSpace(flagVal)
	if v == "" {
		v = strings.TrimSpace(envVal)
	}
	switch strings.ToLower(v) {
	case "", "flat":
		return layoutFlat, nil
	case "month":
		return layoutMonth, nil
	default:
		return layoutFlat, fmt.Errorf("unknown chat-log layout %q: want \"flat\" or \"month\"", v)
	}
}

// monthOf returns the "YYYY-MM" part of a "YYYY-MM-DD" date, or "" if date is
// not shaped like one. Callers treat "" as "keep it flat in the root", which
// is what makes a malformed date degrade to the old layout instead of creating
// a junk directory.
func monthOf(date string) string {
	if len(date) != len("2006-01-02") {
		return ""
	}
	month := date[:7]
	if !monthDirRE.MatchString(month) {
		return ""
	}
	return month
}

// dayOf returns the "DD" part of a "YYYY-MM-DD" date, or "" if date is not
// shaped like one.
func dayOf(date string) string {
	if monthOf(date) == "" {
		return ""
	}
	return date[8:]
}

// chatMonthDir is the directory an export for date belongs in: root/{YYYY-MM},
// falling back to root itself for a date that isn't one.
func chatMonthDir(root, date string) string {
	month := monthOf(date)
	if month == "" {
		return root
	}
	return filepath.Join(root, month)
}

// chatAssetsDir is where an export for date keeps its image attachments.
func chatAssetsDir(root, date string) string {
	return filepath.Join(chatMonthDir(root, date), "assets")
}

// exportAssetsDir is where one export's attachments live: always the `assets/`
// directory beside the .md itself. That is the same rule the `./assets/…`
// links inside the markdown follow, so it holds for a month-filed export and
// for a legacy flat one alike.
func exportAssetsDir(mdPath string) string {
	return filepath.Join(filepath.Dir(mdPath), "assets")
}

// viewerAssetsDir is where the archive's viewer.css / viewer.js live — always
// the root assets/, never a month directory (see the file comment).
func viewerAssetsDir(root string) string {
	return filepath.Join(root, "assets")
}

// chatMDName is the basename of an export inside its month directory:
// `{DD}-{NN}-{slug}.md`. A date that isn't one keeps the full flat name, so
// nothing can collide across months.
func chatMDName(date, idx, slug string) string {
	if day := dayOf(date); day != "" {
		return fmt.Sprintf("%s-%s-%s.md", day, idx, slug)
	}
	return fmt.Sprintf("%s-%s-%s.md", date, idx, slug)
}

// chatMDPath is the full path an export for (date, idx, slug) is written to.
func chatMDPath(root, date, idx, slug string) string {
	return filepath.Join(chatMonthDir(root, date), chatMDName(date, idx, slug))
}

// renamedMDPath is where set_chat_title moves an export to. It renames the
// file *in place* — a legacy flat export keeps its full-date basename and its
// place in the root, a month-filed one keeps its month directory. A retitle
// must never also relocate a file that may already be committed; migration is
// migrateChatLogs' job and nothing else's.
func renamedMDPath(oldPath, root, date, idx, slug string) string {
	dir := filepath.Dir(oldPath)
	if filepath.Clean(dir) == filepath.Clean(root) {
		return flatMDPath(dir, date, idx, slug)
	}
	return filepath.Join(dir, chatMDName(date, idx, slug))
}

// flatMDPath is the pre-month-layout location of an export:
// root/{YYYY-MM-DD}-{NN}-{slug}.md.
func flatMDPath(root, date, idx, slug string) string {
	return filepath.Join(root, fmt.Sprintf("%s-%s-%s.md", date, idx, slug))
}

// exportMDPath is where a NEW export goes, honouring chatLogLayoutSetting.
// Migration deliberately does not use it: migrateChatLogs always targets the
// month layout, because moving files there is the whole point of running it.
func exportMDPath(root, date, idx, slug string) string {
	if chatLogLayoutSetting == layoutMonth {
		return chatMDPath(root, date, idx, slug)
	}
	return flatMDPath(root, date, idx, slug)
}

// chatAssetPrefix is the shared basename prefix of every attachment belonging
// to one export. It keeps the full date even inside a month directory, so the
// `./assets/…` links in already-committed markdown survive migration untouched.
func chatAssetPrefix(date, idx string) string {
	return date + "-" + idx + "-"
}

// chatExportFile is one exported chat found on disk, in either layout.
type chatExportFile struct {
	Path   string // absolute path to the .md
	Rel    string // root-relative, slash-separated: "2026-08/15-01-x.md"
	Date   string // "YYYY-MM-DD"
	Index  string // "01" … "999"
	Slug   string
	Legacy bool // still sitting flat in the root, awaiting migration
}

// scanChatExports returns every export under root in an unspecified order,
// reading both layouts: `{YYYY-MM}/{DD}-{NN}-{slug}.md` and the legacy flat
// `{YYYY-MM-DD}-{NN}-{slug}.md`. Anything that doesn't match either shape (a
// hand-written notes.md, index.html, the assets directories) is skipped. An
// unreadable root returns nil — every caller treats that as "no exports".
func scanChatExports(root string) []chatExportFile {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []chatExportFile
	for _, de := range entries {
		if de.IsDir() {
			if !monthDirRE.MatchString(de.Name()) {
				continue
			}
			out = append(out, scanMonthDir(root, de.Name())...)
			continue
		}
		m := mdExportNameRE.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		out = append(out, chatExportFile{
			Path:   filepath.Join(root, de.Name()),
			Rel:    de.Name(),
			Date:   m[1],
			Index:  m[2],
			Slug:   m[3],
			Legacy: true,
		})
	}
	return out
}

// scanMonthDir returns the exports inside root/month.
func scanMonthDir(root, month string) []chatExportFile {
	entries, err := os.ReadDir(filepath.Join(root, month))
	if err != nil {
		return nil
	}
	var out []chatExportFile
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		m := mdMonthNameRE.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		out = append(out, chatExportFile{
			Path:  filepath.Join(root, month, de.Name()),
			Rel:   month + "/" + de.Name(),
			Date:  month + "-" + m[1],
			Index: m[2],
			Slug:  m[3],
		})
	}
	return out
}

// manifestRef renders an export's root-relative path the way index.html's
// MANIFEST spells it: "./2026-08/15-01-x.md". A path outside root falls back to
// its basename rather than emitting a "../" link the viewer could not fetch.
func manifestRef(root, mdPath string) string {
	rel, err := filepath.Rel(root, mdPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "./" + filepath.Base(mdPath)
	}
	return "./" + filepath.ToSlash(rel)
}
