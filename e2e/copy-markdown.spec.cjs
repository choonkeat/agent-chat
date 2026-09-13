// @ts-check
// "Copy as markdown" — the one action every bubble's ⋯ menu offers.
//
// What the clipboard ends up holding is the markdown SOURCE the bubble was
// rendered from, not its rendered text: `**bold**` must survive the round trip.
// The page tries navigator.clipboard.writeText first and falls back to
// document.execCommand('copy') when the embedding page's Permissions Policy
// blocks it — these tests drive both routes by stubbing the first one.
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

function startServer() {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-copy-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = serverPort();

    const proc = spawn(bin, ['-no-stdio-mcp'], {
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

// Replace navigator.clipboard.writeText with a recorder. `reject` makes it
// behave like a Permissions-Policy-blocked iframe, which is what forces the
// execCommand fallback.
async function stubClipboard(page, { reject = false } = {}) {
  await page.evaluate((shouldReject) => {
    window.__copied = [];
    window.__execCopied = [];
    // execCommand('copy') lifts from the live selection, so record that too —
    // a headless copy may be a no-op even when the call returns true.
    const realExec = document.execCommand.bind(document);
    document.execCommand = function (cmd) {
      if (cmd === 'copy') {
        const el = document.activeElement;
        window.__execCopied.push(el && 'value' in el ? el.value : '');
        return true;
      }
      return realExec.apply(document, arguments);
    };
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: (t) => {
          if (shouldReject) return Promise.reject(new Error('NotAllowedError'));
          window.__copied.push(t);
          return Promise.resolve();
        },
      },
    });
  }, reject);
}

test.describe('Copy as markdown', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => {
    server = await startServer();
  });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('agent bubble copies the markdown source, not the rendered text', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    await stubClipboard(page);

    await page.evaluate(() => window.addAgentMessage('**bold** and `code`', null, null, Date.now()));
    // The rendered bubble shows "bold and code"; the clipboard must not.
    await page.locator('.bubble.agent .bubble-menu-btn').click();
    await page.locator('.bubble-menu [data-action="copy"]').click();

    expect(await page.evaluate(() => window.__copied)).toEqual(['**bold** and `code`']);
  });

  test('user bubble copies its markdown source too', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    await stubClipboard(page);

    await page.evaluate(() => window.addUserMessage('- one\n- two', null, null, Date.now()));
    await page.locator('.bubble.user .bubble-pending-menu').click({ force: true });
    await page.locator('.bubble-menu [data-action="copy"]').click();

    expect(await page.evaluate(() => window.__copied)).toEqual(['- one\n- two']);
  });

  test('falls back to execCommand when the clipboard API is blocked', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    await stubClipboard(page, { reject: true });

    await page.evaluate(() => window.addAgentMessage('fallback me', null, null, Date.now()));
    await page.locator('.bubble.agent .bubble-menu-btn').click();
    await page.locator('.bubble-menu [data-action="copy"]').click();

    await expect.poll(() => page.evaluate(() => window.__execCopied)).toEqual(['fallback me']);
    // The scratch textarea the fallback selects from must not be left behind.
    expect(await page.evaluate(() =>
      document.querySelectorAll('textarea[readonly]').length)).toBe(0);
  });

  test('copying confirms with a toast, and the menu closes', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    await stubClipboard(page);

    await page.evaluate(() => window.addAgentMessage('toast me', null, null, Date.now()));
    await page.locator('.bubble.agent .bubble-menu-btn').click();
    await page.locator('.bubble-menu [data-action="copy"]').click();

    await expect(page.locator('#copy-toast')).toHaveText('Copied as markdown');
    await expect(page.locator('#copy-toast')).toHaveClass(/show/);
    await expect(page.locator('.bubble-menu')).toHaveCount(0);
  });

  // The downloadable HTML export is a third surface: a static file with no
  // websocket and no app.js, carrying its own inline copy of the menu. It must
  // offer the same Copy as markdown, from markdown stashed on each bubble.
  test('the downloadable HTML export offers Copy as markdown on every bubble', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => {
      window.addAgentMessage('**bold** in the export', null, null, Date.now());
      window.addUserMessage('- one\n- two', null, null, Date.now());
    });
    const html = await page.evaluate(() =>
      window.buildExportHtml({ imageMode: 'thumbnail' }));

    // Open the export as its own document — no server, no app.js, exactly what
    // a user gets after clicking Download.
    const exported = await page.context().newPage();
    try {
      await exported.setContent(html);
      await stubClipboard(exported);

      // Every bubble, agent and user alike, carries the button.
      const bubbles = await exported.locator('.bubble').count();
      expect(bubbles).toBeGreaterThanOrEqual(2);
      await expect(exported.locator('.bubble .bubble-menu-btn')).toHaveCount(bubbles);

      // Agent bubble: Copy plus Speak aloud, and Copy yields the source.
      await exported.locator('.bubble.agent', { hasText: 'in the export' })
        .locator('.bubble-menu-btn').click();
      await expect(exported.locator('.bubble-menu [data-action="copy"]')).toHaveCount(1);
      await expect(exported.locator('.bubble-menu [data-action="speak"]')).toHaveCount(1);
      await exported.locator('.bubble-menu [data-action="copy"]').click();
      expect(await exported.evaluate(() => window.__copied)).toEqual(['**bold** in the export']);

      // User bubble: Copy only — there is nothing to speak on the user's side.
      await exported.locator('.bubble.user').locator('.bubble-menu-btn').click({ force: true });
      await expect(exported.locator('.bubble-menu [data-action="speak"]')).toHaveCount(0);
      await exported.locator('.bubble-menu [data-action="copy"]').click();
      expect(await exported.evaluate(() => window.__copied)).toEqual(
        ['**bold** in the export', '- one\n- two']);
    } finally {
      await exported.close().catch(() => {});
    }
  });

  test('the export falls back to execCommand when the clipboard API is blocked', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('export fallback', null, null, Date.now()));
    const html = await page.evaluate(() =>
      window.buildExportHtml({ imageMode: 'thumbnail' }));

    const exported = await page.context().newPage();
    try {
      await exported.setContent(html);
      await stubClipboard(exported, { reject: true });

      await exported.locator('.bubble.agent', { hasText: 'export fallback' })
        .locator('.bubble-menu-btn').click();
      await exported.locator('.bubble-menu [data-action="copy"]').click();

      await expect.poll(() => exported.evaluate(() => window.__execCopied))
        .toEqual(['export fallback']);
      await expect(exported.locator('#copy-toast')).toHaveText('Copied as markdown');
    } finally {
      await exported.close().catch(() => {});
    }
  });

  test('a failed copy says so instead of claiming success', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    // Both routes refuse: writeText rejects and execCommand reports false.
    await page.evaluate(() => {
      document.execCommand = () => false;
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: { writeText: () => Promise.reject(new Error('NotAllowedError')) },
      });
    });

    await page.evaluate(() => window.addAgentMessage('no clipboard here', null, null, Date.now()));
    await page.locator('.bubble.agent .bubble-menu-btn').click();
    await page.locator('.bubble-menu [data-action="copy"]').click();

    await expect(page.locator('#copy-toast')).toHaveText('Copy failed');
    await expect(page.locator('#copy-toast')).toHaveClass(/failed/);
  });
});
