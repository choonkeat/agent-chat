// @ts-check
// Unit-style tests for client-side renderMarkdown() list handling, driven
// through Playwright so we exercise the exact function shipped in
// client-dist/app.js. The function is declared at the top level of a classic
// script, so it lives on `window`.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-md-list-'));
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

test.describe('renderMarkdown — nested lists', () => {
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

  test('flat ordered list still renders (regression)', async ({ page }) => {
    const html = await render(page, '1. one\n2. two\n3. three');
    expect(html).toBe('<ol><li>one</li><li>two</li><li>three</li></ol>');
  });

  test('flat unordered list still renders (regression)', async ({ page }) => {
    const html = await render(page, '- one\n- two\n* three');
    expect(html).toBe('<ul><li>one</li><li>two</li><li>three</li></ul>');
  });

  test('ordered list nested under an ordered list', async ({ page }) => {
    const html = await render(page, [
      '1. Level one',
      '    1. This is one dot one',
      '    1. This is one dot two',
      '1. Level two',
      '    1. This is two dot one',
    ].join('\n'));
    expect(html).toBe(
      '<ol>'
      + '<li>Level one<ol><li>This is one dot one</li><li>This is one dot two</li></ol></li>'
      + '<li>Level two<ol><li>This is two dot one</li></ol></li>'
      + '</ol>'
    );
  });

  test('unordered list nested under an ordered item', async ({ page }) => {
    const html = await render(page, [
      '1. Level three',
      '    - This is three dot something',
      '    - This is three dot next thing',
    ].join('\n'));
    expect(html).toBe(
      '<ol><li>Level three<ul>'
      + '<li>This is three dot something</li>'
      + '<li>This is three dot next thing</li>'
      + '</ul></li></ol>'
    );
  });

  test('the whole reported example renders as one nested tree', async ({ page }) => {
    const html = await render(page, [
      '1. Level one',
      '    1. This is one dot one',
      '    1. This is one dot two',
      '1. Level two',
      '    1. This is two dot one',
      '1. Level three',
      '    - This is three dot something',
      '    - This is three dot next thing',
    ].join('\n'));
    expect(html).toBe(
      '<ol>'
      + '<li>Level one<ol><li>This is one dot one</li><li>This is one dot two</li></ol></li>'
      + '<li>Level two<ol><li>This is two dot one</li></ol></li>'
      + '<li>Level three<ul><li>This is three dot something</li><li>This is three dot next thing</li></ul></li>'
      + '</ol>'
    );
    // No stray <br> or leaked "1." text between items.
    expect(html).not.toContain('<br>');
    expect(html).not.toContain('1. This');
  });

  test('three levels deep nest', async ({ page }) => {
    const html = await render(page, [
      '- a',
      '  - b',
      '    - c',
      '- d',
    ].join('\n'));
    expect(html).toBe(
      '<ul><li>a<ul><li>b<ul><li>c</li></ul></li></ul></li><li>d</li></ul>'
    );
  });

  test('nested items keep inline formatting', async ({ page }) => {
    const html = await render(page, '1. outer\n    - **bold** and `code`');
    expect(html).toContain('<strong>bold</strong>');
    expect(html).toContain('<code>code</code>');
    expect(html).toContain('<ul><li>');
  });

  test('a list switching type at the same level closes and reopens', async ({ page }) => {
    const html = await render(page, '- one\n1. two');
    expect(html).toBe('<ul><li>one</li></ul><ol><li>two</li></ol>');
  });

  test('text after a list is not swallowed', async ({ page }) => {
    const html = await render(page, '1. one\n    - inner\n\nAfter the list.');
    expect(html).toContain('<ol><li>one<ul><li>inner</li></ul></li></ol>');
    expect(html).toContain('After the list.');
  });
});
