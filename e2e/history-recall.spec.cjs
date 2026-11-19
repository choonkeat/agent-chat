// @ts-check
// Recalling past messages into the box.
//
// Ctrl+R anywhere in the box, Up with the cursor at the very start, or
// Backspace in an empty box opens the autocomplete dropdown listing the
// user's own messages in this chat, newest first. Backspace with nothing typed
// since it opened, a tap outside, or a send closes it. Typing filters it; Enter/Tab/click picks one, which takes the
// place of the typed filter text.
const { test, expect } = require('@playwright/test');
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-'));
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
      if (!stderr.includes('Agent Chat UI:')) reject(new Error(`Server exited with code ${code}. stderr: ${stderr}`));
    });
    setTimeout(() => reject(new Error('Server did not start within 10s')), 10000);
  });
}

const test2 = test.extend({
  page: async ({}, use) => {
    const browser = await chromium.connectOverCDP(CDP_ENDPOINT, { ...(SLOW_MO > 0 && { slowMo: SLOW_MO }) });
    const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const page = await context.newPage();
    try { await use(page); } finally { await context.close().catch(() => {}); }
  },
});

async function ready(page, url) {
  await gotoRetry(page, url);
  const box = page.locator('#chat-input');
  await expect(box).toBeEnabled({ timeout: 5000 });
  await box.click();
  return box;
}

// Send each message and wait until it is drawn and the box is usable again.
async function sendAll(page, box, messages) {
  for (let i = 0; i < messages.length; i++) {
    await box.fill(messages[i]);
    await box.press('Enter');
    await expect(page.locator('.bubble.user')).toHaveCount(i + 1, { timeout: 5000 });
    await expect(box).toBeEnabled({ timeout: 5000 });
    await expect(box).toHaveValue('');
  }
}

const options = (page) => page.locator('#autocomplete-dropdown .ac-option');

test2.describe('Recall past messages', () => {
  /** @type {any} */
  let server = null;
  test2.beforeEach(async () => { server = await startServer(); });
  test2.afterEach(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
    server = null;
  });

  test2('Up at the start of the box lists sent messages newest first; Enter picks', async ({ page }) => {
    const box = await ready(page, server.url);
    await sendAll(page, box, ['first message', 'second\nhas two lines', 'first message', 'third one']);
    await box.focus();
    await box.press('ArrowUp');
    // Newest first, a repeated message listed once at its latest use.
    await expect(options(page)).toHaveCount(3);
    await expect(options(page).nth(0)).toHaveAttribute('data-value', 'third one');
    await expect(options(page).nth(1)).toHaveAttribute('data-value', 'first message');
    await expect(options(page).nth(2)).toHaveAttribute('data-value', 'second\nhas two lines');
    await box.press('ArrowDown');
    await box.press('ArrowDown');
    await box.press('Enter');
    await expect(box).toHaveValue('second\nhas two lines');
    await expect(page.locator('#autocomplete-dropdown')).not.toHaveClass(/visible/);
    // Nothing was sent by that Enter.
    await expect(page.locator('.bubble.user')).toHaveCount(4);
  });

  test2('Up with the cursor past the start does not open the list', async ({ page }) => {
    const box = await ready(page, server.url);
    await sendAll(page, box, ['alpha']);
    await box.fill('draft');
    await box.press('End');
    await box.press('ArrowUp');
    await expect(page.locator('#autocomplete-dropdown')).not.toHaveClass(/visible/);
  });

  test2('Backspace in an empty box opens the list; with text in the box it does not', async ({ page }) => {
    const box = await ready(page, server.url);
    await sendAll(page, box, ['alpha', 'beta']);
    await box.fill('x');
    await box.press('Home');
    await box.press('Backspace'); // cursor at start of a draft: no list
    await expect(page.locator('#autocomplete-dropdown')).not.toHaveClass(/visible/);
    await expect(box).toHaveValue('x');
    await box.press('End');
    await box.press('Backspace'); // deletes the x, box now empty: still no list
    await expect(box).toHaveValue('');
    await expect(page.locator('#autocomplete-dropdown')).not.toHaveClass(/visible/);
    await box.press('Backspace'); // fresh press in the empty box
    await expect(options(page)).toHaveCount(2);
    await expect(options(page).nth(0)).toHaveAttribute('data-value', 'beta');
    await box.press('Enter');
    await expect(box).toHaveValue('beta');
  });

  test2('Backspace with nothing typed closes the list; a tap outside closes it; Send closes it', async ({ page }) => {
    const box = await ready(page, server.url);
    await sendAll(page, box, ['alpha']);
    const dropdown = page.locator('#autocomplete-dropdown');
    await box.focus();
    await box.press('Backspace');
    await expect(dropdown).toHaveClass(/visible/);
    await box.pressSequentially('al');
    await box.press('Backspace');
    await box.press('Backspace'); // filter now empty, list still open
    await expect(dropdown).toHaveClass(/visible/);
    await box.press('Backspace'); // nothing typed since it opened: closes
    await expect(dropdown).not.toHaveClass(/visible/);
    await box.press('Backspace'); // and the next one opens it again
    await expect(dropdown).toHaveClass(/visible/);
    await page.locator('#messages, body').first().click({ position: { x: 10, y: 10 } });
    await expect(dropdown).not.toHaveClass(/visible/);
    await box.focus();
    await box.press('Control+r');
    await box.pressSequentially('zzz');
    await expect(dropdown).toHaveClass(/visible/);
    await page.locator('#btn-send').click();
    await expect(page.locator('.bubble.user')).toHaveCount(2);
    await expect(dropdown).not.toHaveClass(/visible/);
  });

  test2('Ctrl+R mid-text: typing filters, and the pick replaces only the filter', async ({ page }) => {
    const box = await ready(page, server.url);
    await sendAll(page, box, ['deploy the staging build', 'run the tests', 'deploy prod']);
    await box.fill('see: ');
    await box.press('End');
    await box.press('Control+r');
    await expect(options(page)).toHaveCount(3);
    await box.pressSequentially('deploy');
    await expect(options(page)).toHaveCount(2);
    await expect(options(page).nth(0)).toHaveAttribute('data-value', 'deploy prod');
    // Ctrl+R again steps to the next (older) match.
    await box.press('Control+r');
    await box.press('Enter');
    await expect(box).toHaveValue('see: deploy the staging build');
  });

  test2('a filter matching nothing is sent by Enter like any text', async ({ page }) => {
    const box = await ready(page, server.url);
    await sendAll(page, box, ['alpha']);
    await box.focus();
    await box.press('Control+r');
    await box.pressSequentially('zzz');
    await expect(page.locator('#autocomplete-dropdown .ac-status')).toHaveText('No sent message matches');
    await box.press('Enter');
    await expect(page.locator('.bubble.user')).toHaveCount(2);
  });
});
