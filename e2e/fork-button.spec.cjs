// @ts-check
// Per-bubble "fork" button. Mirrors the parent_url tests in
// markdown-images.spec.cjs: the relevant logic lives at the top level of the
// classic script shipped in client-dist/app.js, so it is reachable on `window`.
//
// Phase 1 covers the URL plumbing (forkSession param + forkUrl helper).
// Phase 2 covers the rendered button on agent bubbles + the confirm/open flow.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-fork-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';

    const proc = spawn(bin, ['-no-stdio-mcp'], {
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

test.describe('fork button — URL plumbing (Phase 1)', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => {
    server = await startServer();
  });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('fork_session query param is read into forkSession on load', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-abc-123'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const fs2 = await page.evaluate(() => window.forkSession);
    expect(fs2).toBe('sess-abc-123');
  });

  test('no fork_session param: forkSession is empty', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const fs2 = await page.evaluate(() => window.forkSession);
    expect(fs2).toBe('');
  });

  test('forkUrl(seq) builds absolute /api/fork URL against parentBaseUrl', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-abc-123')
      + '&parent_url=' + encodeURIComponent('https://parent.example/app/page'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const url = await page.evaluate(() => window.forkUrl(7));
    expect(url).toBe('https://parent.example/api/fork/sess-abc-123?bubble=7&mode=after');
  });

  test('forkUrl encodes the session uuid', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('a/b c')
      + '&parent_url=' + encodeURIComponent('https://parent.example/app/'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const url = await page.evaluate(() => window.forkUrl(3));
    expect(url).toBe('https://parent.example/api/fork/a%2Fb%20c?bubble=3&mode=after');
  });
});

test.describe('fork button — rendering & interaction (Phase 2)', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => {
    server = await startServer();
  });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('fork_session set: agent bubble shows a fork button', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    await expect(page.locator('.bubble.agent .bubble-fork-btn')).toHaveCount(1);
  });

  test('no fork_session: agent bubble has no fork button', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    await expect(page.locator('.bubble.agent')).toHaveCount(1);
    await expect(page.locator('.bubble.agent .bubble-fork-btn')).toHaveCount(0);
  });

  test('user bubble never shows a fork button (even with fork_session)', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addUserMessage('hi from user', null, null, Date.now()));
    await expect(page.locator('.bubble.user')).toHaveCount(1);
    await expect(page.locator('.bubble.user .bubble-fork-btn')).toHaveCount(0);
  });

  test('agent bubble without a seq shows no fork button', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // Locally-generated agent messages (e.g. "Clearing context...") carry no seq.
    await page.evaluate(() => window.addAgentMessage('local note', null, null, Date.now()));
    await expect(page.locator('.bubble.agent')).toHaveCount(1);
    await expect(page.locator('.bubble.agent .bubble-fork-btn')).toHaveCount(0);
  });

  test('clicking fork button → confirm → window.open(forkUrl, _blank)', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1')
      + '&parent_url=' + encodeURIComponent('https://parent.example/app/'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => {
      window.__opened = [];
      window.confirm = () => true;
      window.open = (u, t) => { window.__opened.push([u, t]); return null; };
      window.addAgentMessage('hello', null, null, Date.now(), 9);
    });

    await page.locator('.bubble.agent .bubble-fork-btn').click();

    const opened = await page.evaluate(() => window.__opened);
    expect(opened).toEqual([
      ['https://parent.example/api/fork/sess-1?bubble=9&mode=after', '_blank'],
    ]);
  });

  test('clicking fork button → cancel confirm → window.open NOT called', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1')
      + '&parent_url=' + encodeURIComponent('https://parent.example/app/'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => {
      window.__opened = [];
      window.confirm = () => false;
      window.open = (u, t) => { window.__opened.push([u, t]); return null; };
      window.addAgentMessage('hello', null, null, Date.now(), 9);
    });

    await page.locator('.bubble.agent .bubble-fork-btn').click();

    const opened = await page.evaluate(() => window.__opened);
    expect(opened).toEqual([]);
  });
});
