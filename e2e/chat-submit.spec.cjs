// @ts-check
// Chat submit / newline keyboard behavior.
//
// Intended behavior:
//   - Ctrl+Enter and Cmd(Meta)+Enter ALWAYS submit, on every platform.
//   - Desktop: plain Enter submits; Shift+Enter / Alt+Enter insert a newline.
//   - Mobile/touch: plain Enter inserts a newline (send button only), but
//     Ctrl/Cmd+Enter (hardware keyboard) still submits.
//
// Observable for "submitted": handleSend() -> showLoading() inserts the
// #loading-bubble, which the server keeps in the DOM while awaiting a reply.
// Observable for "newline": the textarea value gains a "\n" and stays editable.
const { test, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);
// Let the WebSocket fully establish so the first send doesn't race the connection.
const SETTLE_MS = 800;

const MOBILE_UA = 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 '
  + '(KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36';

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

/**
 * Connect to the shared CDP browser and open a fresh page. `mobile: true`
 * uses a mobile user-agent + touch so the client's isMobile detection trips.
 * Returns { context, page }; caller closes context.
 */
async function openPage(mobile) {
  const browser = await chromium.connectOverCDP(CDP_ENDPOINT, {
    ...(SLOW_MO > 0 && { slowMo: SLOW_MO }),
  });
  const context = await browser.newContext(mobile
    ? { userAgent: MOBILE_UA, hasTouch: true, isMobile: true, viewport: { width: 390, height: 800 } }
    : { viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();
  return { context, page };
}

/** Navigate, wait for WebSocket-enabled textarea, focus it, clear it. */
async function ready(page, url) {
  await page.goto(url);
  const textarea = page.locator('#chat-input');
  await expect(textarea).toBeEnabled({ timeout: 5000 });
  await textarea.click();
  await page.waitForTimeout(SETTLE_MS);
  await textarea.fill('');
  return textarea;
}

// Observable for "submitted": handleSend() -> showLoading() inserts
// #loading-bubble, which the server keeps in the DOM while awaiting a reply.
async function expectSubmitted(page, textarea) {
  await expect(page.locator('#loading-bubble')).toBeVisible({ timeout: 3000 });
}

// Observable for "newline, not submitted": no loading bubble, value gained "\n".
async function expectNewline(page, textarea) {
  await expect(textarea).toHaveValue(/hello\n/);
  await expect(page.locator('#loading-bubble')).toHaveCount(0);
}

test.describe('Chat submit / newline keyboard behavior', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  // Fresh server per test: the server replays event history to each newly
  // connected page, so a prior test's submit would otherwise resurface its
  // loading bubble and break the next test's "no submit" assertion.
  test.beforeEach(async () => { server = await startServer(); });
  test.afterEach(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
    server = null;
  });

  test('desktop: plain Enter submits', async () => {
    const { context, page } = await openPage(false);
    try {
      const textarea = await ready(page, server.url);
      await textarea.fill('hello');
      await textarea.press('Enter');
      await expectSubmitted(page, textarea);
    } finally { await context.close().catch(() => {}); }
  });

  test('desktop: Shift+Enter inserts a newline, does not submit', async () => {
    const { context, page } = await openPage(false);
    try {
      const textarea = await ready(page, server.url);
      await textarea.fill('hello');
      await textarea.press('Shift+Enter');
      await expectNewline(page, textarea);
    } finally { await context.close().catch(() => {}); }
  });

  test('desktop: Ctrl+Enter submits', async () => {
    const { context, page } = await openPage(false);
    try {
      const textarea = await ready(page, server.url);
      await textarea.fill('hello');
      await textarea.press('Control+Enter');
      await expectSubmitted(page, textarea);
    } finally { await context.close().catch(() => {}); }
  });

  test('desktop: Cmd(Meta)+Enter submits', async () => {
    const { context, page } = await openPage(false);
    try {
      const textarea = await ready(page, server.url);
      await textarea.fill('hello');
      await textarea.press('Meta+Enter');
      await expectSubmitted(page, textarea);
    } finally { await context.close().catch(() => {}); }
  });

  test('mobile: plain Enter inserts a newline, does not submit', async () => {
    const { context, page } = await openPage(true);
    try {
      const textarea = await ready(page, server.url);
      await textarea.fill('hello');
      await textarea.press('Enter');
      await expectNewline(page, textarea);
    } finally { await context.close().catch(() => {}); }
  });

  test('mobile: Ctrl+Enter still submits (hardware keyboard)', async () => {
    const { context, page } = await openPage(true);
    try {
      const textarea = await ready(page, server.url);
      await textarea.fill('hello');
      await textarea.press('Control+Enter');
      await expectSubmitted(page, textarea);
    } finally { await context.close().catch(() => {}); }
  });
});
