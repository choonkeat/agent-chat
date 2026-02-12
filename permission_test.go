package main

import (
	"os"
	"testing"
)

func loadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("failed to read testdata/%s: %v", name, err)
	}
	return data
}

func TestParseJSONLLine_BashMkdir(t *testing.T) {
	line := loadTestdata(t, "line_59.json")
	prompts, resolved := ParseJSONLLine(line)

	if len(resolved) != 0 {
		t.Fatalf("expected no resolved IDs, got %v", resolved)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}

	p := prompts[0]
	if p.ToolUseID != "toolu_01Kq5eV1ficMFRauXwnUhekD" {
		t.Errorf("wrong tool_use_id: %s", p.ToolUseID)
	}
	if p.ToolName != "Bash" {
		t.Errorf("wrong tool_name: %s", p.ToolName)
	}
	if p.Title != "Ensure tasks directory exists" {
		t.Errorf("wrong title: %s", p.Title)
	}
	if p.Detail != "mkdir -p /repos/agent-chat/workspace/tasks" {
		t.Errorf("wrong detail: %s", p.Detail)
	}
}

func TestParseJSONLLine_BashCp(t *testing.T) {
	line := loadTestdata(t, "line_67.json")
	prompts, resolved := ParseJSONLLine(line)

	if len(resolved) != 0 {
		t.Fatalf("expected no resolved IDs, got %v", resolved)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}

	p := prompts[0]
	if p.ToolUseID != "toolu_017F1rzH6zH8Rs1DhQv68HJi" {
		t.Errorf("wrong tool_use_id: %s", p.ToolUseID)
	}
	if p.ToolName != "Bash" {
		t.Errorf("wrong tool_name: %s", p.ToolName)
	}
	if p.Title != "Copy research file to workspace tasks" {
		t.Errorf("wrong title: %s", p.Title)
	}
	if p.Detail != "cp /workspace/research/2026-02-10-agent-chat-integration.md /repos/agent-chat/workspace/tasks/2026-02-10-permission-detection.md" {
		t.Errorf("wrong detail: %s", p.Detail)
	}
}

func TestParseJSONLLine_BashLs(t *testing.T) {
	line := loadTestdata(t, "line_39.json")
	prompts, _ := ParseJSONLLine(line)

	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}

	p := prompts[0]
	if p.ToolName != "Bash" {
		t.Errorf("wrong tool_name: %s", p.ToolName)
	}
	if p.Title != "List tasks directory" {
		t.Errorf("wrong title: %s", p.Title)
	}
	if p.Detail != "ls /repos/agent-chat/workspace/tasks/" {
		t.Errorf("wrong detail: %s", p.Detail)
	}
}

func TestParseJSONLLine_Read(t *testing.T) {
	line := loadTestdata(t, "line_19.json")
	prompts, _ := ParseJSONLLine(line)

	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}

	p := prompts[0]
	if p.ToolName != "Read" {
		t.Errorf("wrong tool_name: %s", p.ToolName)
	}
	if p.Title != "Read /repos/agent-chat/workspace/TODO.md" {
		t.Errorf("wrong title: %s", p.Title)
	}
	if p.Detail != "/repos/agent-chat/workspace/TODO.md" {
		t.Errorf("wrong detail: %s", p.Detail)
	}
}

func TestParseJSONLLine_Write(t *testing.T) {
	line := loadTestdata(t, "line_77.json")
	prompts, _ := ParseJSONLLine(line)

	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}

	p := prompts[0]
	if p.ToolName != "Write" {
		t.Errorf("wrong tool_name: %s", p.ToolName)
	}
	if p.Title != "Write /repos/agent-chat/workspace/tasks/2026-02-10-permission-detection.md" {
		t.Errorf("wrong title: %s", p.Title)
	}
}

func TestParseJSONLLine_Glob(t *testing.T) {
	line := loadTestdata(t, "line_6.json")
	prompts, _ := ParseJSONLLine(line)

	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}

	p := prompts[0]
	if p.ToolName != "Glob" {
		t.Errorf("wrong tool_name: %s", p.ToolName)
	}
	if p.Title != "Search for **/*.md" {
		t.Errorf("wrong title: %s", p.Title)
	}
	if p.Detail != "/repos/agent-chat/workspace" {
		t.Errorf("wrong detail: %s", p.Detail)
	}
}

func TestParseJSONLLine_Grep(t *testing.T) {
	line := loadTestdata(t, "line_16.json")
	prompts, _ := ParseJSONLLine(line)

	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}

	p := prompts[0]
	if p.ToolName != "Grep" {
		t.Errorf("wrong tool_name: %s", p.ToolName)
	}
	if p.Title != "Search for 'permission'" {
		t.Errorf("wrong title: %s", p.Title)
	}
	if p.Detail != "/repos/agent-chat/workspace" {
		t.Errorf("wrong detail: %s", p.Detail)
	}
}

func TestParseJSONLLine_UserMessage(t *testing.T) {
	line := loadTestdata(t, "line_2.json")
	prompts, resolved := ParseJSONLLine(line)

	if len(prompts) != 0 {
		t.Errorf("expected no prompts for user message, got %d", len(prompts))
	}
	if len(resolved) != 0 {
		t.Errorf("expected no resolved IDs for plain user message, got %v", resolved)
	}
}

func TestParseJSONLLine_Progress(t *testing.T) {
	line := loadTestdata(t, "line_7.json")
	prompts, resolved := ParseJSONLLine(line)

	if len(prompts) != 0 {
		t.Errorf("expected no prompts for progress entry, got %d", len(prompts))
	}
	if len(resolved) != 0 {
		t.Errorf("expected no resolved IDs for progress entry, got %v", resolved)
	}
}

func TestParseJSONLLine_ToolResult(t *testing.T) {
	line := loadTestdata(t, "line_60.json")
	prompts, resolved := ParseJSONLLine(line)

	if len(prompts) != 0 {
		t.Errorf("expected no prompts for tool_result, got %d", len(prompts))
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved ID, got %d", len(resolved))
	}
	if resolved[0] != "toolu_01Kq5eV1ficMFRauXwnUhekD" {
		t.Errorf("wrong resolved ID: %s", resolved[0])
	}
}

func TestParseJSONLLine_InvalidJSON(t *testing.T) {
	prompts, resolved := ParseJSONLLine([]byte("not json"))
	if len(prompts) != 0 || len(resolved) != 0 {
		t.Errorf("expected nothing for invalid JSON")
	}
}

func TestParseJSONLLine_EmptyLine(t *testing.T) {
	prompts, resolved := ParseJSONLLine([]byte(""))
	if len(prompts) != 0 || len(resolved) != 0 {
		t.Errorf("expected nothing for empty line")
	}
}
