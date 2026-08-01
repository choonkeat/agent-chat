// @ts-check
// Workspace-file links -> the embedder's Files pane. When agent-chat is
// embedded in a host (swe-swe) that passes files_url, a markdown link written
// as a bare workspace path -- [main.go](cmd/swe-swe/main.go) -- renders with an
// absolute href into the file-browser origin (so cmd-click still opens a real
// browser tab) plus data-files-path. A plain click on such a link is
// preventDefault'd and posted to the parent as
// { type: 'agent-chat-open-files', path } so the host loads it in its Files
// pane. Root-anchored links, absolute URLs, images, and standalone (no
// files_url) chat keep their previous parent-relative behaviour.
//
// Mirrors local-link-preview.spec.cjs: the classifier and the interceptor live
// at the top level of the classic script in client-dist/app.js, so the
// classifier is reachable on `window` and the handler is wired to #messages.
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

const FILES_BASE = 'http://127.0.0.1:19999/';

function startServer() {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-files-link-'));
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

test.describe('workspace-file links -> Files pane', () => {
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

  // --- classifier: which link targets are workspace files ---
  test('workspaceFilePath accepts bare relative paths and rejects everything else', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const got = await page.evaluate((filesBase) => {
      window.filesBaseUrl = filesBase;
      const urls = [
        'cmd/swe-swe/main.go', './docs/readme.md', 'README.md',
        '/api/fork/x', '/.swe-swe/uploads/a.png', '//evil.example/x',
        'https://example.com/x', 'mailto:a@b.c', '#anchor', '?q=1', '',
      ];
      const out = {};
      for (const u of urls) out[u] = window.workspaceFilePath(u);
      return out;
    }, FILES_BASE);

    // bare relative paths -> workspace file
    expect(got['cmd/swe-swe/main.go']).toBe('cmd/swe-swe/main.go');
    expect(got['./docs/readme.md']).toBe('docs/readme.md');  // leading ./ stripped
    expect(got['README.md']).toBe('README.md');
    // root-anchored stays with the parent app, which is what serves those
    expect(got['/api/fork/x']).toBe('');
    expect(got['/.swe-swe/uploads/a.png']).toBe('');
    // absolute / protocol-relative / scheme / fragment / query / empty
    expect(got['//evil.example/x']).toBe('');
    expect(got['https://example.com/x']).toBe('');
    expect(got['mailto:a@b.c']).toBe('');
    expect(got['#anchor']).toBe('');
    expect(got['?q=1']).toBe('');
    expect(got['']).toBe('');
  });

  // --- classifier: absolute paths inside this session's own directory ---
  // The `@/` autocomplete emits absolute paths (confined to a roots allowlist),
  // so a user can hand me one for a file that IS in this session's workspace.
  // Those must reach the Files pane too; the pane is rooted at the same
  // directory the server runs in, so anything outside it is unservable and
  // stays a plain parent link.
  test('workspaceFilePath accepts an absolute path inside the workspace root', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const got = await page.evaluate((filesBase) => {
      window.filesBaseUrl = filesBase;
      window.workspaceRoot = '/worktrees/try123';
      const urls = [
        '/worktrees/try123/cmd/main.go',   // inside -> relative to the root
        '/worktrees/try123/README.md',
        '/worktrees/try123',              // the root itself, not a file in it
        '/worktrees/try123-old/cmd/main.go', // sibling: boundary must not match
        '/repos/agent-chat/workspace/main.go', // another checkout entirely
        '/api/fork/x',                    // parent app's own route
        '/.swe-swe/uploads/a.png',
      ];
      const out = {};
      for (const u of urls) out[u] = window.workspaceFilePath(u);
      return out;
    }, FILES_BASE);

    expect(got['/worktrees/try123/cmd/main.go']).toBe('cmd/main.go');
    expect(got['/worktrees/try123/README.md']).toBe('README.md');
    expect(got['/worktrees/try123']).toBe('');
    // Prefix match must stop at a path boundary, or a sibling worktree whose
    // name merely starts with ours would be served as if it were ours.
    expect(got['/worktrees/try123-old/cmd/main.go']).toBe('');
    expect(got['/repos/agent-chat/workspace/main.go']).toBe('');
    expect(got['/api/fork/x']).toBe('');
    expect(got['/.swe-swe/uploads/a.png']).toBe('');
  });

  test('with no workspace root known, every absolute path stays with the parent', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const got = await page.evaluate((filesBase) => {
      window.filesBaseUrl = filesBase;
      window.workspaceRoot = '';
      return {
        abs: window.workspaceFilePath('/worktrees/try123/cmd/main.go'),
        api: window.workspaceFilePath('/api/fork/x'),
      };
    }, FILES_BASE);

    expect(got.abs).toBe('');
    expect(got.api).toBe('');
  });

  test('workspaceRoot is inlined by the server as the directory it runs in', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // startServer() runs the binary with cwd = server.dir (a temp dir), and the
    // Files pane in swe-swe is rooted at that same working directory.
    const root = await page.evaluate(() => window.workspaceRoot);
    expect(root).toBe(fs.realpathSync(server.dir));
  });

  test('renderMarkdown turns an in-workspace absolute path into a Files link', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await page.evaluate((filesBase) => {
      window.parentBaseUrl = 'https://parent.example/app/';
      window.filesBaseUrl = filesBase;
      window.workspaceRoot = '/worktrees/try123';
      return window.renderMarkdown('[main.go](/worktrees/try123/cmd/main.go)');
    }, FILES_BASE);

    expect(html).toContain('href="http://127.0.0.1:19999/cmd/main.go"');
    expect(html).toContain('data-files-path="cmd/main.go"');
  });

  test('files_url query param is read into filesBaseUrl on load', async ({ page }) => {
    await gotoRetry(page, server.url + '/?files_url=' + encodeURIComponent(FILES_BASE));
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const base = await page.evaluate(() => window.filesBaseUrl);
    expect(base).toBe(FILES_BASE);
  });

  // --- rendering: absolute href (cmd-click) + data-files-path (plain click) ---
  test('renderMarkdown gives a workspace path an absolute Files href and data-files-path', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await page.evaluate((filesBase) => {
      window.parentBaseUrl = 'https://parent.example/app/';
      window.filesBaseUrl = filesBase;
      return window.renderMarkdown('[main.go](cmd/swe-swe/main.go)');
    }, FILES_BASE);

    expect(html).toContain('href="http://127.0.0.1:19999/cmd/swe-swe/main.go"');
    expect(html).toContain('data-files-path="cmd/swe-swe/main.go"');
    expect(html).toContain('>main.go</a>');
  });

  test('root-anchored links and images still resolve against the parent, not Files', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const out = await page.evaluate((filesBase) => {
      window.parentBaseUrl = 'https://parent.example/app/';
      window.filesBaseUrl = filesBase;
      return {
        link: window.renderMarkdown('[upload](/.swe-swe/uploads/a.png)'),
        image: window.renderMarkdown('![shot](screenshots/a.png)'),
      };
    }, FILES_BASE);

    expect(out.link).toContain('href="https://parent.example/.swe-swe/uploads/a.png"');
    expect(out.link).not.toContain('data-files-path');
    // Images are served by the parent, so they must not be repointed at Files.
    expect(out.image).toContain('src="https://parent.example/app/screenshots/a.png"');
  });

  test('without files_url a bare relative link keeps its previous behaviour', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    const html = await page.evaluate(() => {
      window.parentBaseUrl = '';
      window.filesBaseUrl = '';
      return window.renderMarkdown('[docs](docs/readme.md)');
    });

    expect(html).toContain('<a href="docs/readme.md"');
    expect(html).not.toContain('data-files-path');
  });

  // --- wired handler: click on a workspace-file link, embedded, posts to parent ---
  // Same embed harness as local-link-preview.spec.cjs.
  async function embed(page) {
    await gotoRetry(page, server.url);
    await page.evaluate(({ base, filesBase }) => {
      window.__files = [];
      window.__preview = [];
      window.addEventListener('message', (e) => {
        if (e && e.data && e.data.type === 'agent-chat-open-files') window.__files.push(e.data.path);
        if (e && e.data && e.data.type === 'agent-chat-open-preview') window.__preview.push(e.data.url);
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
    // Recorder: runs after the app handler, records its preventDefault decision
    // and cancels default so a non-intercepted link never navigates the iframe.
    await frame.evaluate(() => {
      window.__lastPrevented = null;
      document.getElementById('messages').addEventListener('click', (e) => {
        window.__lastPrevented = e.defaultPrevented;
        e.preventDefault();
      }, false);
    });
    return frame;
  }

  // Injects a rendered workspace-file anchor into #messages and clicks it.
  function clickFileLink(frame, markdown, opts) {
    return frame.evaluate(({ markdown, opts }) => {
      const m = document.getElementById('messages');
      const holder = document.createElement('div');
      holder.innerHTML = window.renderMarkdown(markdown);
      m.appendChild(holder);
      const a = holder.querySelector('a');
      const ev = new MouseEvent('click', Object.assign({
        bubbles: true, cancelable: true, button: 0,
      }, opts || {}));
      a.dispatchEvent(ev);
      return window.__lastPrevented;
    }, { markdown, opts });
  }

  test('plain click on a workspace-file link posts agent-chat-open-files to the parent', async ({ page }) => {
    const frame = await embed(page);

    const prevented = await clickFileLink(frame, '[main.go](cmd/swe-swe/main.go)');
    expect(prevented).toBe(true);

    await page.waitForFunction(() => window.__files && window.__files.length > 0, null, { timeout: 2000 });
    const paths = await page.evaluate(() => window.__files);
    expect(paths).toContain('cmd/swe-swe/main.go');
    // It must NOT also be misrouted into App Preview: the Files href is a
    // 127.0.0.1 URL, which the local-host classifier would otherwise claim.
    const previews = await page.evaluate(() => window.__preview);
    expect(previews).toEqual([]);
  });

  test('modified click on a workspace-file link is NOT intercepted (escape hatch -> new tab)', async ({ page }) => {
    const frame = await embed(page);

    const prevented = await clickFileLink(frame, '[main.go](cmd/swe-swe/main.go)', { metaKey: true });
    expect(prevented).toBe(false);

    await page.waitForTimeout(300);
    const paths = await page.evaluate(() => window.__files);
    expect(paths).toEqual([]);
  });
});
