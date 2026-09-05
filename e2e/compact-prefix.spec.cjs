// @ts-check
// Pins the `/compact <instruction>` sequence.
//
// `/compact` is `/clear`'s sibling: same three halves (reset the agent, record
// the instruction, wake the agent), but the agent keeps a summary of the
// conversation instead of losing it. Two things follow, and both are silent
// when they break. There is no resume line — naming the chat log would hand a
// second copy of the instruction to an agent that already has its context. And
// the wake-up line is held back, because it is typed through the interrupt
// channel (one Esc first) and an Esc that lands mid-summary cancels the very
// summary it was waiting for.
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { gotoRetry } = require('./goto-retry.cjs');
const { serverPort } = require('./server-port.cjs');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);

const NUDGE = 'agent-chat mcp: check_messages; report progress before you start processing';

function startServer(extraArgs = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-compact-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = serverPort();
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

// The embedded copy, not the page around it: setContent leaves the outer
// frame's URL pointing at the server too, and the reset route deliberately does
// nothing in a frame with no embedder above it.
const innerFrame = (page, url) =>
  page.frames().find((f) => f !== page.mainFrame() && f.url().startsWith(url));

// Loads agent-chat in an iframe under a parent that records postMessages, and
// shortens the post-summary wait so the wake-up line arrives within the test.
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
  await innerFrame(page, url).evaluate(() => { window.compactNudgeDelayMs = 50; });
  return frame;
}

const interrupts = (page) =>
  page.evaluate(() => window.__msgs.filter((m) => m.type === 'agent-chat-interrupt').map((m) => m.text));

test.describe('/compact prefix', () => {
  test('summarises first, records the command and instruction, then wakes the agent', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#chat-input').fill('/compact-and-then now fix the logout bug');
      await frame.locator('#chat-input').press('Enter');

      // 1. The summary goes out immediately — before anything is recorded, so a
      //    still-running agent cannot eat the instruction first.
      await expect.poll(() => interrupts(page), { timeout: 5000 }).toEqual(['/compact']);

      // 2. The stripped instruction lands in the chat as the user's own bubble,
      //    unread: it is waiting in the queue for the agent the nudge wakes.
      const userBubble = frame.locator('.bubble.user', { hasText: 'now fix the logout bug' });
      await expect(userBubble).toBeVisible({ timeout: 10000 });
      await expect(userBubble.locator('.reset-cmd')).toHaveText('/compact-and-then');
      await expect(userBubble).toContainText('now fix the logout bug');
      await expect(userBubble).toHaveClass(/pending-agent/);

      // 3. Only then the wake-up line — and it is the ordinary nudge, not a
      //    resume line: the agent kept its context, so there is no file to name
      //    and no second copy of the instruction to trip over.
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      const [, wake] = await interrupts(page);
      expect(wake).toBe(NUDGE);
      expect(wake).not.toContain('resume ');
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('bare /compact summarises and wakes with no instruction, and is on the record', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#chat-input').fill('/compact');
      await frame.locator('#chat-input').press('Enter');

      // Nothing for the agent, but the wake-up line must still be typed —
      // otherwise the agent sits there and the chat looks dead. The reset itself
      // is a line in the log and a bubble on screen.
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      const bare = frame.locator('.bubble.user');
      await expect(bare).toHaveCount(1, { timeout: 10000 });
      await expect(bare.locator('.reset-cmd')).toHaveText('/compact');
      const [summarise, wake] = await interrupts(page);
      expect(summarise).toBe('/compact');
      expect(wake).toBe(NUDGE);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // "Conversation context only" turns every ordinary message into a `/clear …`.
  // A `/compact …` is already a reset the user asked for by name, so prefixing
  // it would bury `/compact` inside the instruction and wipe what the user
  // asked to keep.
  test('conversation-context-only leaves a /compact-and-then alone', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);
      await frame.locator('#btn-settings').click();
      await frame.locator('#ctx-only-input').check();
      await frame.locator('#btn-settings-done').click();

      const routed = await innerFrame(page, server.url).evaluate(() => ({
        compact: clearRouteText('/compact-and-then fix the logout bug', false),
        ordinary: clearRouteText('fix the logout bug', false),
      }));

      expect(routed.compact).toBe('/compact-and-then fix the logout bug');
      expect(routed.ordinary).toBe('/clear-and-then fix the logout bug');
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // With `/` registered as an autocomplete trigger — which is how agent-chat
  // runs inside swe-swe — a bare `/compact` still has its dropdown open when
  // Enter is pressed, because the trigger only dies on the first space. The
  // dropdown must not eat that Enter, or the message never sends.
  test('bare /compact submits even with the autocomplete dropdown open', async ({ page }) => {
    const server = await startServer(['-autocomplete-triggers', '/=builtin:filepath']);
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#chat-input').pressSequentially('/compact', { delay: 30 });
      await expect(frame.locator('#autocomplete-dropdown')).toHaveClass(/visible/, { timeout: 5000 });

      await frame.locator('#chat-input').press('Enter');
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      expect((await interrupts(page))[0]).toBe('/compact');
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });
});
