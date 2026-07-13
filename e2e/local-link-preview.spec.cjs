// @ts-check
// Local-link → App Preview interceptor. When agent-chat is embedded in a host
// (swe-swe) that passes parent_url, a click on a *local* link inside a chat
// bubble (localhost / 127.0.0.1 / [::1] / *.lvh.me) should be preventDefault'd
// and posted to the parent as { type: 'agent-chat-open-preview', url } — so the
// host loads it in its App Preview pane instead of a new browser tab. External
// links, modified clicks, and standalone (non-embedded) chat keep default
// new-tab behaviour.
//
// Mirrors fork-button.spec.cjs: the interceptor + its classifier live at the
// top level of the classic script shipped in client-dist/app.js, so the
// classifier is reachable on `window` and the handler is wired to #messages.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-link-preview-'));
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

test.describe('local link → App Preview interceptor', () => {
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

  // --- classifier: which hosts are "local" (→ Preview) vs real sites (→ tab) ---
  test('isLocalPreviewHost classifies local hosts, including *.lvh.me vhosts', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const got = await page.evaluate(() => {
      const hosts = [
        'localhost', '127.0.0.1', '[::1]', 'lvh.me',
        'abc.lvh.me', 'a.b.lvh.me', 'MyApp.LVH.ME',
        'example.com', 'notlvh.me', 'evil-lvh.me', '',
      ];
      const out = {};
      for (const h of hosts) out[h] = window.isLocalPreviewHost(h);
      return out;
    });

    // local → true
    expect(got['localhost']).toBe(true);
    expect(got['127.0.0.1']).toBe(true);
    expect(got['[::1]']).toBe(true);
    expect(got['lvh.me']).toBe(true);
    expect(got['abc.lvh.me']).toBe(true);      // the canonical vhost case
    expect(got['a.b.lvh.me']).toBe(true);      // multi-label subdomain
    expect(got['MyApp.LVH.ME']).toBe(true);    // case-insensitive
    // non-local → false
    expect(got['example.com']).toBe(false);
    expect(got['notlvh.me']).toBe(false);      // suffix must be dot-anchored
    expect(got['evil-lvh.me']).toBe(false);    // not a .lvh.me subdomain
    expect(got['']).toBe(false);
  });

  // --- wired handler: click on a local link, embedded, posts to the parent ---
  // Embeds the chat page in a same-origin iframe (so window.parent !== window,
  // matching the swe-swe embed) with parent_url set. A recorder listener on
  // #messages captures the app handler's preventDefault decision and stops any
  // real navigation from a non-intercepted anchor; the parent window captures
  // the posted agent-chat-open-preview URL.
  async function embed(page) {
    await page.goto(server.url);
    await page.evaluate((base) => {
      window.__preview = [];
      window.addEventListener('message', (e) => {
        if (e && e.data && e.data.type === 'agent-chat-open-preview') {
          window.__preview.push(e.data.url);
        }
      });
      const f = document.createElement('iframe');
      f.name = 'embed';
      f.style.width = '600px';
      f.style.height = '400px';
      f.src = base + '/?parent_url=' + encodeURIComponent('https://parent.example/app/');
      document.body.appendChild(f);
    }, server.url);

    const frameLoc = page.frameLocator('iframe[name="embed"]');
    await expect(frameLoc.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    const frame = page.frame({ name: 'embed' });
    // Recorder: runs after the app handler (added at load) on the bubble path.
    // Records the app's preventDefault decision, then cancels default so a
    // non-intercepted link never actually navigates the iframe during the test.
    await frame.evaluate(() => {
      window.__lastPrevented = null;
      document.getElementById('messages').addEventListener('click', (e) => {
        window.__lastPrevented = e.defaultPrevented;
        e.preventDefault();
      }, false);
    });
    return frame;
  }

  // Injects an <a> into #messages and dispatches a click with the given
  // modifier/button, returning the app handler's preventDefault decision.
  function clickLink(frame, href, opts) {
    return frame.evaluate(({ href, opts }) => {
      const m = document.getElementById('messages');
      const a = document.createElement('a');
      a.href = href;
      a.textContent = 'link';
      m.appendChild(a);
      const ev = new MouseEvent('click', Object.assign({
        bubbles: true, cancelable: true, button: 0,
      }, opts || {}));
      a.dispatchEvent(ev);
      return window.__lastPrevented;
    }, { href, opts });
  }

  test('local link click is intercepted and posts agent-chat-open-preview to the parent', async ({ page }) => {
    const frame = await embed(page);

    const prevented = await clickLink(frame, 'http://abc.lvh.me:3000/dashboard');
    expect(prevented).toBe(true);

    await page.waitForFunction(() => window.__preview && window.__preview.length > 0, null, { timeout: 2000 });
    const urls = await page.evaluate(() => window.__preview);
    expect(urls).toContain('http://abc.lvh.me:3000/dashboard');
  });

  test('external link click is NOT intercepted (keeps default new-tab behaviour)', async ({ page }) => {
    const frame = await embed(page);

    const prevented = await clickLink(frame, 'https://example.com/page');
    expect(prevented).toBe(false);

    // Give any (erroneous) postMessage a tick to arrive, then assert none did.
    await page.waitForTimeout(300);
    const urls = await page.evaluate(() => window.__preview);
    expect(urls).toEqual([]);
  });

  test('modified click on a local link is NOT intercepted (escape hatch → new tab)', async ({ page }) => {
    const frame = await embed(page);

    const prevented = await clickLink(frame, 'http://localhost:8080/x', { metaKey: true });
    expect(prevented).toBe(false);

    await page.waitForTimeout(300);
    const urls = await page.evaluate(() => window.__preview);
    expect(urls).toEqual([]);
  });

  test('standalone chat (not embedded) does not intercept local links', async ({ page }) => {
    // Top-level page, no parent_url: window.parent === window, so the handler
    // must bail and let the local link open normally (new tab).
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const prevented = await page.evaluate(() => {
      let prevented = null;
      const m = document.getElementById('messages');
      m.addEventListener('click', (e) => { prevented = e.defaultPrevented; e.preventDefault(); }, false);
      const a = document.createElement('a');
      a.href = 'http://abc.lvh.me:3000/x';
      a.textContent = 'link';
      m.appendChild(a);
      a.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 }));
      return prevented;
    });
    expect(prevented).toBe(false);
  });
});
