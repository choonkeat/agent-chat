package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PermissionPrompt represents a pending tool use that needs user approval.
// Produced by parsing JSONL assistant entries containing tool_use blocks.
type PermissionPrompt struct {
	ToolUseID string `json:"tool_use_id"`
	ToolName  string `json:"tool_name"`
	Title     string `json:"title"`  // short human-readable description
	Detail    string `json:"detail"` // the command, file path, pattern, etc.
}

// jsonlEntry is the top-level structure of a Claude Code session JSONL line.
type jsonlEntry struct {
	Type    string      `json:"type"`
	Message jsonlMessage `json:"message"`
}

type jsonlMessage struct {
	Role    string            `json:"role"`
	Content json.RawMessage   `json:"content"`
}

// toolUseBlock is a tool_use item in the assistant message content array.
type toolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// toolResultBlock is a tool_result item in the user message content array.
type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
}

// ParseJSONLLine parses a single JSONL line and returns any permission prompts found.
// Returns nil if the line is not an assistant entry with tool_use blocks.
// Also returns resolved tool_use IDs from tool_result entries (for clearing pending prompts).
func ParseJSONLLine(line []byte) (prompts []PermissionPrompt, resolvedIDs []string) {
	var entry jsonlEntry
	if json.Unmarshal(line, &entry) != nil {
		return nil, nil
	}

	switch entry.Type {
	case "assistant":
		return parseAssistantEntry(entry.Message), nil
	case "user":
		return nil, parseToolResults(entry.Message)
	default:
		return nil, nil
	}
}

func parseAssistantEntry(msg jsonlMessage) []PermissionPrompt {
	if msg.Role != "assistant" {
		return nil
	}

	var contentItems []json.RawMessage
	if json.Unmarshal(msg.Content, &contentItems) != nil {
		return nil
	}

	var prompts []PermissionPrompt
	for _, raw := range contentItems {
		var block toolUseBlock
		if json.Unmarshal(raw, &block) != nil || block.Type != "tool_use" {
			continue
		}
		if p, ok := toolUseToPrompt(block); ok {
			prompts = append(prompts, p)
		}
	}
	return prompts
}

func parseToolResults(msg jsonlMessage) []string {
	if msg.Role != "user" {
		return nil
	}

	var contentItems []json.RawMessage
	if json.Unmarshal(msg.Content, &contentItems) != nil {
		return nil
	}

	var ids []string
	for _, raw := range contentItems {
		var block toolResultBlock
		if json.Unmarshal(raw, &block) != nil || block.Type != "tool_result" {
			continue
		}
		if block.ToolUseID != "" {
			ids = append(ids, block.ToolUseID)
		}
	}
	return ids
}

func toolUseToPrompt(block toolUseBlock) (PermissionPrompt, bool) {
	p := PermissionPrompt{
		ToolUseID: block.ID,
		ToolName:  block.Name,
	}

	var input map[string]any
	if json.Unmarshal(block.Input, &input) != nil {
		p.Title = block.Name
		return p, true
	}

	switch block.Name {
	case "Bash":
		p.Title = stringField(input, "description")
		p.Detail = stringField(input, "command")
		if p.Title == "" {
			p.Title = p.Detail
		}
	case "Read":
		path := stringField(input, "file_path")
		p.Title = "Read " + path
		p.Detail = path
	case "Write":
		path := stringField(input, "file_path")
		p.Title = "Write " + path
		p.Detail = path
	case "Edit":
		path := stringField(input, "file_path")
		p.Title = "Edit " + path
		p.Detail = path
	case "Glob":
		pattern := stringField(input, "pattern")
		path := stringField(input, "path")
		p.Title = "Search for " + pattern
		if path != "" {
			p.Detail = path
		}
	case "Grep":
		pattern := stringField(input, "pattern")
		path := stringField(input, "path")
		p.Title = "Search for '" + pattern + "'"
		if path != "" {
			p.Detail = path
		}
	default:
		p.Title = block.Name
	}

	return p, true
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// SanitizeCWD converts a working directory path to the directory name
// Claude Code uses inside ~/.claude/projects/. Slashes become dashes
// and leading dashes are stripped.
func SanitizeCWD(cwd string) string {
	s := strings.ReplaceAll(cwd, "/", "-")
	s = strings.TrimLeft(s, "-")
	return s
}

// FindSessionFile searches for the most recent .jsonl file in projectDir
// that contains the given bootID string. Returns the full path of the
// matching file, or empty string if not found.
func FindSessionFile(projectDir, bootID string) (string, error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", err
	}

	// Collect .jsonl files with their mod times, sort newest first
	type fileInfo struct {
		path    string
		modTime int64
	}
	var files []fileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			path:    filepath.Join(projectDir, e.Name()),
			modTime: info.ModTime().UnixNano(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime > files[j].modTime
	})

	// Search up to 10 most recent files
	limit := 10
	if len(files) < limit {
		limit = len(files)
	}
	for _, f := range files[:limit] {
		if fileContains(f.path, bootID) {
			return f.path, nil
		}
	}
	return "", nil
}

// fileContains checks if a file contains the given substring by scanning
// line by line (avoids reading entire large files into memory).
func fileContains(path, substr string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), substr) {
			return true
		}
	}
	return false
}
