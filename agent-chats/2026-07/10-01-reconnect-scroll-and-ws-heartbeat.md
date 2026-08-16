<!-- agent-chat export
title: Reconnect Scroll And Ws Heartbeat
date: 2026-07-10
index: 01
slug: reconnect-scroll-and-ws-heartbeat
agent: claude
version: 0.8.7 (2089b8b)
-->

# Reconnect Scroll And Ws Heartbeat

_2026-07-10 · 01 · claude · agent-chat 0.8.7 (2089b8b)_

**USER**

> I have an environment that keeps having websocket disconnect/reconnect. What happens is that the agent chat will keep bumping down even if I’m reading earlier message.
>
> I think it’s a feature that chat scrolls down when there’s new things, but we can treat disconnect/reconnect differently? Anything else too? Or I’m wrong
>
> Discuss

<small>took 9m 40s</small><br>
**AGENT**

> You're right, and I found the exact culprit. It's not the normal auto-scroll — that already respects your scroll position. It's the **reconnect handshake** specifically.
>
> **Root cause**
> - Normal new content calls `scrollToBottom(false)`, which bails out if you've scrolled up (`isUserScrolledUp` guard). So reading history is normally safe.
> - But on every reconnect, the server's `connected` event handler calls `enableInput()` (app.js:2302), and `enableInput()` unconditionally does `setTimeout(() => scrollToBottom(true), 100)` (app.js:891) — a **forced** scroll that ignores the guard. That's what yanks you to the bottom on each flap.
> - The `Disconnected`/`Reconnected` system bubbles themselves are innocent — they use the non-forced scroll and wouldn't move you.
>
> So yes: disconnect/reconnect can and should be treated differently. The fix is to not force-scroll on the reconnect path (preserve position when you're scrolled up; still auto-follow if you were already at the bottom).
>
> **Anything else — 3 things I noticed:**
>
> 1. **Same force fires for new agent prompts.** `enableInput()` also runs when the agent posts a fresh question with quick-replies. So even without a reconnect, if you scroll up to re-read while the agent is working, the next prompt yanks you down too. We can scope the fix to reconnect-only, or apply "respect scroll" to all of enableInput except right after *you* send a message. I lean toward the broader fix — it's more consistent.
>
> 2. **No heartbeat = the flapping itself.** There's no WebSocket ping/keepalive anywhere (client or Go server). Idle proxies in front of the server love to kill quiet sockets, which is very likely *why* your env keeps disconnecting. A periodic ping would cut the reconnect churn at the source. Bigger change, optional, but it's the real fix for "keeps disconnecting."
>
> 3. **Nice-to-have UX:** instead of ever force-scrolling, show a "↓ new messages" pill when you're scrolled up. More work; only if you want it.
>
> My recommendation: do (1) the broad scroll fix now (small, clearly correct), and separately consider (2) the heartbeat if the disconnects are annoying you beyond the scroll jump. Want me to implement 1 (+2)?

[Quick replies]
- Do 1 (broad scroll fix)
- Do 1 + 2 (scroll + heartbeat)
- Reconnect-only scroll fix
- Just 1, keep it minimal
- Let me think

**USER**

> Do 1 (broad scroll fix)

<small>took 1m 32s</small><br>
**AGENT**

> Done — broad scroll fix is in.
>
> **Change** (`client-dist/app.js`, in `enableInput()`): the forced `scrollToBottom(true)` is now `scrollToBottom(false)`, so it obeys the `isUserScrolledUp` guard. Effect:
> - Scrolled up reading history → reconnects and new agent prompts no longer yank you down.
> - Already at the bottom → still auto-follows as before.
> - You sending a message → unchanged; that path resets the guard and force-scrolls on its own, so it still jumps to your own message.
>
> **Verified:**
> - Rebuilt the linux-x64 binary; confirmed the fix is embedded in it (`EMBEDDED_FIX_PRESENT`).
> - `make unit-test` (vet + tests) passes.
>
> **One thing to note:** I did *not* restart your live agent-chat server, because that server is this very session's process (it also hosts this chat channel) — restarting it would drop our connection. So the running UI still serves the old JS until it's restarted. When you're ready, restart the agent-chat server (or start a fresh session) to pick up the new binary and feel the difference.
>
> Want me to also do #2 (WebSocket heartbeat) to cut down the disconnects themselves, or commit this fix?

[Quick replies]
- Commit this fix
- Also do #2 (heartbeat)
- Commit + do #2
- Not yet, let me test first

**USER**

> Commit + do #2

<small>took 4m 2s</small><br>
**AGENT**

> Both done and committed.
>
> **Commit 1 — `6620129` fix(chat): scroll fix**
> `enableInput()` no longer force-scrolls; it respects your scroll position. Reconnects and new agent prompts won't yank you down while you're reading history. Your own sent messages still jump to the bottom (that path is separate).
>
> **Commit 2 — `bf0d51f` feat(ws): WebSocket keepalive**
> Added a proper ping/pong heartbeat to the Go server (there was none):
> - Writer goroutine pings every 25s; browsers auto-pong.
> - 60s read deadline, reset on each pong → dead clients torn down promptly.
> - Write deadlines so a stuck client can't block the writer.
> - The 25s pings keep idle-timeout proxies/LBs from silently dropping the socket — the likely root cause of your reconnect churn.
>
> **Verified:**
> - `make unit-test` (vet + tests) passes on both changes.
> - Spun up a throwaway HTTP-only server and connected a raw WS probe: `PING_RECEIVED after 25.019s` — heartbeat fires exactly as tuned. Probe + demo server cleaned up; only `main.go` + `client-dist/app.js` changed.
>
> **Still need a restart to go live:** I rebuilt the binary but did NOT restart your session's server (it hosts this chat). Restart agent-chat (or open a fresh session) to pick up both fixes.
>
> Note: 25s ping assumes your proxy idle timeout is ≥30s (typical). If it still flaps after this, the timeout is tighter and I can drop pingPeriod lower. Want me to leave it here?

[Quick replies]
- Leave it here, thanks
- Lower the ping interval
- Run the full E2E suite too
- Push to remote

**USER**

> Run the full E2E suite too

<small>took 16.7s</small><br>
**AGENT**

> Running full E2E suite (CDP warmed; specs launch the rebuilt binary)…

<small>took 2m 50s</small><br>
**AGENT**

> ✅ Full E2E suite: **65 passed (2.4m)**.
>
> This is a real test of the changes — each spec spawns its own agent-chat server from the freshly-rebuilt binary (`-no-stdio-mcp`, random port), so both the scroll fix and the new ping/pong heartbeat were exercised end-to-end. Nothing regressed.
>
> Summary of everything done this session:
> - `6620129` fix(chat): scroll no longer force-jumps on reconnect / new prompts
> - `bf0d51f` feat(ws): WebSocket keepalive ping/pong (verified pinging at 25.0s)
> - `make unit-test` ✅ · `make e2e-test` ✅ 65/65
>
> Both are committed on `main`, unpushed. Reminder: restart your agent-chat server (or start a fresh session) to run the new binary and feel the fixes live. Want me to push, or leave it for you?

[Quick replies]
- Leave it, I'll push
- Push to origin/main
- That's all, thanks

**USER**

> /ck:export-chat-logs

