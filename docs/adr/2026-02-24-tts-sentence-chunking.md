# TTS Sentence-Based Chunking for iOS Truncation

**Date:** 2026-02-24
**Status:** Accepted

## Context

WebKit/iOS truncates `SpeechSynthesisUtterance` playback after approximately
15 seconds. Long agent messages (multiple paragraphs) get cut off mid-sentence
with no error callback.

## Decision

Split text into chunks of ~200 characters at sentence boundaries (periods,
exclamation marks, question marks) before feeding to `speechSynthesis.speak()`.
Each chunk is spoken sequentially with `onend` chaining to the next.

A 15-second safety timer per chunk detects stuck utterances (a separate
Safari/WebKit bug where `speak()` after `cancel()` silently fails). On timeout,
the system falls back to the manual play button.

## Alternatives Considered

- **Server-side chunking** — adds backend complexity for a client-side display
  issue.
- **Fixed character limit** — may split mid-word or mid-sentence.
- **No chunking** — iOS truncation makes long messages unusable.

## Consequences

- Long messages are fully spoken on iOS without truncation.
- Sentence-boundary splitting sounds natural (pauses between chunks are brief).
- Safety timer prevents the UI from hanging on stuck TTS.
- Slight overhead from multiple `speak()` calls, but imperceptible to users.
