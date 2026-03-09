# TTS Warmup Unlocking for iOS

**Date:** 2026-02-24
**Status:** Accepted

## Context

iOS Safari (and some other browsers) require `speechSynthesis.speak()` to be
called within a direct user gesture (click/tap) chain. The voice mode enable
flow calls `getUserMedia()` for mic permission, which shows a browser dialog.
After the dialog resolves, the `Promise.then` callback may no longer count as a
user gesture on some platforms, causing TTS to silently fail.

## Decision

Perform TTS warmup in two stages during `enableVoiceMode()`:

1. **Synchronous warmup** — call `speechSynthesis.speak("Ready")` immediately
   in the click handler, before `getUserMedia()`. This succeeds on platforms
   that require strict user gesture context.

2. **Fallback warmup** — call `speechSynthesis.speak("Ready")` again inside the
   `getUserMedia().then()` callback. This catches platforms where `Promise.then`
   preserves user activation.

A `ttsUnlocked` flag tracks whether either warmup succeeded. If neither worked,
agent verbal replies show a pulsing play button for manual TTS unlock via user
gesture.

## Alternatives Considered

- **Always require manual play** — poor UX for voice-first workflow.
- **Single warmup before getUserMedia** — misses platforms that need the
  callback context.
- **Single warmup after getUserMedia** — fails on iOS strict gesture tracking.

## Consequences

- Auto-play TTS works on most platforms without extra user interaction.
- iOS users who hit the edge case still get a manual play button fallback.
- The "Ready" warmup utterance is audible at volume 1.0 (intentional — confirms
  TTS is working).
