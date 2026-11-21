// @ts-check
// e2e coverage for the "What's new" button in the chat header.
//
//   1. A browser that has never seen the button gets no dot: the seen-version
//      cookie is set quietly to the current release.
//   2. A cookie naming an older release lights the dot; opening the panel
//      shows the embedded CHANGELOG, clears the dot, and a reload keeps it
//      cleared.
//   3. Opening Settings puts the What's new panel away, and vice versa.
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
const SETTLE_MS = 800;

function startServer(extraFlags = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-whatsnew-'));
    fs.writeFileSync(path.join(dir, 'README.md'), '# README\n');

    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = serverPort();

    const proc = spawn(bin, ['-no-stdio-mcp', ...extraFlags], {
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

const COOKIE = 'agent-chat-seen-version';

async function load(page, url) {
  await gotoRetry(page, url);
  await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
}

async function seenCookie(page) {
  const cookies = await page.context().cookies();
  const c = cookies.find((x) => x.name === COOKIE);
  return c ? decodeURIComponent(c.value) : null;
}

test.describe("What's new", () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('a first visit shows no dot and remembers the current release', async ({ page }) => {
    await load(page, server.url);
    const latest = await page.evaluate(() => window.WHATS_NEW_LATEST);
    expect(latest).toMatch(/^\d+\.\d+\.\d+/);
    await expect(page.locator('#btn-whats-new')).toBeVisible();
    await expect(page.locator('#whats-new-dot')).toBeHidden();
    expect(await seenCookie(page)).toBe(latest);
  });

  test('an older seen release lights the dot until the panel is opened', async ({ page }) => {
    await page.context().addCookies([{ name: COOKIE, value: '0.0.1', url: server.url }]);
    await load(page, server.url);
    const latest = await page.evaluate(() => window.WHATS_NEW_LATEST);

    await expect(page.locator('#whats-new-dot')).toBeVisible();
    await expect(page.locator('#whats-new-panel')).toBeHidden();

    await page.locator('#btn-whats-new').click();
    await expect(page.locator('#whats-new-panel')).toBeVisible();
    await expect(page.locator('#whats-new-body h2').first()).toContainText('[' + latest + ']');
    await expect(page.locator('#whats-new-version')).toHaveText('v' + latest);
    await expect(page.locator('#whats-new-dot')).toBeHidden();
    expect(await seenCookie(page)).toBe(latest);

    await page.locator('#btn-whats-new-done').click();
    await expect(page.locator('#whats-new-panel')).toBeHidden();

    await load(page, server.url);
    await expect(page.locator('#whats-new-dot')).toBeHidden();
  });

  test('only one of Settings and What\'s new is open at a time', async ({ page }) => {
    await load(page, server.url);
    await page.locator('#btn-whats-new').click();
    await expect(page.locator('#whats-new-panel')).toBeVisible();

    await page.locator('#btn-settings').click();
    await expect(page.locator('#settings-panel')).toBeVisible();
    await expect(page.locator('#whats-new-panel')).toBeHidden();

    await page.locator('#btn-whats-new').click();
    await expect(page.locator('#whats-new-panel')).toBeVisible();
    await expect(page.locator('#settings-panel')).toBeHidden();
  });
});
