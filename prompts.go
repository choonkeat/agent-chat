package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"
)

//go:embed prompts/agent-reply.tmpl
var agentReplyTmplStr string

var agentReplyTmpl = template.Must(template.New("agent-reply").Parse(agentReplyTmplStr))

// promptsFilePath is AGENT_CHAT_PROMPTS_FILE, set once in main. Empty means the
// built-in rules are used and nothing is read from disk. A package var rather
// than a Getenv at each call so tests are not steered by the caller's env.
var promptsFilePath string

// builtinRulesVersion fingerprints the built-in template. It is stamped into a
// rules file when agent-chat creates one, so a release whose built-in rules
// differ can tell the user. A fingerprint says "different", not "newer": the
// same notice fits a downgrade.
var builtinRulesVersion = fmt.Sprintf("%x", sha256.Sum256([]byte(agentReplyTmplStr)))[:8]

var rulesVersionRe = regexp.MustCompile(`rules-version: ([0-9a-f]+)`)

// rulesFileHeader is a template comment, so it never reaches the agent: only
// the named templates below it are ever executed.
func rulesFileHeader() string {
	return `{{/*
agent-chat reply rules — edit freely. Changes apply from the next message; no restart.

This file is the text agent-chat adds around every user message it hands the agent:
  "format-messages"          how the user's message(s) and attached files are laid out
  "execute-not-echo"         the line after the message, sent with every 10th message
  "execute-not-echo-short"   its short form, sent with the other nine
  "reply-instructions"       the full reply rules, sent with every 10th message
  "reply-instructions-short" the one-line reminder sent with the other nine
  "empty-queue"              what check_messages returns when nothing is waiting

{{$reply}} / {{$progress}} stand for the reply and progress tool names; they switch
to the voice tools when the user is speaking. A block you delete uses the built-in text.

If this file has a mistake, agent-chat uses its built-in rules and says so at start-up.
To start over from the current built-in rules, empty this file.

rules-version: ` + builtinRulesVersion + ` (the built-in rules this file was copied from; leave this line as-is)
*/}}
`
}

// expandHome turns a leading "~/" into the user's home directory; the variable
// is often written in a shell profile or JSON config where ~ is not expanded.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

var (
	promptsWarnMu   sync.Mutex
	promptsLastWarn string
)

// warnPromptsOnce logs a rules-file problem the first time it is seen, so a
// broken file does not repeat the same line on every delivery.
func warnPromptsOnce(msg string) {
	promptsWarnMu.Lock()
	defer promptsWarnMu.Unlock()
	if msg == promptsLastWarn {
		return
	}
	promptsLastWarn = msg
	log.Printf("Warning: %s", msg)
}

// loadRulesFile returns the parsed rules file at path, creating it from the
// built-in rules first when it is missing or blank so the user only ever has
// to edit, never author from scratch. It also returns the file's text for the
// version check. Any error means "use the built-in rules"; the file is never
// overwritten once it has content.
func loadRulesFile(path string) (*template.Template, string, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, "", err
	}
	text := string(data)
	if strings.TrimSpace(text) == "" {
		text = rulesFileHeader() + agentReplyTmplStr
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, "", fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			return nil, "", err
		}
		log.Printf("Wrote the built-in reply rules to %s — edit it to experiment", path)
	}
	t, err := template.New("agent-reply").Parse(text)
	if err != nil {
		return nil, text, err
	}
	return t, text, nil
}

// promptsStatus reports the configured rules file's state as one line for the
// chat's start-up tip, or "" when there is nothing to say (no file configured,
// or it is healthy and matches the built-in rules it was copied from).
func promptsStatus() string {
	if promptsFilePath == "" {
		return ""
	}
	path := expandHome(promptsFilePath)
	_, text, err := loadRulesFile(path)
	if err != nil {
		return fmt.Sprintf("Reply rules file `%s` has a problem, so the built-in rules are in use: %v", path, err)
	}
	if m := rulesVersionRe.FindStringSubmatch(text); m != nil && m[1] != builtinRulesVersion {
		return fmt.Sprintf("The built-in reply rules have changed since your copy in `%s` was made. Empty the file to refresh it (your edits will be replaced).", path)
	}
	return ""
}

func execTemplate(name string, data any) string {
	if promptsFilePath != "" {
		path := expandHome(promptsFilePath)
		t, _, err := loadRulesFile(path)
		// A block the file does not define uses the built-in one: files made
		// by an older release lack blocks added since, and the out-of-date
		// notice already tells the user to refresh.
		if err == nil && t.Lookup(name) == nil {
			return builtinText(name)
		}
		if err == nil {
			var buf bytes.Buffer
			if err = t.ExecuteTemplate(&buf, name, data); err == nil {
				return buf.String()
			}
		}
		warnPromptsOnce(fmt.Sprintf("reply rules file %s: %v (using built-in rules)", path, err))
	}
	return builtinTextWith(name, data)
}

// builtinText renders a built-in template that takes no data.
func builtinText(name string) string { return builtinTextWith(name, nil) }

func builtinTextWith(name string, data any) string {
	var buf bytes.Buffer
	if err := agentReplyTmpl.ExecuteTemplate(&buf, name, data); err != nil {
		panic(fmt.Sprintf("template %s: %v", name, err))
	}
	return buf.String()
}

// formatMessagesData is the data passed to the "format-messages" template.
type formatMessagesData struct {
	// Preamble is the style template hoisted above a multi-message batch that
	// shares one; empty when each message carries its own (see sharedPreamble).
	Preamble string
	Messages []messageData
	Files    []fileData
}

type messageData struct {
	Text    string
	IsVoice bool
}

type fileData struct {
	Path string
	Type string
	Size string
}

// replyInstructionsData is the data passed to the "reply-instructions" template.
type replyInstructionsData struct {
	IsVoice bool
}

// formatSize returns a human-readable size string.
func formatSize(size int64) string {
	if size >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(size)/1024/1024)
	}
	if size >= 1024 {
		return fmt.Sprintf("%.0fKB", float64(size)/1024)
	}
	return fmt.Sprintf("%dB", size)
}
