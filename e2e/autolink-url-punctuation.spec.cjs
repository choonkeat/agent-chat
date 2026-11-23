// @ts-check
// A bare URL must not swallow the sentence punctuation after it, and must keep
// punctuation that is really part of the URL. Driven through the shipped
// window.renderMarkdown.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-url-punct-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = serverPort();
    const proc = spawn(bin, ['-no-stdio-mcp'], { cwd: dir, env: cleanEnv, stdio: ['ignore', 'pipe', 'pipe'] });
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

// [input, expected href, expected text content]
const CASES = [
  ['See https://a.com.', 'https://a.com', 'See https://a.com.'],
  ['https://a.com, then more', 'https://a.com', 'https://a.com, then more'],
  ['Really https://a.com/x?', 'https://a.com/x', 'Really https://a.com/x?'],
  ['Wow https://a.com!', 'https://a.com', 'Wow https://a.com!'],
  ['first https://a.com; second', 'https://a.com', 'first https://a.com; second'],
  ['link: https://a.com: done', 'https://a.com', 'link: https://a.com: done'],
  ['(see https://a.com)', 'https://a.com', '(see https://a.com)'],
  ['(see https://a.com).', 'https://a.com', '(see https://a.com).'],
  ["'https://a.com'", 'https://a.com', "'https://a.com'"],
  ['<https://a.com>', 'https://a.com', '<https://a.com>'],
  ['https://a.com...', 'https://a.com', 'https://a.com...'],
  // Punctuation that belongs to the URL stays.
  ['https://en.wikipedia.org/wiki/Foo_(bar)', 'https://en.wikipedia.org/wiki/Foo_(bar)', 'https://en.wikipedia.org/wiki/Foo_(bar)'],
  ['See https://en.wikipedia.org/wiki/Foo_(bar).', 'https://en.wikipedia.org/wiki/Foo_(bar)', 'See https://en.wikipedia.org/wiki/Foo_(bar).'],
  ['https://a.com/x.html?a=1,2&b=3#top', 'https://a.com/x.html?a=1,2&b=3#top', 'https://a.com/x.html?a=1,2&b=3#top'],
  ['https://a.com:8080/path/', 'https://a.com:8080/path/', 'https://a.com:8080/path/'],
];

test.describe('renderMarkdown — bare URL punctuation', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });
  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('links exactly the URL and keeps the prose around it', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const results = await page.evaluate((cases) => cases.map(([input]) => {
      const t = document.createElement('template');
      // @ts-ignore -- page global from app.js
      t.innerHTML = window.renderMarkdown(input);
      const links = t.content.querySelectorAll('a');
      return {
        input,
        count: links.length,
        href: links[0] && links[0].getAttribute('href'),
        linkText: links[0] && links[0].textContent,
        text: t.content.textContent,
      };
    }), CASES);

    results.forEach((r, i) => {
      const [input, href, text] = CASES[i];
      expect(r, input).toEqual({ input, count: 1, href, linkText: href, text });
    });
  });

  test('a scheme with nothing after it is not linked as a bare scheme', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    const html = await page.evaluate(() => {
      // @ts-ignore -- page global from app.js
      return window.renderMarkdown('https://...');
    });
    expect(html).not.toContain('href="https://"');
  });

  // Parsed in an inert <template>, so nothing in these inputs can execute.
  for (const [name, input] of [
    ['bare URL', 'https://a.com/"onmouseover="x'],
    ['markdown link', '[t](https://a.com/"onmouseover="x)'],
    ['image URL', '![t](https://a.com/"onerror="x)'],
    ['image alt', '![a"onerror="x](https://a.com/i.png)'],
    ['URL inside a link href', '[t](https://a.com/,https://b.com/"onclick="x)'],
    ['URL inside an image alt', '![https://b.com/"onerror="x](https://a.com/i.png)'],
  ]) {
    test(`a double quote cannot add an attribute: ${name}`, async ({ page }) => {
      await gotoRetry(page, server.url);
      await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
      const elements = await page.evaluate((md) => {
        const t = document.createElement('template');
        // @ts-ignore -- page global from app.js
        t.innerHTML = window.renderMarkdown(md);
        return [...t.content.querySelectorAll('*')].map((el) => ({
          tag: el.tagName,
          attrs: el.getAttributeNames().sort(),
        }));
      }, input);
      expect(elements).toHaveLength(1);
      const allowed = elements[0].tag === 'IMG' ? ['alt', 'src'] : ['href', 'rel', 'target'];
      expect(elements[0].attrs).toEqual(allowed);
    });
  }

  test('a URL used as link text stays one link', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    const hrefs = await page.evaluate(() => {
      const t = document.createElement('template');
      // @ts-ignore -- page global from app.js
      t.innerHTML = window.renderMarkdown('[https://a.com](https://b.com)');
      return [...t.content.querySelectorAll('a')].map((a) => a.getAttribute('href'));
    });
    expect(hrefs).toEqual(['https://b.com']);
  });
});
