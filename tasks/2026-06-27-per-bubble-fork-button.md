# Per-bubble "fork" button in agent-chat (`fork_session`)

## Goal

Let the user fork the conversation from any agent speech bubble in the chat
panel: a small **fork** button on each agent bubble (above the existing play
button) that, after a confirmation dialog, opens a new swe-swe session branched
at that point. The original session keeps running.

The server already implements this: `GET /api/fork/<sessionUUID>?bubble=<seq>&mode=after`
maps a chat-event `seq` to the agent's tool-call cut point and stages a forked
session (chat history restored + agent reattached). This task is purely the UI
that surfaces it.

## swe-swe side -- DONE (this repo)

`static/terminal-ui.js` appends `fork_session=<live session uuid>` to the
agent-chat iframe `src`, alongside the existing `parent_url` param:

```js
const u = new URL(chatSrc, window.location.href);
u.searchParams.set('parent_url', window.location.href);
if (this.sessionUUID) u.searchParams.set('fork_session', this.sessionUUID);
chatSrc = u.toString();
```

So the iframe loads with both `parent_url` (origin for resolving relative URLs,
already shipped) and `fork_session` (the live swe-swe session uuid). `/api/fork`
resolves a live session by its in-memory uuid, so this uuid is the right fork
source. Shipped in swe-swe commit "feat(chat): pass fork_session to agent-chat
iframe for per-bubble fork".

## agent-chat side -- TODO (the `/repos/agent-chat` repo)

When the `fork_session` query param is present, render a **fork** button on each
**agent** bubble (`agentMessage` / `verbalReply`), positioned above the existing
play button. On click:

1. Show a **confirmation dialog** (fork creates a new session; guard against
   accidental clicks). Suggested copy: "Fork a new session branched from this
   point? Your current session keeps running."
2. On confirm, build the absolute fork URL and open it in a **new tab**:

```js
var forkSession = new URLSearchParams(window.location.search).get('fork_session') || '';
// parentBaseUrl is the same parent_url already used for relative-link resolution
function forkUrl(seq) {
  return new URL('/api/fork/' + encodeURIComponent(forkSession)
                 + '?bubble=' + seq + '&mode=after', parentBaseUrl).href;
}
// on confirm:
window.open(forkUrl(bubbleSeq), '_blank');
```

- The button only appears when `fork_session` is non-empty (feature flag = presence
  of the param). With no param, behaviour is unchanged (standalone agent-chat).
- Each bubble already carries its event `seq`; pass that as `bubble=<seq>`.
- **New tab, not same-tab**: keeps the current session alive and means any error
  (see below) lands in a throwaway tab instead of replacing the live conversation.

### Constraints / decisions (confirmed with user 2026-06-27)

- **`mode=after` only.** forkconvo implements only the "after" cut today
  (`replay`/`before` are stubs server-side). Do not expose a mode picker; hardcode
  `mode=after` ("continue from after this agent reply").
- **Agent bubbles, not user bubbles.** Matches the play-button placement and the
  intuitive "fork from here". (User-message bubbles are also anchorable server-side
  via the `userMessagesConsumed` stamp, but are out of scope for v1.)
- **Fail gracefully; do not preflight.** Some forks legitimately fail and the
  server returns a non-2xx that the new tab will display:
  - `409` -- the source agent is mid-tool-call (retry when idle).
  - `ErrBubbleNotDrained` (409-ish) -- the bubble hasn't been consumed by an MCP
    call yet.
  - channels-mode agent bubbles have no explicit `send_message` tool_use, so the
    server falls back to text-correlation, which can occasionally not resolve.
  For v1, just let the new tab show the server's error; the live session is
  unaffected. (A nicer v2 could `fetch` + toast, but that's not required.)
- **Reuse `parent_url`** for the origin; do NOT have swe-swe pass a second absolute
  base. `new URL(relativeForkPath, parent_url)` is the agreed construction.
- **SPA staleness** -- same caveat as `parent_url`: both params are captured at
  iframe-src construction time. Accepted for v1.

### Acceptance

- With `fork_session` set: each agent bubble shows a fork button; clicking →
  confirm → new tab opens the forked session with prior chat history and the agent
  reattached.
- With `fork_session` absent (standalone): no fork button, no behaviour change.
- Unit/e2e coverage mirroring the `parent_url` tests (button hidden without param;
  URL built correctly with param).

## Phases

- [x] **Phase 1 — fork URL plumbing (logic).** Read `fork_session` query param into
  a top-level `forkSession` var (mirrors `parentBaseUrl`). Add `forkUrl(seq)` that
  builds the absolute `/api/fork/<forkSession>?bubble=<seq>&mode=after` URL against
  `parentBaseUrl`. Tests (mirroring the `parent_url` tests in
  `e2e/markdown-images.spec.cjs`): `fork_session` query param read into
  `forkSession`; `forkUrl(seq)` builds the correct absolute URL.
- [x] **Phase 2 — fork button on agent bubbles (DOM + interaction).** Thread event
  `seq` into `addBubble`/`addAgentMessage` from the `agentMessage`/`verbalReply`
  handlers and history replay. Add `createForkButton(seq)` rendered on agent bubbles
  ABOVE the play button, only when `forkSession` is non-empty; on click show a
  `confirm()` dialog and on confirm `window.open(forkUrl(seq), '_blank')`. CSS for
  `.bubble-fork-btn`. Tests: no fork button when `fork_session` absent; fork button
  present on agent bubbles when `fork_session` set; user bubbles never get one;
  click → confirm → `window.open` called with the correct fork URL.
  *(Superseded by Phase 3 — see below.)*

- [x] **Phase 3 — consolidate into a "⋯" overflow menu (design pivot, confirmed
  with user 2026-06-27).** A stacked fork-above-play button floats above short
  (1–2 line) bubbles and the two round buttons sit too close (fat-finger risk).
  Pivot: a single `.bubble-menu-btn` (⋯) per agent bubble that opens a menu of
  large (~44px) labeled rows with inline SVG icons — "Speak aloud" (TTS) and
  "Fork from here". Decisions:
  - Menu only when `fork_session` is set AND the bubble has a `seq`; standalone
    agent-chat and seq-less local notices keep the existing plain ▶ `.bubble-tts-btn`
    (zero behaviour change without the param).
  - The deliberate menu selection replaces the old `confirm()` dialog; "Fork from
    here" → `window.open(forkUrl(seq), '_blank')` directly.
  - Toggle on the ⋯ button; dismiss on outside-click and Esc; menu is
    viewport-clamped (flips above the button near the bottom edge).
  - `pulseLastTtsButton` falls back to the menu button so iOS voice-unlock still
    works when the play button lives in the menu.
  - Remove the now-dead `createForkButton`/`.bubble-fork-btn` path.
  Tests: ⋯ present (and no standalone ▶) when fork enabled; standalone keeps plain
  ▶; user bubbles / seq-less bubbles get no ⋯; click ⋯ opens a menu with speak +
  fork rows; toggle and outside-click and Esc dismiss; fork row → `window.open`
  with the correct URL + menu closes; rows meet the ~44px tap-target size; menu
  stays within the viewport.
