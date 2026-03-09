# Voice Mode Rejection in send_message

**Date:** 2026-02-24
**Status:** Accepted

## Context

When the user is in voice mode (headphones, not looking at screen), the agent
might call `send_message` which renders text on screen — invisible to the user.
The agent has `send_verbal_reply` for voice mode, but nothing prevents it from
using the wrong tool.

## Decision

Reject `send_message` calls with an error when the last user message was a
voice message (detected by the 🎤 emoji prefix). Force the agent to use
`send_verbal_reply` instead, which triggers TTS playback.

Detection: `isVoiceMessage()` checks if the message text starts with 🎤.

## Alternatives Considered

- **Auto-convert text to speech** — hides the tool mismatch from the agent;
  agent won't learn to use the correct tool.
- **Allow both** — voice users miss important messages rendered only as text.
- **Prompt-only guidance** — agents don't reliably follow soft instructions.

## Consequences

- Voice-only users always hear agent responses via TTS.
- Agent gets a clear error message telling it to use `send_verbal_reply`.
- The agent learns the correct tool quickly (within 1-2 attempts).
- Non-voice messages still use `send_message` normally.
