// @ts-check
// e2e coverage for the "unsend pending message" flow:
//   - A user bubble in .pending-agent state shows a "⋯" actions button
//   - The menu offers Delete and Send as interrupting
//   - Clicking Delete removes the bubble locally and from the agent's queue
//   - A consumed (non-pending) bubble exposes no ⋯ menu at all
//   - The agent's subsequent check_messages does NOT see the unsent text
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-unsend-'));
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
    // Fresh context + page per test. Eliminates cross-test state bleed
    // in the shared CDP browser (every spec used to grab pages[0], so 22
    // tests across 5 files all ran in the same tab — leftover navigation,
    // fetches, and event listeners caused intermittent flake). Trade-off:
    // tests no longer reuse the pre-existing tab visible in Agent View.
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
  await page.goto(url);
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

test.describe('Unsend pending user message', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('Delete on a pending bubble removes it and the agent never sees it', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    const sendBtn = page.locator('#btn-send');

    // Send a message — it will land in .pending-agent state.
    await textarea.fill('please unsend me');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);

    const userBubble = page.locator('.bubble.user.pending-agent', { hasText: 'please unsend me' });
    await expect(userBubble).toHaveCount(1);

    // The "⋯" actions control should be present and clickable.
    const menuBtn = userBubble.locator('.bubble-pending-menu');
    await expect(menuBtn).toHaveCount(1);
    // Make it visible (it's hover-only by default) for the screenshot.
    await page.evaluate(() => {
      const b = document.querySelector('.bubble.user.pending-agent .bubble-pending-menu');
      if (b) b.style.opacity = '1';
    });
    await page.screenshot({ path: 'test-results/screenshots/05-unsend-button-on-pending.png', fullPage: true });

    // Open the menu and click Delete.
    await menuBtn.click({ force: true });
    const deleteItem = page.locator('.bubble-menu button[data-action="delete"]');
    await expect(deleteItem).toHaveCount(1);
    await deleteItem.click();
    await page.waitForTimeout(SETTLE_MS);

    // Bubble should be gone from the DOM.
    await expect(page.locator('.bubble.user', { hasText: 'please unsend me' })).toHaveCount(0);
    await page.screenshot({ path: 'test-results/screenshots/06-after-unsend-bubble-gone.png', fullPage: true });

    // The agent draining the queue right after must NOT see the message.
    const result = await mcpCall(server.url, '/mcp', 'check_messages');
    expect(result).not.toContain('please unsend me');
  });

  test('the ⋯ menu on a pending bubble offers Delete and Send as interrupting', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    const sendBtn = page.locator('#btn-send');

    await textarea.fill('two options please');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);

    const userBubble = page.locator('.bubble.user.pending-agent', { hasText: 'two options please' });
    await expect(userBubble).toHaveCount(1);

    await userBubble.locator('.bubble-pending-menu').click({ force: true });
    await expect(page.locator('.bubble-menu button[data-action="delete"]')).toHaveCount(1);
    await expect(page.locator('.bubble-menu button[data-action="interrupt"]')).toHaveCount(1);
    await expect(page.locator('.bubble-menu button[data-action="interrupt"]')).toHaveText(/Send as interrupting/);
  });

  test('with several pending bubbles, only the bottom-most offers Send as interrupting', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    const sendBtn = page.locator('#btn-send');

    // Queue two messages without draining — both land in .pending-agent.
    await textarea.fill('first queued');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);
    await textarea.fill('second queued');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);

    const first = page.locator('.bubble.user.pending-agent', { hasText: 'first queued' });
    const second = page.locator('.bubble.user.pending-agent', { hasText: 'second queued' });
    await expect(first).toHaveCount(1);
    await expect(second).toHaveCount(1);

    // The EARLIER bubble: Delete only, no interrupt (interrupt drains the whole
    // queue, so it only belongs on the newest pending bubble).
    await first.locator('.bubble-pending-menu').click({ force: true });
    await expect(page.locator('.bubble-menu button[data-action="delete"]')).toHaveCount(1);
    await expect(page.locator('.bubble-menu button[data-action="interrupt"]')).toHaveCount(0);
    // Close the menu before opening the next one.
    await page.locator('#messages').click({ position: { x: 5, y: 5 } });
    await page.waitForTimeout(200);

    // The BOTTOM-MOST bubble: both Delete and Send as interrupting.
    await second.locator('.bubble-pending-menu').click({ force: true });
    await expect(page.locator('.bubble-menu button[data-action="delete"]')).toHaveCount(1);
    await expect(page.locator('.bubble-menu button[data-action="interrupt"]')).toHaveCount(1);
  });

  test('a consumed (non-pending) bubble does not expose the ⋯ menu', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    const sendBtn = page.locator('#btn-send');

    await textarea.fill('already seen');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);

    // Drain so the bubble flips to consumed.
    await mcpCall(server.url, '/mcp', 'check_messages');
    await page.waitForTimeout(SETTLE_MS);

    const consumed = page.locator('.bubble.user', { hasText: 'already seen' });
    await expect(consumed).toHaveCount(1);
    await expect(consumed).not.toHaveClass(/pending-agent/);
    // The ⋯ control must NOT be present on consumed bubbles — the agent
    // has already processed the text, so Delete/interrupt would be misleading.
    await expect(consumed.locator('.bubble-pending-menu')).toHaveCount(0);
  });
});
