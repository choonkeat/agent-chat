// @ts-check
// Visual proof: renders an image-markdown bubble in the actual chat UI and
// screenshots it. Kept in /e2e so it shares the same harness as the unit-style
// renderMarkdown tests. Skipped from the main suite by default — run with:
//   npx playwright test -c playwright.config.cjs e2e/markdown-images-visual.spec.cjs
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-md-vis-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';
    const proc = spawn(bin, ['-no-stdio-mcp'], { cwd: dir, env: cleanEnv, stdio: ['ignore', 'pipe', 'pipe'] });
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
    const contexts = browser.contexts();
    const pages = contexts.flatMap(c => c.pages());
    const page = pages[0] || (await browser.newContext()).newPage();
    await use(page);
  },
});

test.describe('renderMarkdown — visual', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });
  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('side-by-side bubbles: before vs after image markdown', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // Two bubbles for the same input:
    //   1) Before fix — entire markdown passes through literally because the
    //      old link rule only matched http(s) and there was no image rule.
    //   2) After fix — current renderMarkdown emits <img>.
    // Use a relative URL (matches what users actually hit) and serve a small
    // SVG locally so the <img> renders in the screenshot.
    const sample = '![diagram](/__test_image.svg)';
    await page.route('**/__test_image.svg', (route) => {
      route.fulfill({
        status: 200,
        contentType: 'image/svg+xml',
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="60"><rect width="160" height="60" fill="#22c55e"/><text x="80" y="36" font-family="sans-serif" font-size="18" text-anchor="middle" fill="white">it works!</text></svg>',
      });
    });
    await page.evaluate((sample) => {
      const messages = document.getElementById('messages');
      const quick = document.getElementById('quick-replies');

      function makeBubble(label, htmlBody) {
        const wrap = document.createElement('div');
        wrap.style.margin = '12px';
        const tag = document.createElement('div');
        tag.textContent = label;
        tag.style.cssText = 'font: 12px/1.4 sans-serif; color: #888; margin-bottom: 4px;';
        wrap.appendChild(tag);
        const code = document.createElement('div');
        code.textContent = sample;
        code.style.cssText = 'font: 12px/1.4 monospace; color: #666; margin-bottom: 6px;';
        wrap.appendChild(code);
        const bubble = document.createElement('div');
        bubble.className = 'bubble agent';
        bubble.innerHTML = htmlBody;
        wrap.appendChild(bubble);
        messages.insertBefore(wrap, quick);
      }

      // Old (broken) — the entire markdown passes through as literal text
      // because the old rules only handled http(s) URLs. HTML-escape so the
      // brackets render visibly.
      const esc = sample.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
      makeBubble('Before fix:', esc);

      // New (fixed) — actual renderMarkdown output.
      const fixed = window.renderMarkdown(sample);
      makeBubble('After fix:', fixed);
    }, sample);

    // Sanity: an <img> exists in the second bubble.
    await expect(page.locator('.bubble img')).toHaveCount(1);

    const outPath = path.resolve(__dirname, '..', 'markdown-images-fix.png');
    await page.screenshot({ path: outPath, fullPage: true });
    console.log('Screenshot saved to', outPath);
  });
});
