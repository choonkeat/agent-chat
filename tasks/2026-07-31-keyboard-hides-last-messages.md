# Bug: on-screen keyboard hides the last messages behind the sticky input bar

Filed 2026-07-31. Observed against `@choonkeat/agent-chat-linux-x64@0.8.21`,
iPhone / iOS Safari, agent-chat running inside a swe-swe Agent Chat pane
(an iframe).

## Symptom

Tap the message textarea. The keyboard opens, the visible area gets shorter,
and the sticky input bar comes to rest on top of the last few lines of the
conversation. Those lines are not lost -- dragging the conversation up by hand
brings them back -- but the newest content, including the message you were
replying to and its quick-reply chips, is covered at the exact moment you start
typing.

Reproduced twice, on two different pages of scrollback.

Not the same as the swe-swe-side bug fixed the same day (the app painting short
and leaving a dead band). That one is fixed; the outer frame now sizes and
positions itself correctly against `window.visualViewport`. This report is
about the chat document's own scroll position.

## Root cause

`client-dist/style.css`

- `#chat-footer` (line 842) is `position: sticky; bottom: 0`.
- `body` (line 42) has `padding: 0.75rem 1rem 0` -- no bottom padding.
- `#app` (line 51) is `min-height: 100%`.

So the page scrolls as a whole (`window.scrollY`), and the footer sticks to the
bottom edge of the viewport, painting over whatever flow content happens to sit
underneath it. That is fine while the document is scrolled to the very end,
because then nothing is underneath it. It is wrong at any other scroll offset.

`client-dist/app.js`

- Lines 155-160 track `isUserScrolledUp` on the `scroll` event only.
- Lines 162-166 `scrollToBottom(force)` -- `window.scrollTo(0, scrollHeight)`,
  skipped when the reader has deliberately scrolled up.
- Called after render, on new bubbles, and on send (lines 665, 712, 719, 759,
  1147, 1206, 2980).
- There is **no `resize` handler and no `visualViewport` handler anywhere in the
  file.** Confirmed by grep: `100vh`, `100dvh` and `visualViewport` appear
  nowhere in `client-dist/{app.js,style.css,index.html}`.

Shrinking the viewport is therefore the one way the "distance from the bottom"
can change without any code re-pinning the scroll. The document stays at the
offset it had when the viewport was tall, the footer moves up with the new
bottom edge, and it lands on content.

The amount hidden equals the keyboard height, which on a modern iPhone is
roughly 40% of the screen -- i.e. most of a screenful of conversation.

## Why an iframe changes which event fires

Worth being explicit, because the obvious one-line fix only covers one case.

- **Inside an iframe (swe-swe Agent Chat pane).** The keyboard never overlays
  the iframe. The host page resizes the iframe element, so the inner document's
  *layout* viewport really does shrink and the inner `window` fires `resize`.
- **Standalone tab on iOS Safari.** The keyboard is an overlay. The layout
  viewport does **not** shrink and `window.onresize` may not fire at all. Only
  `window.visualViewport` reports the change, via its `resize` and `scroll`
  events.

Both paths must be handled or the fix will look correct in one embedding and do
nothing in the other.

## Suggested fix

Re-pin to the bottom whenever the visible area changes size, honouring the
existing `isUserScrolledUp` flag so a reader who has scrolled back through
history is not yanked forward.

Add near the existing scroll tracking in `client-dist/app.js` (after line 166):

```js
// --- Re-pin on viewport change ---
//
// #chat-footer is position: sticky, so it paints over flow content whenever the
// document is not scrolled to the very end. Opening the on-screen keyboard
// shortens the visible area without moving the scroll offset, which puts the
// last messages underneath the footer. Re-pin to the bottom, unless the reader
// deliberately scrolled up.
//
// Two listeners, not one: inside an iframe the host resizes the frame and the
// inner window fires `resize`; in a standalone iOS Safari tab the keyboard is an
// overlay, the layout viewport does not change, and only visualViewport reports
// it.
function repinOnViewportChange() {
  // One frame late: Safari has not finished settling the viewport when the
  // event fires, so scrollHeight read now would be stale.
  requestAnimationFrame(function () { scrollToBottom(false); });
}

window.addEventListener('resize', repinOnViewportChange);
if (window.visualViewport) {
  window.visualViewport.addEventListener('resize', repinOnViewportChange);
}
```

Optional second step, if the above still leaves a gap in practice: also re-pin
when the textarea takes focus, since that is the user action that triggers the
keyboard.

```js
chatInput.addEventListener('focus', repinOnViewportChange);
```

`autoGrow()` (line 1363) grows the textarea up to 150px as you type, which moves
the footer's top edge up and hides another line or two. Calling
`repinOnViewportChange()` at the end of `autoGrow` would cover that too. Lower
priority -- it is one or two lines, not a screenful.

### Why not the alternative fixes

- **Give `body` a bottom padding equal to the footer height.** Reserves the
  space so sticky never overlaps. But the footer height is dynamic (the textarea
  auto-grows, quick-reply chips appear and disappear), so the padding has to be
  measured and re-applied in JS anyway, and it permanently wastes a strip of
  screen while the keyboard is closed.
- **Make the footer `position: fixed` and pad the scroller.** Same measurement
  problem, plus it takes the footer out of flow and changes how the page behaves
  when content is shorter than one screen.
- **Switch to a scrolling `#chat` container instead of scrolling the document.**
  Structurally the cleanest, and it would make `scrollToBottom` local rather
  than document-wide. Too large for this bug; worth considering separately.

The scroll re-pin is about eight lines and touches nothing else.

## Verification

Cannot be caught by the node tests -- it needs a real on-screen keyboard. Check
by hand on an iPhone:

1. Open a conversation with more than one screen of scrollback.
2. Tap the textarea. The last message must still be fully visible directly above
   the input bar, with the quick-reply chips visible.
3. Scroll up several screens, then tap the textarea. The view must **stay** where
   it is -- this is the `isUserScrolledUp` case, and yanking it to the bottom
   would be a regression.
4. Dismiss the keyboard. No dead strip, no jump.
5. Repeat in a standalone Safari tab (not inside swe-swe) to exercise the
   `visualViewport` path.

## Context

Screenshots exist but are deliberately not committed here: they show the dev
tunnel hostname in the Safari address bar, and this repo is public.
