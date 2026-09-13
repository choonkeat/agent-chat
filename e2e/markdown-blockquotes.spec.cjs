// @ts-check
// Unit-style tests for client-side renderMarkdown() blockquote handling,
// driven through Playwright so we exercise the exact function shipped in
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-md-quote-'));
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

test.describe('renderMarkdown — blockquotes', () => {
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

  test('a one-line quote still renders (regression)', async ({ page }) => {
    const html = await render(page, '> hello');
    expect(html).toBe('<blockquote>hello</blockquote>');
  });

  test('a nested quote still nests (regression)', async ({ page }) => {
    const html = await render(page, '> outer\n>> inner');
    expect(html).toBe('<blockquote>outer<blockquote>inner</blockquote></blockquote>');
  });

  test('a bare > line is a blank line inside the quote, not the end of it', async ({ page }) => {
    const html = await render(page, '> one\n>\n> two');
    expect(html).toBe('<blockquote>one<br><br>two</blockquote>');
    // The lone marker must not leak out as literal text between two quotes.
    expect(html).not.toContain('&gt;');
  });

  test('a quote marker with no space still quotes', async ({ page }) => {
    const html = await render(page, '>tight');
    expect(html).toBe('<blockquote>tight</blockquote>');
  });

  test('a flat list inside a quote renders as a list', async ({ page }) => {
    const html = await render(page, '> - one\n> - two');
    expect(html).toBe('<blockquote><ul><li>one</li><li>two</li></ul></blockquote>');
  });

  test('a nested list inside a quote nests', async ({ page }) => {
    const html = await render(page, [
      '> 1. Level one',
      '>     1. This is one dot one',
      '>     1. This is one dot two',
    ].join('\n'));
    expect(html).toBe(
      '<blockquote><ol>'
      + '<li>Level one<ol><li>This is one dot one</li><li>This is one dot two</li></ol></li>'
      + '</ol></blockquote>'
    );
  });

  test('a heading inside a quote renders as a heading', async ({ page }) => {
    const html = await render(page, '> # Title');
    expect(html).toBe('<blockquote><h1>Title</h1></blockquote>');
  });

  test('the whole reported example renders as one quote with one nested tree', async ({ page }) => {
    const html = await render(page, [
      '> Our speech bubble markdown renderer does not understand nested bullets',
      '>',
      '> 1. Level one',
      '>     1. This is one dot one',
      '>     1. This is one dot two',
      '> 1. Level two',
      '>     1. This is two dot one',
      '> 1. Level three',
      '>     - This is three dot something',
      '>     - This is three dot next thing',
    ].join('\n'));
    expect(html).toBe(
      '<blockquote>'
      + 'Our speech bubble markdown renderer does not understand nested bullets'
      + '<ol>'
      + '<li>Level one<ol><li>This is one dot one</li><li>This is one dot two</li></ol></li>'
      + '<li>Level two<ol><li>This is two dot one</li></ol></li>'
      + '<li>Level three<ul><li>This is three dot something</li><li>This is three dot next thing</li></ul></li>'
      + '</ol>'
      + '</blockquote>'
    );
    // One quote, not two split by the bare marker line.
    expect(html.match(/<blockquote>/g)).toHaveLength(1);
    expect(html).not.toContain('&gt;');
  });

  test('inline formatting inside a quoted list still applies', async ({ page }) => {
    const html = await render(page, '> - **bold** and `code`');
    expect(html).toContain('<strong>bold</strong>');
    expect(html).toContain('<code>code</code>');
    expect(html).toContain('<blockquote><ul><li>');
  });

  test('text after a quote is not swallowed', async ({ page }) => {
    const html = await render(page, '> quoted\n\nAfter the quote.');
    expect(html).toBe('<blockquote>quoted</blockquote>After the quote.');
  });
});
