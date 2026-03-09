// @ts-check
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

// CDP endpoint for the remote Chrome browser
const CDP_ENDPOINT = process.env.CDP_ENDPOINT || 'http://chrome:9223';

// Optional slowMo for live viewing (set SLOW_MO=500 to watch in browser)
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);

/**
 * Start agent-chat in a temp directory with known fixture files.
 * Returns { url, proc, dir } — caller must kill proc when done.
 *
 * Fixture files:
 *   docs/autocomplete-api.md
 *   main.go
 *   README.md
 */
function startServer() {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-'));
    fs.mkdirSync(path.join(dir, 'docs'));
    fs.writeFileSync(path.join(dir, 'docs', 'autocomplete-api.md'), '# Autocomplete API\n');
    fs.writeFileSync(path.join(dir, 'main.go'), 'package main\n');
    fs.writeFileSync(path.join(dir, 'README.md'), '# README\n');

    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const proc = spawn(bin, ['-no-stdio-mcp'], {
      cwd: dir,
      env: { ...process.env, AGENT_CHAT_PORT: '0' },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    let stderr = '';
    proc.stderr.on('data', (chunk) => {
      stderr += chunk.toString();
      const match = stderr.match(/Agent Chat UI: (http:\/\/localhost:\d+)/);
      if (match) {
        resolve({ url: match[1], proc, dir });
      }
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

// Custom test that connects to remote Chrome via CDP.
// Reuses the existing browser page (visible in Agent View) instead of
// creating a new context, so viewers can watch tests run live.
const test = base.extend({
  page: async ({}, use) => {
    const browser = await chromium.connectOverCDP(CDP_ENDPOINT, {
      ...(SLOW_MO > 0 && { slowMo: SLOW_MO }),
    });
    // Reuse the first existing page so it's visible in Agent View
    const contexts = browser.contexts();
    const pages = contexts.flatMap(c => c.pages());
    const page = pages[0] || (await browser.newContext()).newPage();
    await use(page);
    // Don't close — we're borrowing the existing page
  },
});

/**
 * Helper: navigate to server, wait for connection, clear input.
 *
 * Manual run equivalent (using Playwright MCP tools):
 *   1. browser_navigate to the server URL
 *   2. Wait for textarea to be enabled (WebSocket connected)
 *   3. browser_click on the textarea
 */
async function setupPage(page, url) {
  await page.goto(url);
  const textarea = page.locator('#chat-input');
  await expect(textarea).toBeEnabled({ timeout: 5000 });
  await textarea.click();
  return textarea;
}

/**
 * Helper: type a query and wait for autocomplete to settle.
 *
 * Manual run equivalent:
 *   1. browser_run_code: pressSequentially(text, { delay: 100 })
 *   2. Wait 1s for debounce + fetch + render
 */
async function typeAndWait(page, textarea, text) {
  await textarea.fill('');
  await textarea.pressSequentially(text, { delay: 50 });
  await page.waitForTimeout(1000);
}

test.describe('Autocomplete @filepath', () => {
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

  test('typing @dom shows matching file suggestions', async ({ page }) => {
    // Collect /autocomplete requests and responses
    const autocompleteRequests = [];
    const autocompleteResponses = [];

    page.on('request', (req) => {
      if (req.url().includes('/autocomplete')) {
        autocompleteRequests.push(req.postDataJSON());
      }
    });
    page.on('response', async (res) => {
      if (res.url().includes('/autocomplete')) {
        try { autocompleteResponses.push(await res.json()); } catch {}
      }
    });

    const textarea = await setupPage(page, server.url);
    await typeAndWait(page, textarea, 'read @dom');

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    // Assert: dropdown should have selectable options (not just a status message)
    const optionEls = dropdown.locator('.ac-option');
    const optionCount = await optionEls.count();
    expect(optionCount).toBeGreaterThan(0);

    // The dropdown should contain a file path matching "doc"
    const options = await optionEls.allTextContents();
    expect(options.some(opt => opt.includes('docs'))).toBe(true);

    // Assert request: at least one request should have query containing "dom"
    const hasDomQuery = autocompleteRequests.some(
      r => r && r.query && r.query.length > 0 && 'dom'.startsWith(r.query)
    );
    expect(hasDomQuery).toBe(true);

    // Assert response: at least one response should have results matching "docs"
    const hasDocResult = autocompleteResponses.some(
      r => r && r.results && r.results.some(f => f.includes('docs'))
    );
    expect(hasDocResult).toBe(true);
  });

  test('typing @xyz shows "No results" with debug info', async ({ page }) => {
    const autocompleteResponses = [];
    page.on('response', async (res) => {
      if (res.url().includes('/autocomplete')) {
        try { autocompleteResponses.push(await res.json()); } catch {}
      }
    });

    const textarea = await setupPage(page, server.url);
    await typeAndWait(page, textarea, '@xyz');

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    // Assert: no selectable options
    const optionEls = dropdown.locator('.ac-option');
    expect(await optionEls.count()).toBe(0);

    // Assert: status message is visible (either "No results" or debug info)
    const statusEl = dropdown.locator('.ac-status');
    expect(await statusEl.count()).toBe(1);
    const statusText = await statusEl.textContent();
    // Should contain the query and path info from the server
    expect(statusText).toContain('xyz');

    // Assert response: server returned empty results with info
    const noResultResponse = autocompleteResponses.find(
      r => r && r.results && r.results.length === 0 && r.info
    );
    expect(noResultResponse).toBeDefined();
    expect(noResultResponse.info).toContain('xyz');
  });
});
