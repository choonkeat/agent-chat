// @ts-check
// A long unbroken attachment filename must wrap inside its bubble and must NOT
// widen the chat page. Regression guard for `.file-attachment-link` having no
// `overflow-wrap: anywhere`, so its min-content width was the whole filename.
// Driven through the shipped renderFileAttachments() + CSS.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-long-name-'));
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

const LONG_NAME = 'quarterly_report_'.repeat(12) + 'final.pdf';

function measure(page) {
  return page.evaluate(() => {
    const doc = document.documentElement;
    const bubble = document.getElementById('__long_name_bubble');
    const link = bubble.querySelector('.file-attachment-link');
    return {
      pageScrollWidth: doc.scrollWidth,
      pageClientWidth: doc.clientWidth,
      bubbleRight: bubble.getBoundingClientRect().right,
      linkRight: link.getBoundingClientRect().right,
      linkHeight: link.getBoundingClientRect().height,
      lineHeight: parseFloat(getComputedStyle(link).lineHeight),
      viewport: window.innerWidth,
    };
  });
}

test.describe('file attachment — long filename', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });
  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('wraps inside the bubble and does not widen the page', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate((name) => {
      const messages = document.getElementById('messages');
      const quick = document.getElementById('quick-replies');
      const bubble = document.createElement('div');
      bubble.className = 'bubble user';
      bubble.id = '__long_name_bubble';
      // @ts-ignore -- page global from app.js
      bubble.appendChild(renderFileAttachments([{ name, type: 'application/pdf', url: '#' }]));
      messages.insertBefore(bubble, quick);
    }, LONG_NAME);

    const fixed = await measure(page);
    expect(fixed.pageScrollWidth).toBeLessThanOrEqual(fixed.pageClientWidth + 1);
    expect(fixed.bubbleRight).toBeLessThanOrEqual(fixed.viewport + 1);
    expect(fixed.linkRight).toBeLessThanOrEqual(fixed.bubbleRight + 1);
    // More than one line: the name wrapped rather than being clipped.
    expect(fixed.linkHeight).toBeGreaterThan(fixed.lineHeight * 1.5);

    // Control: without the rule the same name does overflow, so the assertions
    // above are measuring the bug.
    await page.evaluate(() => {
      /** @type {HTMLElement} */ (document.querySelector('#__long_name_bubble .file-attachment-link'))
        .style.overflowWrap = 'normal';
    });
    const broken = await measure(page);
    expect(broken.linkRight).toBeGreaterThan(broken.viewport);
  });
});
