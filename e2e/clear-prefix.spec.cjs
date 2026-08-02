// @ts-check
// Pins the `/clear <instruction>` sequence.
//
// Motivation: wiping the agent's memory and handing it a new instruction are
// two halves of one action, and each half is a separate message to the parent
// frame, so a regression here is silent — the chat simply stops answering. The
// order is load-bearing (wipe, then record, then resume), and so is which
// carrier the resume line calls the instruction: naming both the chat log and
// the queue got the same question answered twice, in two different styles.
//
// The agent-chat page is loaded inside a parent page that records every
// postMessage it receives, which is exactly what swe-swe's terminal pane does
// with them.
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { gotoRetry } = require('./goto-retry.cjs');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);

// Starts a fresh agent-chat server with the streaming chat-log export on —
// without it there is no file for the resume line to name.
function startServer(extraArgs = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-clear-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';
    cleanEnv.AGENT_CHAT_EXPORT_DIR = 'agent-chats';

    const proc = spawn(bin, ['-no-stdio-mcp', ...extraArgs], {
      cwd: dir,
      env: cleanEnv,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stderr = '';
    proc.stderr.on('data', (chunk) => {
      stderr += chunk.toString();
      const match = stderr.match(/Agent Chat UI: (http:\/\/localhost:\d+)/);
      if (match) resolve({ url: match[1], proc, dir });
    });
    proc.on('error', reject);
    proc.on('exit', (code) => {
      if (!stderr.includes('Agent Chat UI:')) {
        reject(new Error(`Server exited with code ${code}. stderr: ${stderr}`));
      }
    });
    setTimeout(() => reject(new Error('Server did not start within 10s')), 10000);
  });
}

const test = base.extend({
  page: async ({}, use) => {
    const browser = await chromium.connectOverCDP(CDP_ENDPOINT, {
      ...(SLOW_MO > 0 && { slowMo: SLOW_MO }),
    });
    const context = await browser.newContext();
    const page = await context.newPage();
    try {
      await use(page);
    } finally {
      await context.close().catch(() => {});
    }
  },
});

// Loads agent-chat in an iframe under a parent that records postMessages.
async function embed(page, url) {
  await gotoRetry(page, url); // proves the server answers before we frame it
  await page.setContent(
    `<iframe id="chat" src="${url}" style="width:100%;height:600px;border:0"></iframe>`
  );
  await page.evaluate(() => {
    window.__msgs = [];
    window.addEventListener('message', (e) => {
      if (e.data && e.data.type) window.__msgs.push(e.data);
    });
  });
  const frame = page.frameLocator('#chat');
  await expect(frame.locator('#chat-input')).toBeEnabled({ timeout: 10000 });
  return frame;
}

const interrupts = (page) =>
  page.evaluate(() => window.__msgs.filter((m) => m.type === 'agent-chat-interrupt').map((m) => m.text));

test.describe('/clear prefix', () => {
  test('wipes first, records the stripped instruction, then names the log file', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#chat-input').fill('/clear now fix the logout bug');
      await frame.locator('#chat-input').press('Enter');

      // 1. The wipe goes out immediately — before anything is recorded, so a
      //    still-running agent cannot eat the instruction and then be erased.
      await expect.poll(() => interrupts(page), { timeout: 5000 }).toEqual(['/clear']);

      // 2. The stripped instruction lands in the chat as the user's own bubble.
      //    The `/clear ` prefix is not part of the recorded message, and the
      //    wipe leaves no bubble of its own — no marker, no status line.
      const userBubble = frame.locator('.bubble.user', { hasText: 'now fix the logout bug' });
      await expect(userBubble).toBeVisible({ timeout: 10000 });
      await expect(userBubble).not.toContainText('/clear');
      await expect(frame.locator('.bubble', { hasText: /context cleared/i })).toHaveCount(0);
      // Still unread: the instruction is sitting in the queue waiting for the
      // agent that comes back, which is what the resume line sends it to
      // collect. A dead waiter swallowing it would show as read.
      await expect(userBubble).toHaveClass(/pending-agent/);

      // 3. Only then is the resume line typed, naming the file by its current
      //    path. The instruction itself comes from check_messages, not from the
      //    file: naming both as the instruction made the agent answer twice.
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      const [, resume] = await interrupts(page);
      expect(resume).toMatch(/^resume agent-chats\/[\d-]+-untitled.*\.md /);
      expect(resume).toContain('for context');
      expect(resume).toContain('check_messages');
      // An `@` would open the agent CLI's file picker and the trailing Enter
      // would pick an entry instead of submitting the line.
      expect(resume).not.toContain('@');

      // The file the resume line names must exist and hold the instruction.
      const named = resume.slice('resume '.length).split(' ')[0];
      const md = fs.readFileSync(path.join(server.dir, named), 'utf8');
      expect(md).toContain('now fix the logout bug');
      expect(md).not.toContain('context cleared');
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('bare /clear wipes and resumes with no instruction', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#chat-input').fill('/clear');
      await frame.locator('#chat-input').press('Enter');

      // No instruction to record and no bubble of any kind, but the resume line
      // must still be typed — otherwise the wiped agent sits there and the chat
      // looks dead.
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      await expect(frame.locator('.bubble.user')).toHaveCount(0);
      await expect(frame.locator('.bubble', { hasText: /context cleared/i })).toHaveCount(0);
      const [wipe, resume] = await interrupts(page);
      expect(wipe).toBe('/clear');
      expect(resume).toMatch(/^resume agent-chats\//);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // "Conversation context only" is the same sequence without the prefix: tick
  // it once in the settings panel and every ordinary message wipes, records and
  // resumes. Worth pinning separately from the typed prefix, because the two
  // reach maybeHandleClearPrefix by different routes and the checkbox is the
  // one with no visible `/clear` to tell the user what it did.
  test('conversation-context-only routes an ordinary message through the clear sequence', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').check();
      await frame.locator('#btn-settings-done').click();

      await frame.locator('#chat-input').fill('now fix the logout bug');
      await frame.locator('#chat-input').press('Enter');

      await expect.poll(() => interrupts(page), { timeout: 5000 }).toEqual(['/clear']);

      // The bubble is the user's own words — the prefix the checkbox stands in
      // for must not leak into what was said.
      const userBubble = frame.locator('.bubble.user', { hasText: 'now fix the logout bug' });
      await expect(userBubble).toBeVisible({ timeout: 10000 });
      await expect(userBubble).not.toContainText('/clear');

      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      const [, resume] = await interrupts(page);
      expect(resume).toMatch(/^resume agent-chats\//);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // A message carrying an attachment goes the same way. It used to be the one
  // exception: the browser skipped the reset for it, so a file-carrying message
  // quietly kept the context every other message had just dropped. Nothing
  // about a file makes it hard to carry — it is uploaded before the message is
  // sent, so what travels is a path, and the `clear` frame has always had a
  // field for it.
  test('conversation-context-only routes an attachment through the clear sequence', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').check();
      await frame.locator('#btn-settings-done').click();

      const upload = path.join(server.dir, 'notes.txt');
      fs.writeFileSync(upload, 'the failing stack trace');
      await frame.locator('#file-picker').setInputFiles(upload);
      // The send button unlocks only once the upload has a path to send.
      await expect(frame.locator('#file-staging')).toHaveClass(/visible/, { timeout: 10000 });
      await expect(frame.locator('#btn-send')).toBeEnabled({ timeout: 10000 });

      await frame.locator('#chat-input').fill('what does this say');
      await frame.locator('#chat-input').press('Enter');

      // Same two-step reset as a message with no file.
      await expect.poll(() => interrupts(page), { timeout: 5000 }).toEqual(['/clear']);
      const userBubble = frame.locator('.bubble.user', { hasText: 'what does this say' });
      await expect(userBubble).toBeVisible({ timeout: 10000 });
      await expect(userBubble).not.toContainText('/clear');
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      const [, resume] = await interrupts(page);
      expect(resume).toMatch(/^resume agent-chats\//);

      // The file went with it: the server's copy of the message carries the
      // attachment, which is what puts it in the log the resume line names.
      const named = resume.slice('resume '.length).split(' ')[0];
      await expect.poll(
        () => fs.readFileSync(path.join(server.dir, named), 'utf8'),
        { timeout: 10000 }
      ).toContain('notes.txt');

      // The staging strip is empty again — a file left staged would ride along
      // with the next message too.
      await expect(frame.locator('#file-staging')).not.toHaveClass(/visible/, { timeout: 10000 });
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // The reset does not reach the server for a couple of seconds — the terminal
  // has to be typed into and left to settle. Waiting for the server's copy to
  // draw the bubble left the message nowhere at all for that whole gap, which
  // reads as having lost it. It is drawn on the way out instead, unread, and
  // the server's copy takes that bubble over rather than adding a second.
  test('the message is drawn as an unread bubble before the server has it', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);
      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').check();
      await frame.locator('#btn-settings-done').click();

      const input = frame.locator('#chat-input');
      await input.fill('what did we just talk about?');
      await input.press('Enter');

      // Drawn while the reset is still settling — the resume line, which only
      // goes out once the server has the message, has not been typed yet.
      const bubble = frame.locator('.bubble.user', { hasText: 'what did we just talk about?' });
      await expect(bubble).toBeVisible({ timeout: 1500 });
      await expect(bubble).toHaveClass(/pending-agent/);
      expect(await interrupts(page)).toEqual(['/clear']);
      // The box is empty and locked: the words are on screen, and they must not
      // be sendable twice.
      await expect(input).toHaveValue('');
      await expect(input).toHaveJSProperty('readOnly', true);

      // The server's copy brings the id and takes the same bubble over. One
      // bubble, not two, and it can be unsent now that it has an id.
      await expect(bubble).toHaveAttribute('data-msg-id', /.+/, { timeout: 10000 });
      await expect(frame.locator('.bubble.user')).toHaveCount(1);
      await expect(bubble.locator('.bubble-pending-menu')).toHaveCount(1);
      await expect(input).toHaveJSProperty('readOnly', false);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // Drawing the bubble before the server has the message is only safe if a
  // failure takes it back down. The connection dying inside the settle window
  // is the one failure this route can actually detect.
  test('a send that fails puts the bubble away and the words back in the box', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);
      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').check();
      await frame.locator('#btn-settings-done').click();

      const input = frame.locator('#chat-input');
      await input.fill('this one never lands');
      await input.press('Enter');
      await expect(frame.locator('.bubble.user', { hasText: 'this one never lands' })).toBeVisible({ timeout: 1500 });

      // Kill the server inside the settle window, before the instruction is sent.
      server.proc.kill('SIGKILL');

      await expect(frame.locator('.bubble', { hasText: /back in the box/i })).toBeVisible({ timeout: 10000 });
      await expect(frame.locator('.bubble.user', { hasText: 'this one never lands' })).toHaveCount(0);
      await expect(input).toHaveValue('this one never lands');
      await expect(input).toHaveJSProperty('readOnly', false);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // Off unless switched on. A browser that has never touched the box sends the
  // ordinary way, and ticking still has to win — a default is a starting
  // position, not a lock.
  test('starts unticked with no flag, and ticking beats it', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);
      await frame.locator('#btn-settings').click();
      await expect(frame.locator('#ctx-only-input')).not.toBeChecked();

      await frame.locator('#btn-settings-done').click();
      await frame.locator('#chat-input').fill('first message');
      await frame.locator('#chat-input').press('Enter');
      await expect(frame.locator('.bubble.user', { hasText: 'first message' })).toBeVisible({ timeout: 10000 });
      expect(await interrupts(page)).toHaveLength(0);

      // Tick: this browser has now answered, and its answer outranks the
      // session's opening position.
      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').check();
      await frame.locator('#btn-settings-done').click();
      await frame.locator('#chat-input').fill('second message');
      await frame.locator('#chat-input').press('Enter');
      await expect.poll(() => interrupts(page), { timeout: 5000 }).toEqual(['/clear']);
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('-conversation-context-only starts ticked', async ({ page }) => {
    const server = await startServer(['-conversation-context-only']);
    try {
      const frame = await embed(page, server.url);
      await frame.locator('#btn-settings').click();
      await expect(frame.locator('#ctx-only-input')).toBeChecked();

      await frame.locator('#btn-settings-done').click();
      await frame.locator('#chat-input').fill('an ordinary message');
      await frame.locator('#chat-input').press('Enter');
      await expect.poll(() => interrupts(page), { timeout: 5000 }).toEqual(['/clear']);
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // The tick travels between sessions the way the message style does — it is
  // how you want to be talked to, not a property of one conversation. Switching
  // it on in one chat switches it on in the next.
  test('the tick reaches every chat in the browser', async ({ page }) => {
    const first = await startServer();
    const second = await startServer();
    try {
      await gotoRetry(page, first.url);
      await page.locator('#btn-settings').click();
      await page.locator('#ctx-only-input').check();
      await expect(page.locator('#ctx-only-input')).toBeChecked();

      // Same browser, a different session started separately: also on.
      await gotoRetry(page, second.url);
      await page.locator('#btn-settings').click();
      await expect(page.locator('#ctx-only-input')).toBeChecked();

      // Back off: the answer travels the other way too.
      await page.locator('#ctx-only-input').uncheck();
      await gotoRetry(page, first.url);
      await page.locator('#btn-settings').click();
      await expect(page.locator('#ctx-only-input')).not.toBeChecked();
    } finally {
      first.proc.kill('SIGTERM');
      second.proc.kill('SIGTERM');
      fs.rmSync(first.dir, { recursive: true, force: true });
      fs.rmSync(second.dir, { recursive: true, force: true });
    }
  });

  // The reset works by asking the surrounding page to type into the agent's
  // terminal, so a chat opened on its own cannot perform one. Routing there
  // would turn every message into an error and the chat would not work at all —
  // so outside an embedder the tick does nothing, even when it is on.
  test('with no embedder the tick is inert and messages send normally', async ({ page }) => {
    const server = await startServer();
    try {
      await gotoRetry(page, server.url);
      await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 10000 });
      await page.locator('#btn-settings').click();
      await page.locator('#ctx-only-input').check();
      await expect(page.locator('#ctx-only-input')).toBeChecked();
      await page.locator('#btn-settings-done').click();

      await page.locator('#chat-input').fill('hello with no parent frame');
      await page.locator('#chat-input').press('Enter');

      await expect(page.locator('.bubble.user', { hasText: 'hello with no parent frame' })).toBeVisible({ timeout: 10000 });
      await expect(page.locator('.bubble', { hasText: /parent frame not connected/i })).toHaveCount(0);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // A tapped chip is a message too. It reaches the send path by its own route,
  // so the tick has to be honoured there separately or half the chat quietly
  // behaves differently from the other half.
  test('conversation-context-only routes a quick-reply chip through the clear sequence', async ({ page }) => {
    const server = await startServer(['-welcome-replies', 'Give me an overview']);
    try {
      const frame = await embed(page, server.url);
      await expect(frame.locator('#quick-replies .chip')).toHaveCount(1, { timeout: 5000 });

      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').check();
      await frame.locator('#btn-settings-done').click();

      await frame.locator('#quick-replies .chip').click();

      await expect.poll(() => interrupts(page), { timeout: 5000 }).toEqual(['/clear']);
      await expect(frame.locator('.bubble.user', { hasText: 'Give me an overview' })).toBeVisible({ timeout: 10000 });
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      expect((await interrupts(page))[1]).toMatch(/^resume agent-chats\//);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // Speech recognition cannot be driven from a test, so the spoken path is
  // pinned at the decision the voice handler makes: what it hands the clear
  // sequence. The mic marker must survive into the recorded instruction — it is
  // what tells the wiped agent it is being spoken to — and a spoken interrupt
  // runs on ("stop, wrong file") where a typed one may not.
  test('conversation-context-only keeps the mic marker and spares run-on spoken interrupts', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);
      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').check();
      await frame.locator('#btn-settings-done').click();

      // The embedded copy, not the page around it: setContent leaves the outer
      // frame's URL pointing at the server too, and the routing deliberately
      // does nothing in a frame with no embedder above it.
      const inner = page.frames().find((f) => f !== page.mainFrame() && f.url().startsWith(server.url));
      const routed = await inner.evaluate(() => ({
        spoken: clearRouteText(VOICE_MARK + 'fix the logout bug', isInterruptPhrase('fix the logout bug', true)),
        runOn: clearRouteText(VOICE_MARK + 'stop, wrong file', isInterruptPhrase('stop, wrong file', true)),
        typedRunOn: clearRouteText('stop the retry loop', isInterruptPhrase('stop the retry loop', false)),
      }));

      expect(routed.spoken).toBe('/clear 🎤 fix the logout bug');
      expect(routed.runOn).toBe('🎤 stop, wrong file'); // untouched: an interrupt
      expect(routed.typedRunOn).toBe('/clear stop the retry loop'); // typed: work, not a stop
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // "stop" asks for an interrupt, not an erasure. Routing it through the clear
  // sequence would kill the agent the user was trying to get the attention of,
  // and there would be nothing left to answer the question that follows.
  test('conversation-context-only leaves interrupt words alone', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').check();
      await frame.locator('#btn-settings-done').click();

      await frame.locator('#chat-input').fill('stop');
      await frame.locator('#chat-input').press('Enter');

      await expect(frame.locator('.bubble.user', { hasText: 'stop' })).toBeVisible({ timeout: 10000 });
      // Exactly the interrupt the chat has always sent — the break-in that
      // reaches a busy agent — and no `/clear` in front of it.
      await expect.poll(() => interrupts(page), { timeout: 10000 })
        .toEqual(['check_messages; ask me how to proceed']);
      // And the box is free again straight away, so a second "stop" needs no
      // waiting: the reset route is what holds it, and this did not take it.
      await expect(frame.locator('#chat-input')).toHaveJSProperty('readOnly', false);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // With `/` registered as an autocomplete trigger — which is how agent-chat
  // runs inside swe-swe — a bare `/clear` still has its dropdown open when
  // Enter is pressed, because the trigger only dies on the first space. The
  // dropdown must not eat that Enter: a status-only dropdown ("No results")
  // has nothing to select, so the message would simply never send.
  test('bare /clear submits even with the autocomplete dropdown open', async ({ page }) => {
    const server = await startServer(['-autocomplete-triggers', '/=builtin:filepath']);
    try {
      const frame = await embed(page, server.url);

      // Typed character by character so the trigger fires exactly as it would
      // for a real user; fill() would set the value in one shot.
      await frame.locator('#chat-input').pressSequentially('/clear', { delay: 30 });
      await expect(frame.locator('#autocomplete-dropdown')).toHaveClass(/visible/, { timeout: 5000 });

      await frame.locator('#chat-input').press('Enter');
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      expect(await frame.locator('#chat-input').inputValue()).toBe('');
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });
});
