# Spoken "Be right back" for Voice Progress Updates

**Date:** 2026-03-10
**Status:** Accepted
**Supersedes:** 2026-03-09-distinct-beep-tones-for-voice-listening-states.md

## Context

The previous approach used distinct Web Audio API beep tones (880 Hz vs 440 Hz)
to signal whether the agent was actively waiting for input or passively
listening. Testing on iOS revealed that these beeps were completely masked by
iOS system sounds that play when SpeechRecognition starts and stops. Users
heard the same sound regardless of the code path, making the beep distinction
useless on iOS.

## Decision

Remove all `playBeep()` calls from voice mode (mic on, mic off, and post-TTS)
and replace with spoken word cues:

- **Progress messages** (no quick replies — agent still working): TTS appends
  "Be right back." to the spoken text. The message bubble shows `[brb]` via a
  CSS `::after` pseudo-element on the `.brb` class, keeping the actual message
  text clean.
- **Reply messages** (with quick replies — agent waiting for input): no suffix,
  TTS speaks the message as-is.

The `playBeep()` function is retained in case future use is needed but is no
longer called.

## Alternatives Considered

- **Louder/longer beeps** — would not solve the iOS system sound masking issue.
- **Different waveforms** (square vs sine) — still masked by iOS system sounds.
- **Double beep for active listening** — tested; still indistinguishable from
  iOS system sounds.

## Consequences

- Works reliably on iOS where system sounds mask Web Audio API output.
- Voice-only users hear "Be right back" and know the agent is still working.
- No suffix on replies means silence = "your turn to speak."
- `[brb]` CSS indicator gives visual users the same information at a glance.
- No platform-specific code needed — the spoken approach works everywhere.
