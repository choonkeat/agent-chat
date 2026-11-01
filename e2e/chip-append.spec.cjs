// @ts-check
// Shift/Cmd/Ctrl-click on a quick-reply chip adds its text to the text box
// instead of sending it, so it can be edited or combined first. While the
// modifier is held, chips show a trailing "…" (the old menu convention for
// "asks for more before acting") so the user knows a click won't send.
// Touch has no modifier keys, so a long-press on a chip does the same.
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

// Starts a fresh agent-chat server with the given extra CLI args. Each call
// gets its own temp cwd so the in-memory event log starts empty.
function startServer(extraArgs = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-chipappend-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = serverPort();

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
    const context = await browser.newContext();
    const page = await context.newPage();
    try {
      await use(page);
    } finally {
      await context.close().catch(() => {});
    }
  },
});

test.describe('Modifier-click appends a quick reply to the box', () => {
  test('shift-click appends without sending; plain click still sends', async ({ page }) => {
    const server = await startServer(['-welcome-replies', 'Yes,Wait']);
    try {
      await gotoRetry(page, server.url);
      const input = page.locator('#chat-input');
      await expect(input).toBeEnabled({ timeout: 5000 });
      const chips = page.locator('#quick-replies .chip');
      await expect(chips).toHaveCount(2, { timeout: 5000 });

      // Holding Shift marks the chips: "…" appended via CSS, without the chip
      // changing size (a width change made every chip jump sideways).
      const before = await chips.nth(0).boundingBox();
      await page.keyboard.down('Shift');
      await expect(page.locator('body')).toHaveClass(/chip-append/);
      const during = await chips.nth(0).boundingBox();
      expect(during).toEqual(before);
      const after = await chips.nth(0).evaluate((el) => getComputedStyle(el, '::after').content);
      expect(after).toBe('"\u2026"');
      await chips.nth(0).click();
      await chips.nth(1).click();
      await page.keyboard.up('Shift');
      await expect(page.locator('body')).not.toHaveClass(/chip-append/);

      await expect(input).toHaveValue('Yes Wait');
      await expect(input).toBeFocused();
      // Nothing was sent; chips are still offered.
      await expect(page.locator('.bubble.user')).toHaveCount(0);
      await expect(chips).toHaveCount(2);

      // Meta-click works too, and spacing respects existing whitespace.
      await input.fill('Ok ');
      await chips.nth(1).click({ modifiers: ['Meta'] });
      await expect(input).toHaveValue('Ok Wait');

      // A plain click still sends immediately.
      await input.fill('');
      await chips.nth(0).click();
      await expect(page.locator('.bubble.user').last()).toContainText('Yes', { timeout: 5000 });
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('touch long-press appends without sending; a short tap still sends', async ({ page }) => {
    const server = await startServer(['-welcome-replies', 'Yes,Wait']);
    try {
      await gotoRetry(page, server.url);
      const input = page.locator('#chat-input');
      await expect(input).toBeEnabled({ timeout: 5000 });
      const chips = page.locator('#quick-replies .chip');
      await expect(chips).toHaveCount(2, { timeout: 5000 });

      const touch = { pointerType: 'touch', isPrimary: true, bubbles: true, clientX: 5, clientY: 5 };

      // Hold: the chip shows "…" while held, then appends once the hold completes.
      await chips.nth(1).dispatchEvent('pointerdown', touch);
      await expect(chips.nth(1)).toHaveClass(/chip-holding/);
      await expect(input).toHaveValue('Wait', { timeout: 2000 });
      await expect(chips.nth(1)).not.toHaveClass(/chip-holding/);
      await chips.nth(1).dispatchEvent('pointerup', touch);
      await chips.nth(1).dispatchEvent('click'); // the click a real release fires
      await expect(page.locator('.bubble.user')).toHaveCount(0);
      await expect(chips).toHaveCount(2);

      // Moving the finger (a scroll) cancels the hold.
      await chips.nth(0).dispatchEvent('pointerdown', touch);
      await chips.nth(0).dispatchEvent('pointermove', { ...touch, clientX: 5, clientY: 40 });
      await page.waitForTimeout(800);
      await expect(input).toHaveValue('Wait');

      // A short tap sends as before.
      await input.fill('');
      await chips.nth(0).dispatchEvent('pointerdown', touch);
      await chips.nth(0).dispatchEvent('pointerup', touch);
      await chips.nth(0).dispatchEvent('click');
      await expect(page.locator('.bubble.user').last()).toContainText('Yes', { timeout: 5000 });
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });
});
