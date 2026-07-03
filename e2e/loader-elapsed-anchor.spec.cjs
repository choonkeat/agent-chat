// @ts-check
// e2e coverage for the loading indicator's elapsed-tick anchoring:
//   1. showLoading() anchors the tick to the previous speech bubble's
//      timestamp (lastBubbleTs), NOT Date.now(), so on reconnect/replay the
//      counter reflects real elapsed instead of restarting at 0s.
//   2. With no prior bubble it falls back to Date.now().
//   3. A new bubble arriving while the loader is up re-anchors the tick to
//      that bubble's timestamp (no double-counting the elapsed-time separator).
//
// These drive the real (embedded) client functions directly with controlled
// timestamps so the assertions are deterministic — we check dataset.loaderStart
// equality rather than rendered wall-clock seconds.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-loader-'));
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

async function setupPage(page, url) {
  await page.goto(url);
  const textarea = page.locator('#chat-input');
  await expect(textarea).toBeEnabled({ timeout: 5000 });
  await page.waitForTimeout(SETTLE_MS);
  return textarea;
}

test.describe('Loading indicator elapsed-tick anchoring', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('showLoading anchors loaderStart to the previous bubble timestamp, not now', async ({ page }) => {
    await setupPage(page, server.url);

    // Simulate a prior speech bubble that landed 65s ago, then show the loader
    // as if the agent just started working on it (the reconnect/replay case).
    const result = await page.evaluate(() => {
      const now = Date.now();
      const prior = now - 65000; // 65s ago
      window.lastBubbleTs = prior;
      window.showLoading();
      const el = document.getElementById('loading-bubble');
      return {
        prior,
        loaderStart: Number(el.dataset.loaderStart),
        elapsedText: el.querySelector('.elapsed').textContent,
      };
    });

    // Anchored to the prior bubble, NOT ~now.
    expect(result.loaderStart).toBe(result.prior);
    // And the rendered tick reflects the real elapsed (>1 minute), proving it
    // did not restart at 0s.
    expect(result.elapsedText).toMatch(/^1m /);
  });

  test('showLoading falls back to Date.now() when there is no prior bubble', async ({ page }) => {
    await setupPage(page, server.url);

    const result = await page.evaluate(() => {
      window.lastBubbleTs = 0; // fresh session, nothing before the loader
      const before = Date.now();
      window.showLoading();
      const el = document.getElementById('loading-bubble');
      return {
        before,
        after: Date.now(),
        loaderStart: Number(el.dataset.loaderStart),
        elapsedText: el.querySelector('.elapsed').textContent,
      };
    });

    // loaderStart stamped ~now (between the before/after brackets).
    expect(result.loaderStart).toBeGreaterThanOrEqual(result.before);
    expect(result.loaderStart).toBeLessThanOrEqual(result.after);
    expect(result.elapsedText).toBe('0s');
  });

  test('a new bubble arriving while the loader is up re-anchors the tick', async ({ page }) => {
    await setupPage(page, server.url);

    const result = await page.evaluate(() => {
      const now = Date.now();
      const oldTs = now - 120000; // loader initially anchored 2m ago
      window.lastBubbleTs = oldTs;
      window.showLoading();
      const el = document.getElementById('loading-bubble');
      const anchoredOld = Number(el.dataset.loaderStart);

      // A mid-turn progress bubble arrives with a fresh timestamp.
      const progressTs = Date.now();
      // addBubble(text, role, files, extraClass, timestamp, messageId, seq, forkable)
      window.addBubble('still working…', 'agent', null, null, progressTs, undefined, undefined, false);

      return {
        oldTs,
        anchoredOld,
        progressTs,
        reAnchored: Number(el.dataset.loaderStart),
        elapsedText: el.querySelector('.elapsed').textContent,
      };
    });

    // Loader started anchored to the old timestamp…
    expect(result.anchoredOld).toBe(result.oldTs);
    // …and re-anchored to the progress bubble's timestamp once it arrived.
    expect(result.reAnchored).toBe(result.progressTs);
    // So the tick counts forward from the progress bubble (≈0s), not 2m.
    expect(result.elapsedText).toBe('0s');
  });
});
