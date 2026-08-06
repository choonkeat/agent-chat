// @ts-check
// e2e coverage for the done-ding: the short tone played when a long run ends
// and the Send button turns from amber back to blue.
//
//   1. A run past the threshold dings, exactly once.
//   2. A run under the threshold stays silent (no ding per one-liner).
//   3. Progress updates mid-run redraw the loader without restarting the
//      clock, so a long run interrupted by progress still dings.
//   4. History streaming in on connect never dings, however long the runs in
//      it were.
//   5. Switched off in Settings, nothing sounds.
//
// These drive the real (embedded) client functions directly and stub playDing,
// because asserting on audio output is not something a browser test can do —
// the decision to play is the behaviour worth pinning.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-ding-'));
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

// Counts decisions to play instead of playing, and shortens the threshold so a
// "long" run is a test-length one.
async function setupPage(page, url, thresholdMs = 50) {
  await gotoRetry(page, url);
  await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
  await page.waitForTimeout(SETTLE_MS);
  await page.evaluate((ms) => {
    window.__dings = 0;
    window.playDing = function () { window.__dings++; };
    window.DING_MIN_MS = ms;
    window.historyStreaming = false;
    window.busySince = 0;
    window.removeLoading();
    window.__dings = 0; // the reset above must not count
  }, thresholdMs);
}

const dings = (page) => page.evaluate(() => window.__dings);

test.describe('Done-ding on long runs', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('a run past the threshold dings once when the button goes blue', async ({ page }) => {
    await setupPage(page, server.url);

    await page.evaluate(() => window.showLoading());
    await expect(page.locator('#btn-send')).toHaveClass(/agent-busy/);
    await page.waitForTimeout(200);
    await page.evaluate(() => window.removeLoading());

    await expect(page.locator('#btn-send')).not.toHaveClass(/agent-busy/);
    expect(await dings(page)).toBe(1);

    // Idle now: a second removeLoading has no run to end.
    await page.evaluate(() => window.removeLoading());
    expect(await dings(page)).toBe(1);
  });

  test('a run under the threshold stays silent', async ({ page }) => {
    await setupPage(page, server.url, 5000);

    await page.evaluate(() => window.showLoading());
    await page.waitForTimeout(100);
    await page.evaluate(() => window.removeLoading());

    expect(await dings(page)).toBe(0);
  });

  test('progress updates mid-run do not restart the clock', async ({ page }) => {
    await setupPage(page, server.url, 300);

    await page.evaluate(() => window.showLoading());
    await page.waitForTimeout(200);
    await page.evaluate(() => window.showLoading()); // progress update redraw
    await page.waitForTimeout(200);
    await page.evaluate(() => window.removeLoading());

    // Neither half alone passes 300ms; the run as a whole does.
    expect(await dings(page)).toBe(1);
  });

  test('history streaming in on connect never dings', async ({ page }) => {
    await setupPage(page, server.url);

    await page.evaluate(() => {
      window.historyStreaming = true;
      window.showLoading();
    });
    await page.waitForTimeout(200);
    await page.evaluate(() => window.removeLoading());

    expect(await dings(page)).toBe(0);
  });

  test('switched off in Settings, nothing sounds', async ({ page }) => {
    await setupPage(page, server.url);
    await page.evaluate(() => window.setDing(false));

    await page.evaluate(() => window.showLoading());
    await page.waitForTimeout(200);
    await page.evaluate(() => window.removeLoading());

    expect(await dings(page)).toBe(0);

    // And the box reflects it when the panel is opened.
    await page.locator('#btn-settings').click();
    await expect(page.locator('#ding-input')).not.toBeChecked();
  });

  test('the box is on by default and survives a reload', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    await page.locator('#btn-settings').click();
    await expect(page.locator('#ding-input')).toBeChecked();

    await page.locator('#ding-input').uncheck();
    await gotoRetry(page, server.url);
    await page.locator('#btn-settings').click();
    await expect(page.locator('#ding-input')).not.toBeChecked();
  });
});
