// @ts-check
// On-screen keyboard must not leave the last messages hidden behind the
// sticky input bar.
//
// #chat-footer is `position: sticky; bottom: 0`, so it paints over flow
// content whenever the document is not scrolled to the very end. Opening the
// keyboard shortens the visible area without moving the scroll offset, which
// parks the footer on top of the newest messages.
//
// A shrinking viewport is the closest thing a headless browser has to an
// on-screen keyboard: inside the swe-swe iframe that is literally what
// happens (the host resizes the frame and the inner window fires `resize`).
// The iOS-Safari-standalone path is visualViewport-only and cannot be
// simulated here, so it is covered structurally instead — see the last test.
const { test, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);
const SETTLE_MS = 800;

const MOBILE_UA = 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 '
  + '(KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36';

const TALL = { width: 390, height: 800 };
// 800 -> 400 is roughly what an iPhone keyboard takes.
const SHORT = { width: 390, height: 400 };

/** Start agent-chat in a temp dir on a random port. Caller kills proc. */
function startServer(extraFlags = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';
    const proc = spawn(bin, ['-no-stdio-mcp', ...extraFlags], {
      cwd: dir, env: cleanEnv, stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stderr = '';
    proc.stderr.on('data', (chunk) => {
      stderr += chunk.toString();
      const match = stderr.match(/Agent Chat UI: http:\/\/localhost:(\d+)/);
      // 127.0.0.1, not localhost: the server binds 0.0.0.0 (IPv4 only) and
      // Chrome may try ::1 first, which refuses the connection.
      if (match) resolve({ url: `http://127.0.0.1:${match[1]}`, proc, dir });
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

/** Connect to the shared CDP browser, open a mobile-sized page. */
async function openPage() {
  const browser = await chromium.connectOverCDP(CDP_ENDPOINT, {
    ...(SLOW_MO > 0 && { slowMo: SLOW_MO }),
  });
  const context = await browser.newContext({
    userAgent: MOBILE_UA, hasTouch: true, isMobile: true, viewport: TALL,
  });
  const page = await context.newPage();
  // Record visualViewport listener registrations before app.js runs, so a
  // later test can assert the iOS-only code path is wired up at all.
  await page.addInitScript(() => {
    window.__vvListeners = [];
    if (window.visualViewport) {
      const orig = window.visualViewport.addEventListener.bind(window.visualViewport);
      window.visualViewport.addEventListener = function (type, ...rest) {
        window.__vvListeners.push(type);
        return orig(type, ...rest);
      };
    }
  });
  return { context, page };
}

/**
 * Navigate and wait for the WebSocket-enabled textarea.
 *
 * The goto is retried: the browser reaches this container's ports through a
 * forwarder that takes a few seconds to notice a freshly bound port, so the
 * first navigation after startServer() often gets ERR_CONNECTION_REFUSED.
 */
async function ready(page, url) {
  const deadline = Date.now() + 20000;
  for (;;) {
    try {
      await page.goto(url, { timeout: 5000 });
      break;
    } catch (err) {
      if (Date.now() > deadline) throw err;
      await page.waitForTimeout(500);
    }
  }
  await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
  await page.waitForTimeout(SETTLE_MS);
}

/**
 * Fill the conversation with several screens of scrollback and mark the last
 * bubble, then scroll to the very end (as the app does after every render).
 */
async function seedScrollback(page) {
  await page.evaluate(() => {
    const messages = document.getElementById('messages');
    for (let i = 0; i < 30; i++) {
      const div = document.createElement('div');
      div.textContent = `seeded message ${i} — filler line to build up scrollback`;
      div.style.padding = '12px 0';
      if (i === 29) div.id = 'last-probe';
      messages.appendChild(div);
    }
    window.scrollTo(0, document.documentElement.scrollHeight);
  });
  await page.waitForTimeout(200);
}

/** Pixels of the last bubble hidden behind the sticky footer (0 = fully visible). */
function overlapPx(page) {
  return page.evaluate(() => {
    const probe = document.getElementById('last-probe').getBoundingClientRect();
    const footer = document.getElementById('chat-footer').getBoundingClientRect();
    return Math.max(0, probe.bottom - footer.top);
  });
}

test.describe('on-screen keyboard re-pins the conversation', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeEach(async () => { server = await startServer(); });
  test.afterEach(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
    server = null;
  });

  test('shrinking the viewport keeps the last message above the input bar', async () => {
    const { context, page } = await openPage();
    try {
      await ready(page, server.url);
      await seedScrollback(page);

      // Precondition: at rest, the last message sits clear of the footer.
      expect(await overlapPx(page)).toBe(0);

      // Keyboard opens.
      await page.setViewportSize(SHORT);
      await page.waitForTimeout(400);

      expect(await overlapPx(page)).toBe(0);
    } finally { await context.close().catch(() => {}); }
  });

  test('a reader who scrolled up is not yanked to the bottom', async () => {
    const { context, page } = await openPage();
    try {
      await ready(page, server.url);
      await seedScrollback(page);

      // Scroll back through history — well past the 40px isUserScrolledUp
      // threshold — and let the scroll handler run.
      await page.evaluate(() => window.scrollTo(0, window.scrollY - 500));
      await page.waitForTimeout(200);
      const before = await page.evaluate(() => window.scrollY);

      await page.setViewportSize(SHORT);
      await page.waitForTimeout(400);

      const after = await page.evaluate(() => window.scrollY);
      expect(Math.abs(after - before)).toBeLessThan(50);
      // And definitely not re-pinned to the end.
      const atBottom = await page.evaluate(() =>
        document.documentElement.scrollHeight - window.scrollY - window.innerHeight < 40);
      expect(atBottom).toBe(false);
    } finally { await context.close().catch(() => {}); }
  });

  test('the visualViewport path is wired up (iOS Safari standalone tab)', async () => {
    const { context, page } = await openPage();
    try {
      await ready(page, server.url);
      const types = await page.evaluate(() => window.__vvListeners || []);
      expect(types).toContain('resize');
    } finally { await context.close().catch(() => {}); }
  });
});
