<!-- agent-chat export
title: Chat Submit Ctrl Cmd Enter Autofocus Audit
date: 2026-06-13
index: 01
slug: chat-submit-ctrl-cmd-enter-autofocus-audit
agent: claude
version: 0.8.0 (f5b4443)
-->

# Chat Submit Ctrl Cmd Enter Autofocus Audit

_2026-06-13 · 01 · claude · agent-chat 0.8.0 (f5b4443)_

**USER**

> 1. Ctrl/Cmd + Enter should submit chat
> 2. Shift-Enter should always be just adding a newline character
> 3. We have some code that tries to auto focus our chat textarea: let’s have an audit of where they are

<small>took 2m 33s</small><br>
**AGENT**

> Done — edited `client-dist/app.js` (keydown handler) and rebuilt the linux-x64 binary.
>
> ## 1 & 2. Submit / newline behavior
> Old: plain **Enter** sent (desktop only), Shift/Alt+Enter = newline, mobile = newline.
> New (all platforms):
> - **Ctrl/Cmd + Enter** → submits
> - **Enter** (plain) → newline
> - **Shift + Enter** → newline (always)
>
> I removed the now-unused `isMobile` mobile-detection var since the submit gesture is uniform across platforms now. (Autocomplete dropdown still uses Enter/Tab to pick a highlighted option when it's open — unchanged.)
>
> ## 3. Auto-focus audit — every place chat textarea is focused
>
> | # | File:line | Trigger | Purpose |
> |---|-----------|---------|---------|
> | 1 | `client-dist/index.html:30` | `autofocus` attribute on `<textarea>` | Focus on initial page load |
> | 2 | `client-dist/app.js:96` | `focusChatInput()` on `window` focus + `visibilitychange` | Re-focus when tab/iframe regains focus (guarded by `!disabled && !readOnly`) |
> | 3 | `client-dist/app.js:738` | `enableInput()` | Focus when input is re-enabled (replies available) |
> | 4 | `client-dist/app.js:904` | `handleSend()` | Focus right before setting `readOnly` during send (keeps mobile keyboard up) |
> | 5 | `client-dist/app.js:1046` | `acSelect()` | Re-focus after picking an autocomplete option |
>
> Note #2 is the most aggressive one — it grabs focus on every window focus / tab-visibility change. If autofocus ever feels intrusive, that's the one to reconsider.
>
> ⚠️ The new binary won't take effect until the running `agent-chat` process is restarted (client-dist is embedded at compile time). Want me to restart it, or would you prefer to handle that?

[Quick replies]
- Restart it
- I'll restart myself
- Soften autofocus #2 too

**USER**

> > submit/newline behaviour
>
> Oh no! Old behaviour already looks good (i like the desktop Enter = submit; so i do need isMobile back!). Actually should’ve been just adding Ctrl/Cmd + Enter = submit it seems?
>
> More robust if we undo the change and try again TDD?
>
> > auto-focus
>
> I’m considering to drop #2

<small>took 9m 5s</small><br>
**AGENT**

> Redone TDD-style, both parts. ✅
>
> ## Submit / newline (kept `isMobile`, added Ctrl/Cmd+Enter)
> You were right — on **desktop** the old code already submitted on Ctrl/Cmd+Enter (it only bailed on Shift/Alt). The actual gap was **mobile/hardware-keyboard**: `if (isMobile) return` ran *before* any modifier check, so Ctrl/Cmd+Enter inserted a newline there too.
>
> New `keydown` order in `client-dist/app.js`:
> ```js
> if (e.key !== 'Enter') return;
> if (e.metaKey || e.ctrlKey) { e.preventDefault(); handleSend(); return; } // Ctrl/Cmd+Enter ALWAYS sends
> if (isMobile) return;                  // mobile: plain Enter = newline
> if (e.shiftKey || e.altKey) return;    // desktop: Shift/Alt+Enter = newline
> e.preventDefault(); handleSend();      // desktop: plain Enter = send
> ```
> Net behavior: desktop Enter still submits (as you like), Shift+Enter is always a newline, and Ctrl/Cmd+Enter now submits on **every** platform.
>
> ## TDD: `e2e/chat-submit.spec.cjs` (new)
> 6 specs over desktop + mobile (mobile via mobile-UA context):
> - desktop: plain Enter submits · Shift+Enter → newline · Ctrl+Enter submits · Cmd(Meta)+Enter submits
> - mobile: plain Enter → newline · **Ctrl+Enter still submits**
>
> Red→green: against the original code, **5 passed and only `mobile: Ctrl+Enter` failed** — exactly the one behavior I changed. After the fix: **6/6 green**. (Submit is observed via `#loading-bubble`; each test gets a fresh server since the server replays history to new pages.)
>
> ## Auto-focus #2 — dropped
> Removed the `window 'focus'` + `visibilitychange` auto-focus block (`focusChatInput`) — the aggressive one that grabbed focus on every tab/window refocus. The other 4 remain (initial `autofocus`, `enableInput`, `handleSend`, autocomplete select). Easy to restore if you miss it.
>
> ## Tests
> `make test`: unit ✅, e2e 30 passed + 1 flake (`:zzzznotanemoji` — autocomplete debounce timing under parallel load; passes in isolation, doesn't touch Enter/submit). My 6 new specs all pass.
>
> ⚠️ Same caveat: the running `agent-chat` process needs a restart to serve the rebuilt binary (client-dist is embedded). Want me to restart it?

[Quick replies]
- Restart it
- I'll restart myself
- Restore autofocus #2
- Commit this

**USER**

> export agent chat log and git commit; make build

