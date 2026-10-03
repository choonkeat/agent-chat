// @ts-check
// The text box keeps what the user is typing.
//
//   - A message pushed in from outside (swe-swe's send_chat_message on the
//     orchestrator server) used to empty the box: every arriving user message
//     cleared it, whoever sent it. Now only this browser's own send does.
//   - What is typed is copied to localStorage per session, so a reload puts it
//     back; a send deletes the copy, so sent words never come back; another
//     session (a different SESSION_UUID) never sees it.
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
const SETTLE_MS = 800;
// Longer than the save's typing pause (DRAFT_SAVE_DELAY_MS in app.js).
const SAVE_WAIT_MS = 600;

/** Start agent-chat in a temp dir as session `uuid`. Caller kills proc. */
function startServer(uuid) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = serverPort();
    cleanEnv.SESSION_UUID = uuid;
    const proc = spawn(bin, ['-no-stdio-mcp'], {
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

function stopServer(server) {
  if (!server?.proc) return;
  server.proc.kill('SIGTERM');
  fs.rmSync(server.dir, { recursive: true, force: true });
}

/** Wait until the server has let go of the port, so the next one can bind it. */
function waitExit(server) {
  return new Promise((resolve) => {
    if (!server?.proc || server.proc.exitCode !== null) return resolve();
    server.proc.once('exit', () => resolve());
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

async function ready(page, url) {
  await gotoRetry(page, url);
  const textarea = page.locator('#chat-input');
  await expect(textarea).toBeEnabled({ timeout: 5000 });
  await page.waitForTimeout(SETTLE_MS);
  return textarea;
}

/** swe-swe's way in: the orchestrator's send_chat_message. */
async function pushFromOutside(baseUrl, text) {
  const res = await fetch(`${baseUrl}/mcp/orchestrator`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Accept': 'application/json, text/event-stream' },
    body: JSON.stringify({
      jsonrpc: '2.0', id: Date.now(), method: 'tools/call',
      params: { name: 'send_chat_message', arguments: { text } },
    }),
  });
  return res.text();
}

test.describe('Draft in the text box is kept', () => {
  /** @type {any} */
  let server = null;
  test.afterEach(async () => { stopServer(server); server = null; });

  test('a message pushed from outside leaves the typed text alone', async () => {
    server = await startServer('draft-session-a');
    const { context, page } = await openPage();
    try {
      const textarea = await ready(page, server.url);
      await textarea.fill('an intricate half-typed prompt');
      expect(await pushFromOutside(server.url, 'from another agent')).toContain('message pushed');
      await expect(page.locator('.bubble.user', { hasText: 'from another agent' })).toBeVisible({ timeout: 5000 });
      await expect(textarea).toHaveValue('an intricate half-typed prompt');
      await expect(textarea).toHaveJSProperty('readOnly', false);
    } finally { await context.close().catch(() => {}); }
  });

  test('own send still empties the box and unlocks it', async () => {
    server = await startServer('draft-session-a');
    const { context, page } = await openPage();
    try {
      const textarea = await ready(page, server.url);
      await textarea.fill('hello');
      await textarea.press('Enter');
      await expect(page.locator('.bubble.user', { hasText: 'hello' })).toBeVisible({ timeout: 5000 });
      await expect(textarea).toHaveValue('');
      await expect(textarea).toHaveJSProperty('readOnly', false);
    } finally { await context.close().catch(() => {}); }
  });

  test('a reload puts the draft back; a send stops it coming back', async () => {
    server = await startServer('draft-session-a');
    const { context, page } = await openPage();
    try {
      let textarea = await ready(page, server.url);
      await textarea.fill('survives a reload');
      await page.waitForTimeout(SAVE_WAIT_MS);
      textarea = await ready(page, server.url);
      await expect(textarea).toHaveValue('survives a reload');

      await textarea.press('Enter');
      await expect(textarea).toHaveValue('');
      textarea = await ready(page, server.url);
      await expect(textarea).toHaveValue('');
    } finally { await context.close().catch(() => {}); }
  });

  test('another session does not pick up the draft', async () => {
    server = await startServer('draft-session-a');
    const { context, page } = await openPage();
    try {
      const textarea = await ready(page, server.url);
      await textarea.fill('belongs to session a');
      await page.waitForTimeout(SAVE_WAIT_MS);

      // Same address, different session: only SESSION_UUID tells them apart.
      const first = server;
      stopServer(first);
      await waitExit(first);
      server = await startServer('draft-session-b');
      const other = await ready(page, server.url);
      await expect(other).toHaveValue('');
      const keys = await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('agent-chat-draft:')));
      expect(keys).toEqual(['agent-chat-draft:draft-session-a']);
    } finally { await context.close().catch(() => {}); }
  });

  test('old and surplus copies are pruned when a page opens', async () => {
    server = await startServer('draft-session-a');
    const { context, page } = await openPage();
    try {
      await ready(page, server.url);
      await page.evaluate(() => {
        const now = Date.now();
        localStorage.setItem('agent-chat-draft:stale', JSON.stringify({ text: 'old', t: now - 8 * 24 * 3600 * 1000 }));
        localStorage.setItem('agent-chat-draft:broken', 'not json');
        for (let i = 0; i < 25; i++) {
          localStorage.setItem('agent-chat-draft:s' + i, JSON.stringify({ text: 'x', t: now - i * 1000 }));
        }
        localStorage.setItem('unrelated', 'kept');
      });
      await ready(page, server.url);
      const left = await page.evaluate(() => Object.keys(localStorage).sort());
      const drafts = left.filter((k) => k.startsWith('agent-chat-draft:'));
      expect(drafts.length).toBe(20);
      expect(drafts).not.toContain('agent-chat-draft:stale');
      expect(drafts).not.toContain('agent-chat-draft:broken');
      expect(drafts).toContain('agent-chat-draft:s0');
      expect(drafts).not.toContain('agent-chat-draft:s24');
      expect(left).toContain('unrelated');
    } finally { await context.close().catch(() => {}); }
  });
});
