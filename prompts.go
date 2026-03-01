package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

//go:embed prompts/agent-reply.tmpl
var agentReplyTmplStr string

var agentReplyTmpl = template.Must(template.New("agent-reply").Parse(agentReplyTmplStr))

// formatMessagesData is the data passed to the "format-messages" template.
type formatMessagesData struct {
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

// voiceSuffixData is the data passed to the "voice-suffix" template.
type voiceSuffixData struct {
	IsVoice bool
}

func execTemplate(name string, data any) string {
	var buf bytes.Buffer
	if err := agentReplyTmpl.ExecuteTemplate(&buf, name, data); err != nil {
		panic(fmt.Sprintf("template %s: %v", name, err))
	}
	return buf.String()
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
