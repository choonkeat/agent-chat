# Web Audio API for Voice Mode Beeps

**Date:** 2026-02-24
**Status:** Accepted

## Context

Voice mode needs audible feedback when the microphone starts/stops listening.
Users operating without looking at the screen rely on these sounds to know the
system state.

## Decision

Generate beep tones programmatically using the Web Audio API (`AudioContext`,
`OscillatorNode`, `GainNode`) instead of embedding audio files. The `playBeep()`
function creates a short oscillator tone at a given frequency and duration.

Current tones:
- **880 Hz** — voice mode enabled; active listening (quick replies present)
- **440 Hz** — passive listening (no quick replies); disable voice mode

## Alternatives Considered

- **Embedded audio files (.mp3/.wav)** — adds file size to the binary; harder
  to tune parameters; requires preloading.
- **No audio feedback** — poor UX for voice-only users who can't see the screen.

## Consequences

- Zero additional assets — no audio files to embed or preload.
- Tones are easily tunable (frequency, duration, volume) without replacing files.
- Works across all modern browsers with Web Audio API support.
- Gain set to 0.15 to avoid startling users.
