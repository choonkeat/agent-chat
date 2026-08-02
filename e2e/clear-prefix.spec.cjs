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

  // The session decides what a browser that has never touched the box starts
  // with. That is how swe-swe can hand out a context-only session without
  // anyone ticking anything, and unticking still has to win — a default is a
  // starting point, not a lock.
  test('-conversation-context-only starts ticked, and unticking beats it', async ({ page }) => {
    const server = await startServer(['-conversation-context-only']);
    try {
      const frame = await embed(page, server.url);
      await frame.locator('#btn-settings').click();
      await expect(frame.locator('#ctx-only-input')).toBeChecked();

      await frame.locator('#btn-settings-done').click();
      await frame.locator('#chat-input').fill('first message');
      await frame.locator('#chat-input').press('Enter');
      await expect.poll(() => interrupts(page), { timeout: 5000 }).toEqual(['/clear']);
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);

      // Untick: this browser has now answered, and its answer outranks the
      // session's opening position.
      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').uncheck();
      await frame.locator('#btn-settings-done').click();
      await frame.locator('#chat-input').fill('second message');
      await frame.locator('#chat-input').press('Enter');
      await expect(frame.locator('.bubble.user', { hasText: 'second message' })).toBeVisible({ timeout: 10000 });
      // Still the two from the first message — no third wipe.
      expect(await interrupts(page)).toHaveLength(2);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // Cookies ignore port numbers, so without the session key in the cookie's
  // name one tick would arm every agent-chat on the host — including sessions
  // started later, whose agents would start losing their memory unasked. The
  // message-style cookie is deliberately shared this way; this one must not be.
  test('the tick belongs to its own session, not to every chat in the browser', async ({ page }) => {
    const armed = await startServer();
    const other = await startServer();
    try {
      await gotoRetry(page, armed.url);
      await page.locator('#btn-settings').click();
      await page.locator('#ctx-only-input').check();
      await expect(page.locator('#ctx-only-input')).toBeChecked();

      // Same browser, different session: untouched.
      await gotoRetry(page, other.url);
      await page.locator('#btn-settings').click();
      await expect(page.locator('#ctx-only-input')).not.toBeChecked();

      // And the session that was ticked still is.
      await gotoRetry(page, armed.url);
      await page.locator('#btn-settings').click();
      await expect(page.locator('#ctx-only-input')).toBeChecked();
    } finally {
      armed.proc.kill('SIGTERM');
      other.proc.kill('SIGTERM');
      fs.rmSync(armed.dir, { recursive: true, force: true });
      fs.rmSync(other.dir, { recursive: true, force: true });
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

      const inner = page.frames().find((f) => f.url().startsWith(server.url));
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
      // The ordinary path nudges the agent to check_messages; a wipe would have
      // put a bare `/clear` on the wire first.
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(1);
      expect((await interrupts(page))[0]).not.toBe('/clear');
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
