// @ts-check
// [[name]] wiki links -> the embedder's Files pane. With files_url set,
// `[[parse-dont-validate]]` renders as an anchor to md-serve's
// `_md-serve-resolve?name=...`, which picks the file at click time (one match
// opens it, none or several open the finder). Same anchor shape as a workspace
// file link, so the existing click handler posts agent-chat-open-files.
// Setup copied from files-link.spec.cjs.
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

const FILES_BASE = 'http://127.0.0.1:19999/';

function startServer() {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-wikilink-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = serverPort();

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

test.describe('[[name]] wiki links -> Files pane', () => {
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

  async function render(page, markdown, filesBase) {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    return page.evaluate(({ markdown, filesBase }) => {
      window.parentBaseUrl = 'https://parent.example/app/';
      window.filesBaseUrl = filesBase;
      const holder = document.createElement('div');
      holder.innerHTML = window.renderMarkdown(markdown);
      const a = holder.querySelector('a');
      return {
        html: holder.innerHTML,
        text: holder.textContent,
        anchors: holder.querySelectorAll('a').length,
        filesPath: a && a.getAttribute('data-files-path'),
        href: a && a.getAttribute('href'),
        title: a && a.getAttribute('title'),
        linkText: a && a.textContent,
        ems: holder.querySelectorAll('em, strong').length,
      };
    }, { markdown, filesBase });
  }

  test('[[name]] renders as a resolve link with the bare name as its text', async ({ page }) => {
    const r = await render(page, 'see [[parse-dont-validate]] for why', FILES_BASE);
    expect(r.filesPath).toBe('_md-serve-resolve?name=parse-dont-validate');
    expect(r.href).toBe(FILES_BASE + '_md-serve-resolve?name=parse-dont-validate');
    expect(r.linkText).toBe('parse-dont-validate');
    expect(r.title).toBe('Find parse-dont-validate in Files');
    expect(r.text).toBe('see parse-dont-validate for why');
  });

  test('[[name]] inside a code span or fenced block stays literal', async ({ page }) => {
    const span = await render(page, 'write `[[x]]` to link', FILES_BASE);
    expect(span.anchors).toBe(0);
    expect(span.text).toContain('[[x]]');

    const fenced = await render(page, '```\n[[x]]\n```', FILES_BASE);
    expect(fenced.anchors).toBe(0);
    expect(fenced.text).toContain('[[x]]');
  });

  test('underscores in the name are not turned into emphasis', async ({ page }) => {
    const r = await render(page, '[[project_mca_hub]] and [[pkg__init__]]', FILES_BASE);
    expect(r.ems).toBe(0);
    expect(r.anchors).toBe(2);
    expect(r.linkText).toBe('project_mca_hub');
    expect(r.html).toContain('_md-serve-resolve?name=pkg__init__');
  });

  test('look-alikes from real chat logs stay plain text', async ({ page }) => {
    for (const s of ['[[:space:]]', '[[...]]', "[['submission', String(row.id)]]", '[[880, 0]]']) {
      const r = await render(page, s, FILES_BASE);
      expect(r.anchors, s).toBe(0);
      expect(r.text, s).toBe(s);
    }
  });

  test('[[a]](b) becomes a wiki link followed by plain (b)', async ({ page }) => {
    const r = await render(page, '[[a]](b)', FILES_BASE);
    expect(r.anchors).toBe(1);
    expect(r.filesPath).toBe('_md-serve-resolve?name=a');
    expect(r.text).toBe('a(b)');
  });

  test('without files_url the text stays [[name]] with no anchor', async ({ page }) => {
    const r = await render(page, '[[parse-dont-validate]]', '');
    expect(r.anchors).toBe(0);
    expect(r.text).toBe('[[parse-dont-validate]]');
  });

  test('plain click on a wiki link posts agent-chat-open-files to the parent', async ({ page }) => {
    await gotoRetry(page, server.url);
    await page.evaluate(({ base, filesBase }) => {
      window.__files = [];
      window.addEventListener('message', (e) => {
        if (e && e.data && e.data.type === 'agent-chat-open-files') window.__files.push(e.data.path);
      });
      const f = document.createElement('iframe');
      f.name = 'embed';
      f.style.width = '600px';
      f.style.height = '400px';
      f.src = base + '/?parent_url=' + encodeURIComponent('https://parent.example/app/')
        + '&files_url=' + encodeURIComponent(filesBase);
      document.body.appendChild(f);
    }, { base: server.url, filesBase: FILES_BASE });

    const frameLoc = page.frameLocator('iframe[name="embed"]');
    await expect(frameLoc.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    const frame = page.frame({ name: 'embed' });
    const prevented = await frame.evaluate(() => {
      window.__lastPrevented = null;
      const m = document.getElementById('messages');
      m.addEventListener('click', (e) => {
        window.__lastPrevented = e.defaultPrevented;
        e.preventDefault();
      }, false);
      const holder = document.createElement('div');
      holder.innerHTML = window.renderMarkdown('[[parse-dont-validate]]');
      m.appendChild(holder);
      holder.querySelector('a').dispatchEvent(new MouseEvent('click', {
        bubbles: true, cancelable: true, button: 0,
      }));
      return window.__lastPrevented;
    });
    expect(prevented).toBe(true);

    await page.waitForFunction(() => window.__files && window.__files.length > 0, null, { timeout: 2000 });
    const paths = await page.evaluate(() => window.__files);
    expect(paths).toEqual(['_md-serve-resolve?name=parse-dont-validate']);
  });
});
