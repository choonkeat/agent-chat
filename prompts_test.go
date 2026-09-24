package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useRulesFile points AGENT_CHAT_PROMPTS_FILE at path for one test.
func useRulesFile(t *testing.T, path string) {
	t.Helper()
	promptsFilePath = path
	t.Cleanup(func() { promptsFilePath = "" })
}

func builtinSuffix() string {
	saved := promptsFilePath
	promptsFilePath = ""
	defer func() { promptsFilePath = saved }()
	return voiceSuffix([]UserMessage{{Text: "hi"}})
}

func TestRulesFileMissingIsCreatedFromBuiltin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "rules.tmpl")
	useRulesFile(t, path)

	if got := voiceSuffix([]UserMessage{{Text: "hi"}}); got != builtinSuffix() {
		t.Errorf("fresh rules file should render the built-in rules:\ngot:  %q", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("rules file not created: %v", err)
	}
	if !strings.Contains(string(data), "rules-version: "+builtinRulesVersion) || !strings.HasSuffix(string(data), agentReplyTmplStr) {
		t.Errorf("created file should be the header plus the built-in rules, got:\n%s", data)
	}
	if s := promptsStatus(); s != "" {
		t.Errorf("fresh file should need no notice, got %q", s)
	}
}

func TestRulesFileBlankIsPopulated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.tmpl")
	os.WriteFile(path, []byte("  \n\t\n"), 0644)
	useRulesFile(t, path)

	voiceSuffix([]UserMessage{{Text: "hi"}})
	data, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(data), agentReplyTmplStr) {
		t.Errorf("blank file should be filled with the built-in rules, got:\n%s", data)
	}
}

func TestRulesFileEditsAreUsedAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.tmpl")
	edited := strings.Replace(agentReplyTmplStr, "The TUI is invisible to the user", "EDITED: the TUI is invisible", 1)
	os.WriteFile(path, []byte(edited), 0644)
	useRulesFile(t, path)

	if got := voiceSuffix([]UserMessage{{Text: "hi"}}); !strings.HasPrefix(got, "EDITED: the TUI is invisible") {
		t.Errorf("edited rules not used, got %q", got)
	}
	if data, _ := os.ReadFile(path); string(data) != edited {
		t.Error("an edited rules file must never be overwritten")
	}
	// No version line (the user removed it): nothing to compare, no notice.
	if s := promptsStatus(); s != "" {
		t.Errorf("unexpected notice %q", s)
	}
}

func TestRulesFileBrokenFallsBackToBuiltin(t *testing.T) {
	for name, text := range map[string]string{
		"parse error": `{{define "reply-instructions"}}{{if}}{{end}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rules.tmpl")
			os.WriteFile(path, []byte(text), 0644)
			useRulesFile(t, path)

			if got := voiceSuffix([]UserMessage{{Text: "hi"}}); got != builtinSuffix() {
				t.Errorf("broken file should fall back to built-in rules, got %q", got)
			}
			if data, _ := os.ReadFile(path); string(data) != text {
				t.Error("a broken rules file must not be touched")
			}
			if s := promptsStatus(); !strings.Contains(s, "has a problem") {
				t.Errorf("expected a problem notice, got %q", s)
			}
		})
	}
}

func TestRulesFileOlderVersionNotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.tmpl")
	old := strings.Replace(rulesFileHeader(), builtinRulesVersion, "00000000", 1) + agentReplyTmplStr
	os.WriteFile(path, []byte(old), 0644)
	useRulesFile(t, path)

	if s := promptsStatus(); !strings.Contains(s, "Newer built-in reply rules exist") {
		t.Errorf("expected an out-of-date notice, got %q", s)
	}
}

func TestRulesFileUnsetMeansNoNotice(t *testing.T) {
	if s := promptsStatus(); s != "" {
		t.Errorf("no rules file configured, got notice %q", s)
	}
}

// A file that defines only some blocks (e.g. one made before a block was
// added) uses its own blocks and the built-in text for the rest.
func TestRulesFilePartialUsesBuiltinForMissingBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.tmpl")
	os.WriteFile(path, []byte(`{{define "reply-instructions"}}only this one{{end}}`), 0644)
	useRulesFile(t, path)

	if got := voiceSuffix([]UserMessage{{Text: "hi"}}); got != "only this one" {
		t.Errorf("defined block not used, got %q", got)
	}
	if got := execTemplate("empty-queue", nil); got != emptyQueueGuidance {
		t.Errorf("missing block should use built-in text, got %q", got)
	}
	if s := promptsStatus(); s != "" {
		t.Errorf("a partial file is not a problem, got notice %q", s)
	}
}

func TestRulesFileOverridesFixedSentences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.tmpl")
	os.WriteFile(path, []byte(`{{define "execute-not-echo"}}FULL{{end}}`+
		`{{define "execute-not-echo-short"}}SHORT{{end}}`+
		`{{define "empty-queue"}}NOTHING WAITING{{end}}`), 0644)
	useRulesFile(t, path)
	resetGuidance(t)

	msgs := []UserMessage{{Text: "hi"}}
	if got := deliveryGuidance(msgs); !strings.HasPrefix(got, "FULL\n\n") {
		t.Errorf("full delivery should lead with the override, got %q", got)
	}
	if got := deliveryGuidance(msgs); !strings.HasPrefix(got, "SHORT\n\n") {
		t.Errorf("repeat delivery should lead with the short override, got %q", got)
	}
	if got := composeCheckMessagesResult(nil, nil); got != "NOTHING WAITING" {
		t.Errorf("empty queue override not used, got %q", got)
	}
}
