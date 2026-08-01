// @ts-check
// Pins the opt-out from mobile Safari's text autosizing.
//
// Motivation: on iPhone the chat's type would grow on its own — a screenshot at
// 12:08 and another at 12:09 showed identical content at two different sizes —
// and a reload put it back. That is Safari re-running text autosizing whenever
// the layout shifts (the keyboard opening, the swe-swe iframe resizing, a
// rotation) and deciding the column is too narrow for the current size. The
// page never opted out, so the browser was free to override every font-size we
// set. `text-size-adjust: 100%` on html+body turns it off.
//
// Chrome (the CDP browser these tests drive) does not autosize, so this test
// cannot reproduce the growth itself. It pins the declaration instead: the
// computed value must be 100% on both html and body, which is the whole fix.
// Without this, a stylesheet edit could silently drop the line and the bug
// would come back only on a phone, where no test looks.
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

function startServer(extraArgs = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-autosize-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';

    const proc = spawn(bin, ['-no-stdio-mcp', ...extraArgs], {
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
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const page = await context.newPage();
    try {
      await use(page);
    } finally {
      await context.close().catch(() => {});
    }
  },
});

test.describe('Text autosizing opt-out', () => {
  test('html and body pin text-size-adjust to 100%', async ({ page }) => {
    const server = await startServer();
    try {
      await gotoRetry(page, server.url);
      await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

      const adjust = await page.evaluate(() => {
        const read = (el) => {
          const s = getComputedStyle(el);
          // Chrome exposes the -webkit- prefixed longhand; the unprefixed name
          // is what the stylesheet writes for every other engine.
          return s.getPropertyValue('-webkit-text-size-adjust')
            || s.getPropertyValue('text-size-adjust');
        };
        return {
          html: read(document.documentElement).trim(),
          body: read(document.body).trim(),
        };
      });

      expect(adjust.html).toBe('100%');
      expect(adjust.body).toBe('100%');
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });
});
