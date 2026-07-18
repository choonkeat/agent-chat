package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
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
}

// newChatLogStream claims the next daily NN in dir by creating the provisional
// .md file (O_EXCL, retrying on collision) and writes the export header,
// including a `session:` line so a restarted process can find this file again.
func newChatLogStream(dir, sessionID, agent, version string, now time.Time) (*chatLogStream, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := ensureViewerAssets(filepath.Join(dir, "assets")); err != nil {
		return nil, err
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

	return &chatLogStream{
		dir:      dir,
		mdPath:   mdPath,
		meta:     meta,
		imageMap: map[string]string{},
		f:        f,
	}, nil
}

// MDPath returns the current path of the stream's markdown file.
func (s *chatLogStream) MDPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mdPath
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
}
