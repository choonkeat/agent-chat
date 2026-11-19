package main

// Step 2 phase 3 of tasks/2026-11-18-mcp-apps-step2-page-tools.md: the
// user's answers to "Let the <server> page run its tools?".
//
// A Yes covers one page (the chat window keeps that). Here agent-chat keeps,
// per tool program name, which sessions said Yes, and whether the user chose
// Always allow. After a Yes in an earlier session, the question also offers
// Always allow; a No resets the count. Each session's chat runs on its own
// port (its own browser origin), so this lives on disk, not in localStorage:
// <user cache dir>/agent-chat/mcp-app-allow.json, mode 0600.
//
// Known limit: a different program given the same name is also allowed.

import (
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

// mcpAppSessionID tells sessions apart: swe-swe's SESSION_UUID, or a random
// id for this run.
var mcpAppSessionID = func() string {
	if id := os.Getenv("SESSION_UUID"); id != "" {
		return id
	}
	return uuid.New().String()
}()

const mcpAppAllowMaxSessions = 5

type mcpAppAllowEntry struct {
	YesSessions []string `json:"yesSessions,omitempty"`
	Always      bool     `json:"always,omitempty"`
}

type mcpAppAllowFile struct {
	Servers map[string]*mcpAppAllowEntry `json:"servers"`
}

// mcpAppAllowState is what the chat window needs to ask the question.
type mcpAppAllowState struct {
	Always      bool `json:"always"`
	OfferAlways bool `json:"offerAlways"`
}

var mcpAppAllowMu sync.Mutex

func mcpAppAllowPath() (string, error) {
	base, err := mcpAppBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mcp-app-allow.json"), nil
}

// loadMcpAppAllow reads the store; a missing or broken file is empty.
// Caller holds mcpAppAllowMu.
func loadMcpAppAllow() mcpAppAllowFile {
	f := mcpAppAllowFile{Servers: map[string]*mcpAppAllowEntry{}}
	path, err := mcpAppAllowPath()
	if err != nil {
		return f
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &f) != nil || f.Servers == nil {
		f.Servers = map[string]*mcpAppAllowEntry{}
	}
	return f
}

// saveMcpAppAllow writes the store atomically, 0600. Caller holds mcpAppAllowMu.
func saveMcpAppAllow(f mcpAppAllowFile) error {
	path, err := mcpAppAllowPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-allow-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// mcpAppFromChatPage refuses requests that didn't come from agent-chat's own
// page: Sec-Fetch-Site other than same-origin, or (for bodies) anything but
// JSON, which a cross-site page can't send without a CORS preflight.
func mcpAppFromChatPage(w http.ResponseWriter, r *http.Request, needJSON bool) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		http.Error(w, "only from the chat page", http.StatusForbidden)
		return false
	}
	if needJSON {
		if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/json" {
			http.Error(w, "application/json only", http.StatusUnsupportedMediaType)
			return false
		}
	}
	return true
}

// handleMcpAppAllow: GET ?server= → state; POST {server, answer} records
// yes / no / always; DELETE forgets everything.
func handleMcpAppAllow(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !mcpAppFromChatPage(w, r, false) {
			return
		}
		server := r.URL.Query().Get("server")
		mcpAppAllowMu.Lock()
		e := loadMcpAppAllow().Servers[server]
		mcpAppAllowMu.Unlock()
		st := mcpAppAllowState{}
		if e != nil && server != "" {
			st.Always = e.Always
			for _, s := range e.YesSessions {
				if s != mcpAppSessionID {
					st.OfferAlways = true
				}
			}
		}
		writeJSON(w, http.StatusOK, st)
	case http.MethodPost:
		if !mcpAppFromChatPage(w, r, true) {
			return
		}
		var b struct {
			Server string `json:"server"`
			Answer string `json:"answer"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil || b.Server == "" || len(b.Server) > 200 {
			http.Error(w, "server and answer are required", http.StatusBadRequest)
			return
		}
		if b.Answer != "yes" && b.Answer != "no" && b.Answer != "always" {
			http.Error(w, `answer must be "yes", "no" or "always"`, http.StatusBadRequest)
			return
		}
		mcpAppAllowMu.Lock()
		defer mcpAppAllowMu.Unlock()
		f := loadMcpAppAllow()
		e := f.Servers[b.Server]
		if e == nil {
			e = &mcpAppAllowEntry{}
			f.Servers[b.Server] = e
		}
		switch b.Answer {
		case "yes":
			seen := false
			for _, s := range e.YesSessions {
				seen = seen || s == mcpAppSessionID
			}
			if !seen {
				e.YesSessions = append(e.YesSessions, mcpAppSessionID)
				if n := len(e.YesSessions); n > mcpAppAllowMaxSessions {
					e.YesSessions = e.YesSessions[n-mcpAppAllowMaxSessions:]
				}
			}
		case "no":
			delete(f.Servers, b.Server)
		case "always":
			e.Always = true
		}
		if err := saveMcpAppAllow(f); err != nil {
			http.Error(w, "could not save: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodDelete:
		if !mcpAppFromChatPage(w, r, false) {
			return
		}
		mcpAppAllowMu.Lock()
		defer mcpAppAllowMu.Unlock()
		if err := saveMcpAppAllow(mcpAppAllowFile{Servers: map[string]*mcpAppAllowEntry{}}); err != nil {
			http.Error(w, "could not save: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "GET, POST or DELETE", http.StatusMethodNotAllowed)
	}
}
