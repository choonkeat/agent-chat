package main

import "encoding/json"

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
