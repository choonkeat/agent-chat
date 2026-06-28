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

test.describe('fork — overflow menu (Phase 3)', () => {
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

  test('fork_session set: agent bubble shows a ⋯ menu button, not a standalone play button', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    await expect(page.locator('.bubble.agent .bubble-menu-btn')).toHaveCount(1);
    // The play action lives inside the menu now — no standalone TTS button.
    await expect(page.locator('.bubble.agent .bubble-tts-btn')).toHaveCount(0);
    // The old stacked fork button is gone.
    await expect(page.locator('.bubble.agent .bubble-fork-btn')).toHaveCount(0);
  });

  test('no fork_session: agent bubble keeps the plain play button, no menu', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    await expect(page.locator('.bubble.agent .bubble-tts-btn')).toHaveCount(1);
    await expect(page.locator('.bubble.agent .bubble-menu-btn')).toHaveCount(0);
  });

  test('agent bubble without a seq: no menu, falls back to plain play button', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // Locally-generated agent notices (e.g. "Clearing context...") carry no seq.
    await page.evaluate(() => window.addAgentMessage('local note', null, null, Date.now()));
    await expect(page.locator('.bubble.agent .bubble-tts-btn')).toHaveCount(1);
    await expect(page.locator('.bubble.agent .bubble-menu-btn')).toHaveCount(0);
  });

  test('user bubble never shows a menu or play button', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addUserMessage('hi from user', null, null, Date.now()));
    await expect(page.locator('.bubble.user')).toHaveCount(1);
    await expect(page.locator('.bubble.user .bubble-menu-btn')).toHaveCount(0);
    await expect(page.locator('.bubble.user .bubble-tts-btn')).toHaveCount(0);
  });

  test('clicking ⋯ opens a menu with speak + fork rows', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    await expect(page.locator('.bubble-menu')).toHaveCount(0);

    await page.locator('.bubble.agent .bubble-menu-btn').click();
    await expect(page.locator('.bubble-menu')).toHaveCount(1);
    await expect(page.locator('.bubble-menu [data-action="speak"]')).toHaveCount(1);
    await expect(page.locator('.bubble-menu [data-action="fork"]')).toHaveCount(1);
  });

  test('clicking ⋯ again toggles the menu closed', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    const btn = page.locator('.bubble.agent .bubble-menu-btn');
    await btn.click();
    await expect(page.locator('.bubble-menu')).toHaveCount(1);
    await btn.click();
    await expect(page.locator('.bubble-menu')).toHaveCount(0);
  });

  test('clicking outside dismisses the menu', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    await page.locator('.bubble.agent .bubble-menu-btn').click();
    await expect(page.locator('.bubble-menu')).toHaveCount(1);

    // Click an empty area of the page.
    await page.mouse.click(5, 5);
    await expect(page.locator('.bubble-menu')).toHaveCount(0);
  });

  test('Escape dismisses the menu', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    await page.locator('.bubble.agent .bubble-menu-btn').click();
    await expect(page.locator('.bubble-menu')).toHaveCount(1);

    await page.keyboard.press('Escape');
    await expect(page.locator('.bubble-menu')).toHaveCount(0);
  });

  test('"Fork from here" opens forkUrl in a new tab and closes the menu', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1')
      + '&parent_url=' + encodeURIComponent('https://parent.example/app/'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => {
      window.__opened = [];
      window.open = (u, t) => { window.__opened.push([u, t]); return null; };
      window.addAgentMessage('hello', null, null, Date.now(), 9);
    });

    await page.locator('.bubble.agent .bubble-menu-btn').click();
    await page.locator('.bubble-menu [data-action="fork"]').click();

    const opened = await page.evaluate(() => window.__opened);
    expect(opened).toEqual([
      ['https://parent.example/api/fork/sess-1?bubble=9&mode=after', '_blank'],
    ]);
    // Menu closes after acting.
    await expect(page.locator('.bubble-menu')).toHaveCount(0);
  });

  test('"Speak aloud" triggers TTS and closes the menu', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => {
      window.__spoke = [];
      // speakText is the top-level TTS entry point; stub it to observe the call.
      window.speakText = (text, done) => { window.__spoke.push(text); if (done) done(); };
      window.addAgentMessage('read me out', null, null, Date.now(), 5);
    });

    await page.locator('.bubble.agent .bubble-menu-btn').click();
    await page.locator('.bubble-menu [data-action="speak"]').click();

    const spoke = await page.evaluate(() => window.__spoke);
    expect(spoke.length).toBe(1);
    expect(spoke[0]).toContain('read me out');
    await expect(page.locator('.bubble-menu')).toHaveCount(0);
  });

  test('menu rows are comfortable tap targets (fat-finger guard)', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    await page.locator('.bubble.agent .bubble-menu-btn').click();

    const heights = await page.evaluate(() =>
      [...document.querySelectorAll('.bubble-menu button')].map(b => Math.round(b.getBoundingClientRect().height))
    );
    expect(heights.length).toBeGreaterThanOrEqual(2);
    for (const h of heights) expect(h).toBeGreaterThanOrEqual(40);
  });

  test('open menu stays within the viewport', async ({ page }) => {
    await page.goto(server.url + '/?fork_session=' + encodeURIComponent('sess-1'));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => window.addAgentMessage('hello', null, null, Date.now(), 5));
    await page.locator('.bubble.agent .bubble-menu-btn').click();

    const fits = await page.evaluate(() => {
      const m = document.querySelector('.bubble-menu').getBoundingClientRect();
      return m.left >= 0 && m.top >= 0 && m.right <= window.innerWidth && m.bottom <= window.innerHeight;
    });
    expect(fits).toBe(true);
  });
});
