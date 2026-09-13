// @ts-check
// Unit-style tests for what hangs off a list ITEM: a block indented under the
// item belongs to it (CommonMark's continuation), and an ordered list that
// starts at 6 must still say 6.
//
// Both were reported from the same torture-test bubble: a quote indented under
// item 3 came out as literal "> " lines, and because those lines ended the
// list, every item after them restarted the numbering at 1.
//
// Driven through Playwright so we exercise the exact function shipped in
// client-dist/app.js, which lives on `window` as a classic script.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-md-cont-'));
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


test.describe('renderMarkdown — list item continuation and start numbers', () => {
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

  /** @param {import('@playwright/test').Page} page @param {string} md */
  async function render(page, md) {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    return page.evaluate((src) => window.renderMarkdown(src), md);
  }

  test('a quote indented under an item renders inside that item', async ({ page }) => {
    const html = await render(page, [
      '1. Item with a quote:',
      '',
      '    > Quoted line',
      '',
      '2. Next item',
    ].join('\n'));
    expect(html).toBe(
      '<ol><li>Item with a quote:<blockquote>Quoted line</blockquote></li>'
      + '<li>Next item</li></ol>'
    );
    // The marker must not survive as literal text.
    expect(html).not.toContain('&gt;');
  });

  test('a quoted list indented under an item keeps its own list', async ({ page }) => {
    const html = await render(page, [
      '- Item:',
      '',
      '    > - one',
      '    > - two',
    ].join('\n'));
    expect(html).toBe(
      '<ul><li>Item:<blockquote><ul><li>one</li><li>two</li></ul></blockquote></li></ul>'
    );
  });

  test('an indented block does not end the list', async ({ page }) => {
    const html = await render(page, [
      '1. one',
      '',
      '    > quoted',
      '',
      '1. two',
      '1. three',
    ].join('\n'));
    // One list, three items — not a list, a stray quote, then a fresh list.
    expect(html.match(/<ol/g)).toHaveLength(1);
    expect(html.match(/<li>/g)).toHaveLength(3);
  });

  test('plain indented text continues the item it sits under', async ({ page }) => {
    const html = await render(page, [
      '1. one',
      '',
      '    continued here',
      '',
      '2. two',
    ].join('\n'));
    expect(html).toBe('<ol><li>one<br>continued here</li><li>two</li></ol>');
  });

  test('an ordered list starting at 6 says 6', async ({ page }) => {
    const html = await render(page, '6. six\n7. seven');
    expect(html).toBe('<ol start="6"><li>six</li><li>seven</li></ol>');
  });

  test('an ordered list starting at 1 carries no start attribute (regression)', async ({ page }) => {
    const html = await render(page, '1. one\n1. two');
    expect(html).toBe('<ol><li>one</li><li>two</li></ol>');
  });

  test('a nested ordered list honours its own first marker', async ({ page }) => {
    const html = await render(page, '1. one\n    3. three');
    expect(html).toBe('<ol><li>one<ol start="3"><li>three</li></ol></li></ol>');
  });

  test('text left of the list still ends it (regression)', async ({ page }) => {
    const html = await render(page, '1. one\n    - inner\n\nAfter the list.');
    expect(html).toContain('<ol><li>one<ul><li>inner</li></ul></li></ol>');
    expect(html).toContain('After the list.');
  });

  test('the reported bubble: item 3 keeps its quote and item 4 keeps the list', async ({ page }) => {
    const html = await render(page, [
      '1. Third item with a quote hanging off it:',
      '',
      '    > Quote inside a list item',
      '    >',
      '    > - and a bullet inside the quote',
      '    > - second bullet',
      '',
      '4. Now the markers jump to 4 on purpose',
    ].join('\n'));
    expect(html).toContain('<blockquote>Quote inside a list item');
    expect(html).toContain('<ul><li>and a bullet inside the quote</li><li>second bullet</li></ul>');
    expect(html).not.toContain('&gt;');
    // Still one list: the quote did not split it in two.
    expect(html.match(/<ol/g)).toHaveLength(1);
  });
});
