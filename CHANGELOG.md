# Changelog

All notable changes to agent-chat are documented in this file.

## [Unreleased] (since 0.1.9)

## [0.1.9] — 2026-03-11

### Features
- Replace beep tones with spoken "Be right back" for voice progress updates
- Preview TTS voice on selection and persist choice in localStorage
- Distinct beep tones for active vs passive listening (superseded by spoken brb)
- Score and rank autocomplete results by match quality
- Add `has_more` flag to autocomplete API; skip client cache when results are truncated

### Fixes
- TTS cutoff on iOS: proportional safety timeout and numbered-list protection
- Prefer local build over npm-installed platform binary

### Docs
- Add 14+ ADRs documenting past architectural decisions
- Add ADR for spoken brb, supersede beep tone ADRs

### Refactor
- Split Makefile test targets into unit-test, e2e-test, e2e-report

### Tests
- Use @dom fuzzy query in E2E autocomplete happy path
- Add no-results scenario to E2E autocomplete suite

## [0.1.8] — 2026-03-01

### Features
- Add per-bubble TTS play button to agent messages
- Trigger-based autocomplete with external provider proxy
- Per-trigger URLs and built-in @filepath autocomplete
- Structured autocomplete response with debug info

### Fixes
- Remind agent to use chat tools after check_messages returns empty
- Include nudge text in interrupt postMessage and support standalone mode
- Nudge agent toward send_message when task is done
- Collapse repeated system messages into counter
- Eager file upload on selection to prevent silent attachment drops
- Show loading and no-results states in autocomplete dropdown
- Filepath autocomplete root-skip and empty-cache bugs

### Docs
- Add autocomplete API reference

### Tests
- Add Playwright E2E test for @filepath autocomplete

## [0.1.7] — 2026-03-01

### Fixes
- Handle loose markdown lists (blank lines between items)

### Tests
- Add loose list cases to markdown torture test

## [0.1.6] — 2026-02-27

### Features
- Show version stamp in chat on connect
- Show version mismatch between server and page
- Voice interrupt — stop/cancel phrases send Esc-Esc to parent PTY
- Add /mcp/orchestrator endpoint for external chat interaction
- Persist unchosen quick replies in chat log
- Extend interrupt detection to typed messages
- Rewrite reply-instructions template with confirmation checklist
- Ask one question at a time in voice mode
- Tell agent not to ask questions in the TUI

### Fixes
- Update test expectations to match current reply-instructions template
- Inline config.js to prevent dark-mode stuck when proxied
- Send historyEnd event after reconnect replay
- Strengthen one-question-per-message rule in voice mode

### Refactor
- Extract message formatting to embedded templates
- Rename voice-suffix template to reply-instructions
- Simplify reply instructions to express intent not steps
- Rename push_message to send_chat_message for consistency

## [0.1.5] — 2026-02-25

### Features
- Broadcast user messages to all browsers and add cursor-based event sync
- Improve markdown rendering and UX polish
- Add blockquote rendering with nested quote support
- Reload event log from disk on server restart
- Track quick_replies state for browser reconnect and strip stale replies
- Show WebSocket connect/disconnect as system messages in chat

### Fixes
- Remove optimistic display from quick reply handler to prevent duplicate bubbles
- Resolve three markdown rendering bugs
- Adjust code block font size and use CSS vars for code backgrounds
- Use readOnly instead of disabled to preserve focus while sending
- Align messages to bottom of chat when few are present
- Scroll to bottom on user message
- Move quick-replies into message flow for inline display

### Docs
- Add README and www/ landing page with screenshots

## [0.1.4] — 2026-02-23

### Features
- Add file upload with drag-drop, thumbnails, and agent file paths
- Add voice mode with STT/TTS and send_verbal_reply tool
- Add send_verbal_progress tool, quick_replies to verbal reply, and iOS TTS fix
- Add speech-detection blink indicator and harden voice mode
- Add image_urls to send tools, timestamps on events, and STT warning
- Elapsed time labels, TTS queue, voice styling, and export improvements
- Add check_messages reminder to all message responses
- Reject send_message when user is in voice mode

### Fixes
- Expand file drop zone to cover entire window
- Voice messages now trigger parent frame notification and show clear STT context
- Let Enter insert newline on mobile, send only via button
- Make speech confirmation prompt explicitly ask for yes/no
- Move voice dropdown to header row and loading indicator below messages
- Split TTS into sentence-sized chunks to avoid iOS truncation

### Refactor
- Simplify loading indicator and quick replies logic
- Simplify voice mode warmup to just "Ready"

## [0.1.3] — 2026-02-22

### Features
- Persist events to JSONL and add playback mode
- Add light/dark theme support, syntax highlighting, and table rendering
- Add -v flag with version and git commit SHA
- Add npm distribution via npx @choonkeat/agent-chat

### Fixes
- Fix link contrast and duplicate first message on reload
- Fix npx execute permission issue with postinstall chmod
- Fix make bump to update optionalDependencies versions

## [0.1.0] — 2026-02-09

### Features
- Initial release: MCP-based chat UI with WebSocket
- Send/receive messages with quick reply chips
- Non-blocking check_messages polling
- Inline canvas drawing with Rough.js hand-drawn rendering
- Lightweight markdown rendering
- Auto-growing textarea input
- Send_progress tool with animated thinking dots
