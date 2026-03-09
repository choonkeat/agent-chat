# Distinct Beep Tones for Voice Listening States

**Date:** 2026-03-09
**Status:** Accepted

## Context

When voice mode is active, the app plays a beep after TTS finishes to signal
that the microphone is back on. Previously, the same 880 Hz tone played
regardless of whether the agent was blocked waiting for user input or simply
passively listening.

Users operating by voice alone (not looking at the screen) had no way to
distinguish between:

1. **Active listening** — the agent has presented quick replies and is blocked
   until the user speaks.
2. **Passive listening** — the mic is on but the agent is not waiting for
   immediate input; any speech will be queued rather than acted on right away.

## Decision

Use two distinct beep frequencies after TTS completes:

| State | Frequency | Meaning |
|---|---|---|
| Quick replies present | **880 Hz** (high) | Agent is waiting — speak now |
| No quick replies | **440 Hz** (low) | Mic is on, but agent isn't blocked |

The first-time voice enable beep remains 880 Hz (unchanged).

## Consequences

- Voice-only users can tell by ear whether the agent needs their input or is
  just passively listening.
- No new audio files or dependencies — both tones use the existing `playBeep()`
  Web Audio API oscillator.
- The change is a single conditional in `speakVerbalReply()`:
  `playBeep(hasQuickReplies ? 880 : 440, 0.15)`.
