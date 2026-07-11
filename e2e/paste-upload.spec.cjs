// @ts-check
// Paste-to-upload behavior.
//
// Intended behavior (see the 'paste' handler in client-dist/app.js):
//   - Pasting a file/image with NO meaningful text (e.g. a screenshot) stages
//     it for upload, same as drag-drop, and suppresses the default text paste.
//   - Pasting when real text is present (rich text / spreadsheet ranges carry
//     text/plain alongside an image/png snapshot) pastes as text and IGNORES
//     the image — no file is staged.
//
// Observable for "staged": renderStaging() inserts a .file-chip into
// #file-staging synchronously, before the upload round-trip completes.
// Observable for "not staged": #file-staging stays empty.
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

async function openPage() {
  const browser = await chromium.connectOverCDP(CDP_ENDPOINT, {
    ...(SLOW_MO > 0 && { slowMo: SLOW_MO }),
  });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
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

/**
 * Dispatch a synthetic paste on #chat-input carrying a fake PNG file, and
 * optionally a text/plain payload. Returns whether the event's default was
 * prevented (i.e. the handler intercepted it as an upload).
 */
async function pastePng(page, text) {
  return page.evaluate((textPayload) => {
    const el = document.getElementById('chat-input');
    const dt = new DataTransfer();
    const bytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47]); // "\x89PNG"
    dt.items.add(new File([bytes], 'shot.png', { type: 'image/png' }));
    if (textPayload) dt.setData('text/plain', textPayload);
    const evt = new ClipboardEvent('paste', {
      clipboardData: dt, bubbles: true, cancelable: true,
    });
    el.dispatchEvent(evt);
    return evt.defaultPrevented;
  }, text || null);
}

test.describe('Paste to upload', () => {
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

  test('image with no text is staged for upload', async () => {
    const { context, page } = await openPage();
    try {
      await ready(page, server.url);
      const prevented = await pastePng(page, '');
      expect(prevented).toBe(true); // default text paste suppressed
      await expect(page.locator('#file-staging .file-chip')).toHaveCount(1, { timeout: 3000 });
    } finally { await context.close().catch(() => {}); }
  });

  test('image accompanied by real text pastes as text, does not stage a file', async () => {
    const { context, page } = await openPage();
    try {
      await ready(page, server.url);
      const prevented = await pastePng(page, 'hello from a rich-text copy');
      expect(prevented).toBe(false); // let the browser insert the text
      await expect(page.locator('#file-staging .file-chip')).toHaveCount(0);
    } finally { await context.close().catch(() => {}); }
  });

  test('whitespace-only text still counts as no text (screenshot case)', async () => {
    const { context, page } = await openPage();
    try {
      await ready(page, server.url);
      const prevented = await pastePng(page, '   \n\t ');
      expect(prevented).toBe(true);
      await expect(page.locator('#file-staging .file-chip')).toHaveCount(1, { timeout: 3000 });
    } finally { await context.close().catch(() => {}); }
  });
});
