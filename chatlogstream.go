package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// chatLogStream appends each renderable chat event to a markdown export file
// the moment it is published — the curated twin of the JSONL event log. The
// file is created up front under a provisional `{date}-{NN}-untitled.md` name
// (NN claimed at creation) with the header already written; every
// userMessage/agentMessage/verbalReply event then appends one bubble, copying
// the event's attachments into assets/ at that same moment, while the upload
// files still exist. All other event types are ignored. Rendering goes through
// the same renderChatBubble fold as the batch exporter, so the on-disk file is
// always byte-identical to a batch render of the events so far.
// chatStream is the process-wide streaming exporter. It stays nil unless
// AGENT_CHAT_EXPORT_DIR enabled the feature at boot (main.go); tool handlers
// must treat nil as "feature off".
var chatStream *chatLogStream

type chatLogStream struct {
	mu       sync.Mutex
	dir      string // export dir (absolute)
	mdPath   string // current file (provisional until titled)
	meta     chatExportMeta
	st       renderState       // renderer carry-state
	assetN   int               // per-file asset counter (shared numbering with the .md's references)
	imageMap map[string]string // upload path -> ./assets/... relative URL
	f        *os.File          // O_APPEND handle
	stopped  bool              // chatlog_optout

	indexDebounce time.Duration // turn-end debounce for index regeneration (0 = 2s default)
	indexTimer    *time.Timer   // pending debounced regeneration, if any
}

// chatLogSessionID derives the stable identity written to the `session:`
// header line. The JSONL event log path (AGENT_CHAT_EVENT_LOG) is per-session
// and survives restarts, so its hash identifies the session across restarts;
// without an event log there is no history to resume from anyway, so a random
// UUID suffices.
func chatLogSessionID(eventLogPath string) string {
	if eventLogPath == "" {
		return uuid.New().String()
	}
	abs, err := filepath.Abs(eventLogPath)
	if err != nil {
		abs = eventLogPath
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
}

// initChatLogStream is the boot-time entry point for the streaming export:
// exportDir is AGENT_CHAT_EXPORT_DIR verbatim ("" = feature off), resolved
// relative to cwd with the same cannot-escape-cwd rule as export_chat_md's
// target_dir — except an escaping path is a misconfiguration warning +
// feature off, not an error: a bad env var must never take the chat down.
func initChatLogStream(exportDir, cwd, sessionID, agent, version string, history []Event, now time.Time) (*chatLogStream, error) {
	if exportDir == "" {
		return nil, nil
	}
	dir := exportDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	dir = filepath.Clean(dir)
	cwdClean := filepath.Clean(cwd)
	rel, err := filepath.Rel(cwdClean, dir)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		log.Printf("agent-chat: fatal misconfiguration: AGENT_CHAT_EXPORT_DIR %q resolves outside the working directory %q — streaming chat-log export disabled", exportDir, cwdClean)
		return nil, nil
	}
	return newChatLogStream(dir, sessionID, agent, version, history, now)
}

// newChatLogStream claims the next daily NN in dir by creating the provisional
// .md file (O_EXCL, retrying on collision) and writes the export header,
// including a `session:` line so a restarted process can find this file again.
// If dir already contains an export whose `session:` header matches sessionID
// (this process was restarted or forked), that file is resumed instead of
// minting a new NN: the renderer fold state (lastTs, assetN, imageMap) is
// recovered by re-folding history — the same in-memory events the bus replays
// — never by parsing the markdown.
func newChatLogStream(dir, sessionID, agent, version string, history []Event, now time.Time) (*chatLogStream, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := ensureViewerAssets(filepath.Join(dir, "assets")); err != nil {
		return nil, err
	}

	if s, err := resumeChatLogStream(dir, sessionID, agent, version, history); s != nil || err != nil {
		return s, err
	}

	date := now.Format("2006-01-02")
	const slug = "untitled"
	idxNum := nextDailyIndex(dir, date)
	var (
		f      *os.File
		idx    string
		mdPath string
	)
	for {
		idx = fmt.Sprintf("%02d", idxNum)
		mdPath = filepath.Join(dir, fmt.Sprintf("%s-%s-%s.md", date, idx, slug))
		var err error
		f, err = os.OpenFile(mdPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create %s: %w", mdPath, err)
		}
		idxNum++ // another session claimed this NN between the scan and the create
	}

	meta := chatExportMeta{
		Title:   humanTitle(slug),
		Date:    date,
		Index:   idx,
		Slug:    slug,
		Session: sessionID,
		Agent:   agent,
		Version: version,
	}
	if _, err := f.WriteString(renderChatMarkdown(nil, meta, nil)); err != nil {
		f.Close()
		return nil, fmt.Errorf("write header %s: %w", mdPath, err)
	}
	f.Sync()

	s := &chatLogStream{
		dir:      dir,
		mdPath:   mdPath,
		meta:     meta,
		imageMap: map[string]string{},
		f:        f,
	}
	// Feature enabled mid-session (event log restored history but no export
	// file existed yet): stream the backlog into the fresh file now.
	// Attachments are best-effort — some upload sources may already be gone.
	for _, e := range history {
		s.HandleEvent(e)
	}
	return s, nil
}

// resumeChatLogStream scans dir for an export whose header `session:` line
// matches sessionID and, if found, reopens it for appending with the fold
// state recovered from history. Returns (nil, nil) when no file matches.
func resumeChatLogStream(dir, sessionID, agent, version string, history []Event) (*chatLogStream, error) {
	if sessionID == "" {
		return nil, nil
	}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		m := mdExportNameRE.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		header := readChatHeader(filepath.Join(dir, de.Name()))
		if header["session"] != sessionID {
			continue
		}
		mdPath := filepath.Join(dir, de.Name())
		f, err := os.OpenFile(mdPath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("reopen %s: %w", mdPath, err)
		}
		title := header["title"]
		if title == "" {
			title = humanTitle(m[3])
		}
		s := &chatLogStream{
			dir:    dir,
			mdPath: mdPath,
			meta: chatExportMeta{
				Title:   title,
				Date:    m[1],
				Index:   m[2],
				Slug:    m[3],
				Session: sessionID,
				Agent:   agent,
				Version: version,
			},
			imageMap: map[string]string{},
			f:        f,
		}
		s.recoverFromHistory(history)
		return s, nil
	}
	return nil, nil
}

// recoverFromHistory re-folds the in-memory event history to rebuild the
// renderer carry-state after a resume: lastTs via renderChatBubble, and
// assetN/imageMap by re-walking each event's attachments in order. The asset
// files themselves were copied when their events first streamed (their upload
// sources may be long gone), so each ordinal is matched back to the existing
// `{date}-{NN}-{n}-{sha12}…` file in assets/ instead of being recopied.
func (s *chatLogStream) recoverFromHistory(history []Event) {
	assetsDir := filepath.Join(s.dir, "assets")
	for _, e := range history {
		switch e.Type {
		case "userMessage", "agentMessage", "verbalReply":
		default:
			continue
		}
		for _, fr := range e.Files {
			if fr.Path == "" {
				continue
			}
			if _, ok := s.imageMap[fr.Path]; ok {
				continue
			}
			s.assetN++
			prefix := fmt.Sprintf("%s-%s-%d-", s.meta.Date, s.meta.Index, s.assetN)
			if matches, _ := filepath.Glob(filepath.Join(assetsDir, prefix+"*")); len(matches) > 0 {
				s.imageMap[fr.Path] = "./assets/" + filepath.Base(matches[0])
			}
		}
		renderChatBubble(e, &s.st, s.imageMap)
	}
}

// SetTitle renames the export to `{date}-{NN}-{slug}.md` (slug derived like
// export_chat_md) and rewrites the whole file from the in-memory history —
// the title is baked into the header comment, the H1, and the byline, so a
// rename is always a full rewrite. Afterwards the stream returns to pure
// appending. history must be the bus's full event history.
func (s *chatLogStream) SetTitle(title string, history []Event) error {
	slug := slugifyTitle(title)
	if slug == "" {
		return fmt.Errorf("title %q slugifies to nothing — need at least one alphanumeric character", title)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil && !s.stopped {
		return fmt.Errorf("chat log stream is closed")
	}

	s.meta.Slug = slug
	s.meta.Title = humanTitle(slug)
	newPath := filepath.Join(s.dir, fmt.Sprintf("%s-%s-%s.md", s.meta.Date, s.meta.Index, slug))

	// Full rewrite: fresh fold state, but the existing imageMap — assets were
	// copied when their events streamed and the upload sources may be gone.
	var st renderState
	var b strings.Builder
	b.WriteString(renderChatMarkdown(nil, s.meta, nil))
	for _, e := range history {
		b.WriteString(renderChatBubble(e, &st, s.imageMap))
	}

	if s.f != nil {
		s.f.Close()
		s.f = nil
	}
	if newPath != s.mdPath {
		// Retire the old name; after chatlog_optout it is already gone.
		if err := os.Remove(s.mdPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", s.mdPath, err)
		}
		s.mdPath = newPath
	}
	if err := os.WriteFile(newPath, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("rewrite %s: %w", newPath, err)
	}
	f, err := os.OpenFile(newPath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("reopen %s: %w", newPath, err)
	}
	s.f = f
	s.st = st
	s.stopped = false // set_chat_title re-arms a stream stopped by chatlog_optout
	return nil
}

// Optout implements chatlog_optout: stop appending, delete this session's .md
// (assets stay — their content-sha names may be shared with other sessions;
// orphans are harmless) and regenerate index.html so the archive no longer
// lists it. SetTitle re-arms.
func (s *chatLogStream) Optout() error {
	s.mu.Lock()
	if s.indexTimer != nil {
		s.indexTimer.Stop()
	}
	s.stopped = true
	if s.f != nil {
		s.f.Close()
		s.f = nil
	}
	mdPath, dir := s.mdPath, s.dir
	s.mu.Unlock()

	if err := os.Remove(mdPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", mdPath, err)
	}
	return regenerateIndexHTML(dir)
}

// scheduleIndexRegen (call with s.mu held) debounces the turn-end
// housekeeping: index.html is regenerated once the event stream has been
// quiet for the debounce window, not on every append.
func (s *chatLogStream) scheduleIndexRegen() {
	d := s.indexDebounce
	if d == 0 {
		d = 2 * time.Second
	}
	if s.indexTimer != nil {
		s.indexTimer.Stop()
	}
	dir := s.dir
	s.indexTimer = time.AfterFunc(d, func() {
		if err := regenerateIndexHTML(dir); err != nil {
			log.Printf("agent-chat: chatlog stream: regenerate index: %v", err)
		}
	})
}

// Close flushes and closes the stream's file and runs a final index
// regeneration (the SIGTERM path — nothing else needs writing at session end;
// every bubble is already on disk). Subsequent HandleEvent calls are no-ops.
func (s *chatLogStream) Close() {
	s.mu.Lock()
	if s.indexTimer != nil {
		s.indexTimer.Stop()
	}
	closed := false
	if s.f != nil {
		s.f.Sync()
		s.f.Close()
		s.f = nil
		closed = true
	}
	dir := s.dir
	s.mu.Unlock()
	if closed {
		if err := regenerateIndexHTML(dir); err != nil {
			log.Printf("agent-chat: chatlog stream: final index regeneration: %v", err)
		}
	}
}

// MDPath returns the current path of the stream's markdown file.
func (s *chatLogStream) MDPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mdPath
}

// Dir returns the export directory the stream writes into.
func (s *chatLogStream) Dir() string {
	return s.dir
}

// HandleEvent appends one bubble for a renderable event: attachments are
// copied into assets/ immediately, then the rendered markdown (possibly empty
// for blank turns) is appended to the file. Hidden bookkeeping events and
// anything after chatlog_optout write nothing.
func (s *chatLogStream) HandleEvent(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.f == nil {
		return
	}
	switch e.Type {
	case "userMessage", "agentMessage", "verbalReply":
	default:
		return
	}
	if len(e.Files) > 0 {
		warnings, err := writeEventAttachments(e, filepath.Join(s.dir, "assets"), s.meta.Date, s.meta.Index, &s.assetN, s.imageMap)
		for _, w := range warnings {
			log.Printf("agent-chat: chatlog stream: %s", w)
		}
		if err != nil {
			log.Printf("agent-chat: chatlog stream: copy attachments: %v", err)
		}
	}
	md := renderChatBubble(e, &s.st, s.imageMap)
	if md == "" {
		return
	}
	if _, err := s.f.WriteString(md); err != nil {
		log.Printf("agent-chat: chatlog stream: append %s: %v", s.mdPath, err)
		return
	}
	s.f.Sync()
	s.scheduleIndexRegen()
}
