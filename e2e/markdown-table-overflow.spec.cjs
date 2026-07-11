// @ts-check
// A wide markdown table must scroll horizontally *within its bubble* and must
// NOT widen the whole chat page. Regression guard for the bug where
// `.bubble table` had no overflow handling (unlike `.bubble pre`), so a
// many-column table blew past the bubble's 80% cap and forced the page wider.
// Driven through the real UI so we exercise the shipped renderMarkdown() + CSS.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-md-tbl-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';
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

// A deliberately wide, many-column table — guaranteed to exceed the bubble's
// 80% cap at the narrow viewport below.
const WIDE_TABLE = [
  '| Column A | Column B | Column C | Column D | Column E | Column F | Column G | Column H | Column I | Column J | Column K | Column L |',
  '|----------|----------|----------|----------|----------|----------|----------|----------|----------|----------|----------|----------|',
  '| aaaaaaaaaaaaaa | bbbbbbbbbbbbbb | cccccccccccccc | dddddddddddddd | eeeeeeeeeeeeee | ffffffffffffff | gggggggggggggg | hhhhhhhhhhhhhh | iiiiiiiiiiiiii | jjjjjjjjjjjjjj | kkkkkkkkkkkkkk | llllllllllllll |',
  '| 1111111111 | 2222222222 | 3333333333 | 4444444444 | 5555555555 | 6666666666 | 7777777777 | 8888888888 | 9999999999 | 0000000000 | 1234567890 | 0987654321 |',
].join('\n');

test.describe('renderMarkdown — wide table overflow', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });
  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('wide table scrolls within its bubble and does not widen the page', async ({ page }) => {
    // Narrow viewport so a 12-column table is guaranteed wider than the bubble.
    await page.setViewportSize({ width: 640, height: 800 });
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate((md) => {
      const messages = document.getElementById('messages');
      const quick = document.getElementById('quick-replies');
      const bubble = document.createElement('div');
      bubble.className = 'bubble agent';
      bubble.id = '__wide_table_bubble';
      bubble.innerHTML = window.renderMarkdown(md);
      messages.insertBefore(bubble, quick);
    }, WIDE_TABLE);

    // Sanity: the table actually rendered.
    await expect(page.locator('#__wide_table_bubble table')).toHaveCount(1);

    const metrics = await page.evaluate(() => {
      const doc = document.documentElement;
      const bubble = document.getElementById('__wide_table_bubble');
      const table = bubble.querySelector('table');
      return {
        pageScrollWidth: doc.scrollWidth,
        pageClientWidth: doc.clientWidth,
        bubbleClientWidth: bubble.getBoundingClientRect().width,
        viewport: window.innerWidth,
        tableScrollWidth: table.scrollWidth,
        tableClientWidth: table.clientWidth,
      };
    });

    // (a) The page must NOT overflow horizontally. Allow 1px for rounding.
    expect(metrics.pageScrollWidth).toBeLessThanOrEqual(metrics.pageClientWidth + 1);

    // (b) The bubble must stay within the viewport (it may not push the page wide).
    expect(metrics.bubbleClientWidth).toBeLessThanOrEqual(metrics.viewport + 1);

    // (c) The overflow must be *contained*: the table is the scroll container,
    // so its scrollable content is wider than its visible box. This proves the
    // wide content still exists (just scrolls) rather than being hidden/clipped.
    expect(metrics.tableScrollWidth).toBeGreaterThan(metrics.tableClientWidth);
  });
});
