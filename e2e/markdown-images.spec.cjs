// @ts-check
// Unit-style tests for client-side renderMarkdown(), driven through Playwright
// so we exercise the exact function shipped in client-dist/app.js. The function
// is declared at the top level of a classic script, so it lives on `window`.
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);

function startServer() {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-md-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';

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

test.describe('renderMarkdown — images', () => {
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

  test('![alt](url) renders an <img> tag', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await page.evaluate(() =>
      window.renderMarkdown('![Cat](https://example.com/cat.png)')
    );
    expect(html).toContain('<img');
    expect(html).toContain('src="https://example.com/cat.png"');
    expect(html).toContain('alt="Cat"');
    // The literal "!" must not leak through as text in front of a link.
    expect(html).not.toMatch(/!<a /);
  });

  test('![](url) renders <img> with empty alt', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await page.evaluate(() =>
      window.renderMarkdown('![](https://example.com/x.png)')
    );
    expect(html).toContain('<img');
    expect(html).toContain('src="https://example.com/x.png"');
    expect(html).toContain('alt=""');
  });

  test('plain [text](url) link still renders (regression)', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await page.evaluate(() =>
      window.renderMarkdown('[Google](https://www.google.com)')
    );
    expect(html).toContain('<a href="https://www.google.com"');
    expect(html).toContain('>Google</a>');
    expect(html).not.toContain('<img');
  });

  test('relative path: ![](/foo.png) renders <img> too', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await page.evaluate(() =>
      window.renderMarkdown('![](/repos/agent-chat/workspace/diagram.png)')
    );
    expect(html).toContain('<img');
    expect(html).toContain('src="/repos/agent-chat/workspace/diagram.png"');
    expect(html).toContain('alt=""');
  });

  test('javascript: URL is rejected (no <img> emitted)', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await page.evaluate(() =>
      window.renderMarkdown('![x](javascript:alert(1))')
    );
    expect(html).not.toContain('<img');
    expect(html).not.toContain('javascript:');
  });

  test('mixed: image and link together', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await page.evaluate(() =>
      window.renderMarkdown('See ![diagram](https://example.com/d.png) and [docs](https://example.com).')
    );
    expect(html).toContain('<img');
    expect(html).toContain('src="https://example.com/d.png"');
    expect(html).toContain('alt="diagram"');
    expect(html).toContain('<a href="https://example.com"');
  });
});
