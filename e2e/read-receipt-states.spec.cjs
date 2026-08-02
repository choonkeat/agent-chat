// @ts-check
// e2e coverage for when a user bubble stops being "unread".
//
// The bug this guards against: the user breaks out of the agent's blocking
// send_message in the host terminal, then replies in the chat. The server-side
// waiter is dead but still drains the queue, so the old UI flipped the bubble
// to read even though nothing received it — and removed the ⋯ menu (with it,
// "Send as interrupting") at the exact moment that escape hatch was needed.
//
// The fix keeps ONE unread state and moves the boundary: a bubble is
// .pending-agent until receipt is established. That happens two ways, and both
// are covered here:
//
//   1. The agent asks for the queue itself (check_messages). The call is
//      executing, so the agent is alive at that instant — the drain IS the
//      receipt and the bubble flips immediately.
//   2. A wait parked inside a blocking send_message drains the queue. That
//      waiter may be a zombie, so the bubble stays unread until the agent's
//      NEXT agent-chat call proves it arrived. Meanwhile data-handed-over
//      hides Delete, because an unsend can no longer pull the message back.
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
const SETTLE_MS = 800;

function startServer(extraFlags = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-receipt-'));
    fs.writeFileSync(path.join(dir, 'README.md'), '# README\n');

    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';

    const proc = spawn(bin, ['-no-stdio-mcp', ...extraFlags], {
      cwd: dir,
      env: cleanEnv,
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    let stderr = '';
    proc.stderr.on('data', (chunk) => {
      stderr += chunk.toString();
      const match = stderr.match(/Agent Chat UI: (http:\/\/localhost:\d+)/);
      if (match) {
        resolve({ url: match[1], proc, dir });
      }
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

async function setupPage(page, url) {
  await gotoRetry(page, url);
  const textarea = page.locator('#chat-input');
  await expect(textarea).toBeEnabled({ timeout: 5000 });
  await textarea.click();
  await page.waitForTimeout(SETTLE_MS);
  return textarea;
}

async function mcpCall(baseUrl, mountPath, toolName, args) {
  const body = {
    jsonrpc: '2.0',
    method: 'tools/call',
    params: { name: toolName, arguments: args || {} },
    id: Date.now(),
  };
  const res = await fetch(`${baseUrl}${mountPath}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Accept': 'application/json, text/event-stream',
    },
    body: JSON.stringify(body),
  });
  return res.text();
}

// Open the ⋯ menu on a bubble and report which rows it offers.
async function menuActionsFor(page, bubble) {
  await bubble.locator('.bubble-pending-menu').click({ force: true });
  const actions = await page.locator('.bubble-menu button[data-action]').evaluateAll(
    (els) => els.map((el) => el.getAttribute('data-action'))
  );
  // Close it again so the next open starts clean.
  await page.locator('#messages').click({ position: { x: 5, y: 5 } });
  await page.waitForTimeout(200);
  return actions;
}

test.describe('Unread until proven read', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('check_messages marks the bubble read on the spot', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    const sendBtn = page.locator('#btn-send');

    await textarea.fill('receipt states');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);

    const bubble = page.locator('.bubble.user', { hasText: 'receipt states' });

    // Queued: dim, below the loader, both menu rows on offer.
    await expect(bubble).toHaveClass(/pending-agent/);
    await expect(bubble).toHaveAttribute('title', /agent/i);
    expect(await menuActionsFor(page, bubble)).toEqual(['delete', 'interrupt']);
    const belowLoader = await page.evaluate(() => {
      const loader = document.getElementById('loading-bubble');
      const bubbles = Array.from(document.querySelectorAll('.bubble.user'));
      const b = bubbles[bubbles.length - 1];
      if (!loader || !b) return 'missing';
      return (loader.compareDocumentPosition(b) & 4) ? 'after' : 'before';
    });
    expect(belowLoader).toBe('after');
    await page.screenshot({ path: 'test-results/screenshots/20-receipt-queued.png', fullPage: true });

    // The agent asked for the queue itself, so it is provably alive right now:
    // ONE call is enough — no second call, no lag.
    await mcpCall(server.url, '/mcp', 'check_messages');
    await page.waitForTimeout(SETTLE_MS);

    await expect(bubble).not.toHaveClass(/pending-agent/);
    await expect(bubble).not.toHaveAttribute('title', /.*/);
    await expect(bubble.locator('.bubble-pending-menu')).toHaveCount(0);
    await page.screenshot({ path: 'test-results/screenshots/22-receipt-read.png', fullPage: true });
  });

  test('a parked send_message drain leaves the bubble unread until the next call', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    const sendBtn = page.locator('#btn-send');

    // Park a blocking send_message: deliberately NOT awaited, because it only
    // returns once the user replies below. This is the waiter that a terminal
    // break-out would turn into a zombie.
    const parked = mcpCall(server.url, '/mcp', 'send_message', {
      text: 'anything to add?',
      first_quick_reply: 'No',
    }).catch(() => {});
    await page.waitForTimeout(SETTLE_MS);

    await textarea.fill('handed to a parked waiter');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);

    const bubble = page.locator('.bubble.user', { hasText: 'handed to a parked waiter' });

    // Handed over, but nothing has proven it arrived: still dim, still
    // tooltipped, "⋯" intact — only Delete drops off.
    await expect(bubble).toHaveClass(/pending-agent/);
    await expect(bubble).toHaveAttribute('data-handed-over', '1');
    await expect(bubble.locator('.bubble-pending-menu')).toHaveCount(1);
    expect(await menuActionsFor(page, bubble)).toEqual(['interrupt']);
    const stillBelow = await page.evaluate(() => {
      const loader = document.getElementById('loading-bubble');
      const bubbles = Array.from(document.querySelectorAll('.bubble.user'));
      const b = bubbles[bubbles.length - 1];
      if (!loader || !b) return 'missing';
      return (loader.compareDocumentPosition(b) & 4) ? 'after' : 'before';
    });
    expect(stillBelow).toBe('after');
    await page.screenshot({ path: 'test-results/screenshots/21-receipt-handed-over.png', fullPage: true });

    // The agent's next agent-chat call is the proof.
    await mcpCall(server.url, '/mcp', 'check_messages');
    await page.waitForTimeout(SETTLE_MS);

    await expect(bubble).not.toHaveClass(/pending-agent/);
    await expect(bubble.locator('.bubble-pending-menu')).toHaveCount(0);
    await parked;
  });

  test('a reload mid-hand-over replays the bubble as unread, not read', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    const sendBtn = page.locator('#btn-send');

    // Hand over via a parked send_message — the one drain that does not prove
    // itself — and never prove it.
    const parked = mcpCall(server.url, '/mcp', 'send_message', {
      text: 'anything to add?',
      first_quick_reply: 'No',
    }).catch(() => {});
    await page.waitForTimeout(SETTLE_MS);

    await textarea.fill('survives a reload');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);
    await parked;

    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    await page.waitForTimeout(SETTLE_MS);

    // replayHistory rebuilds "unread, handed over" from a userMessagesConsumed
    // with no matching userMessagesRead.
    const replayed = page.locator('.bubble.user', { hasText: 'survives a reload' });
    await expect(replayed).toHaveCount(1);
    await expect(replayed).toHaveClass(/pending-agent/);
    await expect(replayed).toHaveAttribute('data-handed-over', '1');
    await expect(replayed.locator('.bubble-pending-menu')).toHaveCount(1);
    expect(await menuActionsFor(page, replayed)).toEqual(['interrupt']);
    await page.screenshot({ path: 'test-results/screenshots/23-receipt-unread-after-reload.png', fullPage: true });
  });
});
