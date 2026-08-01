// @ts-check
// Bare workspace paths -> Files-pane links. A bubble that merely mentions
// client-dist/app.js becomes clickable, without anyone writing markdown link
// syntax. The server is the only side that can stat, so it attaches the paths
// its text really names (`file_paths`) and the browser links exactly those.
//
// Two layers here:
//   1. Full stack — a real server in a real workspace, a page loaded with
//      files_url, and an agent bubble pushed through MCP. Proves the switch,
//      the annotation and the rendering all line up.
//   2. Unit-style through window.renderMarkdown, for the rules that are
//      awkward to provoke end-to-end (code blocks, boundaries, nesting).
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { gotoRetry } = require('./goto-retry.cjs');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);

const FILES_BASE = 'http://127.0.0.1:19998/';

// The workspace the server runs in, so the paths under test genuinely exist.
function seedWorkspace(dir) {
  fs.mkdirSync(path.join(dir, 'client-dist'), { recursive: true });
  fs.mkdirSync(path.join(dir, 'docs', 'adr'), { recursive: true });
  fs.writeFileSync(path.join(dir, 'client-dist', 'app.js'), 'x');
  fs.writeFileSync(path.join(dir, 'Makefile'), 'x');
}

function startServer() {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-autolink-'));
    seedWorkspace(dir);
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

// send_progress publishes an agent bubble without blocking on a reply, which is
// what makes it usable as a test injector.
async function pushAgentBubble(baseUrl, text) {
  const res = await fetch(`${baseUrl}/mcp`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Accept': 'application/json, text/event-stream',
    },
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'tools/call',
      params: { name: 'send_progress', arguments: { text } },
      id: Date.now(),
    }),
  });
  return res.text();
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

test.describe('bare workspace paths -> Files-pane links', () => {
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

  // --- full stack ---

  test('an agent bubble mentioning a real path renders one Files-pane link', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // Only real paths link: client-dist/app.js exists in the seeded workspace,
    // Ctrl/Cmd and client-dist/nope.js do not.
    await pushAgentBubble(server.url, 'edit client-dist/app.js, press Ctrl/Cmd, ignore client-dist/nope.js');

    const bubble = page.locator('.bubble.agent').last();
    await expect(bubble).toContainText('press Ctrl/Cmd', { timeout: 5000 });

    const links = bubble.locator('a[data-files-path]');
    await expect(links).toHaveCount(1);
    await expect(links.first()).toHaveAttribute('data-files-path', 'client-dist/app.js');
    await expect(links.first()).toHaveText('@client-dist/app.js');
    await expect(links.first()).toHaveAttribute('href', FILES_BASE + 'client-dist/app.js');
  });

  test('a directory link carries the trailing slash', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await pushAgentBubble(server.url, 'the writeups live in docs/adr today');

    const bubble = page.locator('.bubble.agent').last();
    await expect(bubble).toContainText('the writeups live in', { timeout: 5000 });

    const link = bubble.locator('a[data-files-path]').first();
    await expect(link).toHaveAttribute('data-files-path', 'docs/adr');
    await expect(link).toHaveText('@docs/adr/');
  });

  test('an @-prefixed path is not double-prefixed', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await pushAgentBubble(server.url, 'run @Makefile now');

    const bubble = page.locator('.bubble.agent').last();
    await expect(bubble).toContainText('run', { timeout: 5000 });

    const link = bubble.locator('a[data-files-path]').first();
    await expect(link).toHaveText('@Makefile');
    await expect(bubble).not.toContainText('@@');
  });

  test('without files_url the same text stays plain', async ({ page }) => {
    // No files_url: no Files pane, so a link would lead nowhere. The client
    // renders the path as the plain text it has always been, whatever the
    // server attached.
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await pushAgentBubble(server.url, 'standalone mentions client-dist/app.js plainly');

    const bubble = page.locator('.bubble.agent').last();
    await expect(bubble).toContainText('standalone mentions', { timeout: 5000 });
    await expect(bubble.locator('a[data-files-path]')).toHaveCount(0);
    await expect(bubble).toContainText('client-dist/app.js');
  });

  // --- rendering rules, driven straight through renderMarkdown ---

  const FILE_PATHS = [
    { raw: 'client-dist/app.js', path: 'client-dist/app.js' },
    { raw: 'docs/adr', path: 'docs/adr', dir: true },
  ];

  async function render(page, text, filePaths) {
    return page.evaluate(
      ([filesBase, t, fp]) => {
        window.filesBaseUrl = filesBase;
        return window.renderMarkdown(t, fp);
      },
      [FILES_BASE, text, filePaths === undefined ? FILE_PATHS : filePaths]
    );
  }

  test('a code span holding only a real path becomes a link', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // Backticks are the most natural way to write a path in chat, so this is
    // the same link a bare path gets — one appearance for one behaviour.
    const html = await render(page, 'see `client-dist/app.js` here');
    expect(html).toContain('data-files-path="client-dist/app.js"');
    expect(html).toContain('>@client-dist/app.js</a>');
    expect(html).not.toContain('<code>');
    expect((html.match(/<a /g) || []).length).toBe(1);
  });

  test('a code span with anything else in it stays literal', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // Whole-content match only — partial rewriting inside a span would re-open
    // the hole the stash exists to close.
    const html = await render(page, 'run `see client-dist/app.js` now');
    expect(html).toContain('<code>see client-dist/app.js</code>');
    expect(html).not.toContain('data-files-path');
  });

  test('a code span holding a path that does not exist stays literal', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // client-dist/nope.js is absent from file_paths because it is absent from
    // disk, so there is nothing to link to.
    const html = await render(page, 'see `client-dist/nope.js` here');
    expect(html).toContain('<code>client-dist/nope.js</code>');
    expect(html).not.toContain('data-files-path');
  });

  test('a fenced code block is still the way to show a path literally', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await render(page, 'x\n```\nclient-dist/app.js\n```\ny');
    expect(html).toContain('<pre><code>');
    expect(html).not.toContain('data-files-path');
  });

  test('an explicit markdown link is not wrapped twice', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await render(page, 'see [the app](client-dist/app.js) here');
    expect((html.match(/<a /g) || []).length).toBe(1);
    expect(html).toContain('>the app</a>');
  });

  test('a bare URL containing a workspace path stays a single link', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await render(page, 'see https://example.dev/client-dist/app.js ok');
    expect((html.match(/<a /g) || []).length).toBe(1);
    expect(html).not.toContain('data-files-path');
  });

  test('a longer neighbouring path is not matched', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // Only client-dist/app.js is in the list, so the .jsx sibling must not fire.
    const html = await render(page, 'see client-dist/app.jsx here');
    expect(html).not.toContain('data-files-path');
  });

  test('no file_paths on the event leaves the text untouched', async ({ page }) => {
    await gotoRetry(page, `${server.url}?files_url=${encodeURIComponent(FILES_BASE)}`);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await render(page, 'see client-dist/app.js here', null);
    expect(html).not.toContain('<a ');
    expect(html).toContain('client-dist/app.js');
  });
});
