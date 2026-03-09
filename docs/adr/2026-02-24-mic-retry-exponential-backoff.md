# Microphone Retry with Exponential Backoff

**Date:** 2026-02-24
**Status:** Accepted

## Context

`webkitSpeechRecognition` can fail transiently due to network issues (it uses a
cloud service), permission edge cases, or browser audio session conflicts. A
single failure shouldn't disable voice mode entirely, but unbounded retries
would drain battery and spam error events.

## Decision

Retry microphone start with exponential backoff: base delay 500ms, doubling
each attempt, up to 5 retries max. On each retry, destroy and recreate the
`SpeechRecognition` instance to clear any bad internal state.

```
Retry 1: 500ms
Retry 2: 1000ms
Retry 3: 2000ms
Retry 4: 4000ms
Retry 5: 8000ms
```

After 5 failures, display a system message and disable voice mode.

## Alternatives Considered

- **No retry** — too fragile; transient failures are common on mobile.
- **Fixed delay retry** — doesn't back off; wastes resources on persistent
  failures.
- **Infinite retry** — risks runaway loops and battery drain.

## Consequences

- Transient mic failures recover automatically within seconds.
- Persistent failures (revoked permission, hardware issue) give up gracefully.
- Recreating the recognition instance on each retry avoids stale-state bugs.
- Retry counter resets on successful mic start or TTS completion.
