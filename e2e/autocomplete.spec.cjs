// @ts-check
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

// CDP endpoint for the remote Chrome browser.
// Prefer explicit CDP_ENDPOINT, then derive from BROWSER_CDP_PORT (swe-swe env),
// then fall back to the legacy chrome:9223 default.
const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');

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
function startServer(extraFlags = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-'));
    fs.mkdirSync(path.join(dir, 'docs'));
    fs.writeFileSync(path.join(dir, 'docs', 'autocomplete-api.md'), '# Autocomplete API\n');
    fs.writeFileSync(path.join(dir, 'main.go'), 'package main\n');
    fs.writeFileSync(path.join(dir, 'README.md'), '# README\n');

    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    // Build a clean env: inherit process.env but remove AGENT_CHAT_* vars
    // that could leak state from the host (e.g. shared event log).
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';

    const proc = spawn(bin, ['-no-stdio-mcp', ...extraFlags], {
      cwd: dir,
      env: cleanEnv,
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
      r => r && r.results && r.results.some(f => {
        var val = typeof f === 'string' ? f : (f.v || '');
        return val.includes('docs');
      })
    );
    expect(hasDocResult).toBe(true);
  });

  test('selecting @filepath inserts trigger + value', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    await typeAndWait(page, textarea, '@mai');

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    // Select the first option (should be main.go)
    const firstOption = dropdown.locator('.ac-option').first();
    await firstOption.click();

    // The input should contain @main.go (trigger kept)
    const value = await textarea.inputValue();
    expect(value).toContain('@main.go');
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

test.describe('Autocomplete :emoji', () => {
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

  test('typing :heart shows emoji suggestions with replace_trigger', async ({ page }) => {
    const autocompleteResponses = [];
    page.on('response', async (res) => {
      if (res.url().includes('/autocomplete')) {
        try { autocompleteResponses.push(await res.json()); } catch {}
      }
    });

    const textarea = await setupPage(page, server.url);
    await typeAndWait(page, textarea, ':heart');

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    // Should have emoji options
    const optionEls = dropdown.locator('.ac-option');
    const optionCount = await optionEls.count();
    expect(optionCount).toBeGreaterThan(0);

    // Response should have replace_trigger: true
    const emojiResponse = autocompleteResponses.find(
      r => r && r.replace_trigger === true
    );
    expect(emojiResponse).toBeDefined();
    expect(emojiResponse.results.length).toBeGreaterThan(0);
  });

  test('selecting emoji replaces trigger character', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    await typeAndWait(page, textarea, ':thumbsup');

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    // Select the first option (should be 👍)
    const firstOption = dropdown.locator('.ac-option').first();
    await firstOption.click();

    // The input should contain just the emoji, NOT `:👍`
    const value = await textarea.inputValue();
    expect(value).not.toContain(':');
    expect(value).toContain('👍');
  });

  test('typing :zzzznotanemoji shows no results', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    await typeAndWait(page, textarea, ':zzzznotanemoji');

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    const optionEls = dropdown.locator('.ac-option');
    expect(await optionEls.count()).toBe(0);

    const statusEl = dropdown.locator('.ac-status');
    expect(await statusEl.count()).toBe(1);
    const statusText = await statusEl.textContent();
    expect(statusText).toContain('zzzznotanemoji');
  });

  test('client-side cache filter matches hint labels', async ({ page }) => {
    const textarea = await setupPage(page, server.url);

    // Type a short emoji query to populate the cache with results that
    // have hint labels (keyword text). ":thu" returns thumbsup, etc.
    await typeAndWait(page, textarea, ':thu');

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    // Verify we got cached results
    const optionEls = dropdown.locator('.ac-option');
    const initialCount = await optionEls.count();
    expect(initialCount).toBeGreaterThan(0);

    // Now extend the query — this triggers client-side cache filtering.
    // The emoji values are emoji characters (e.g. 👍) which don't contain
    // "thumbsup", but the hint label does. Without hint matching this
    // would filter to zero results.
    await textarea.pressSequentially('mbsup', { delay: 50 });
    await page.waitForTimeout(500);

    // Should still have results because hint labels are matched
    await expect(dropdown).toHaveClass(/visible/);
    const filteredCount = await dropdown.locator('.ac-option').count();
    expect(filteredCount).toBeGreaterThan(0);

    // The thumbsup emoji should be in the results
    const options = await dropdown.locator('.ac-option').allTextContents();
    expect(options.some(opt => opt.includes('👍'))).toBe(true);

    // Hint chars that satisfied the fuzzy match should be highlighted —
    // not just the value chars. Find the option containing 👍 and check
    // its .ac-hint span has at least one .ac-highlight inside it.
    const thumbsOption = dropdown.locator('.ac-option').filter({ hasText: '👍' }).first();
    const hintHighlightCount = await thumbsOption.locator('.ac-hint .ac-highlight').count();
    expect(hintHighlightCount).toBeGreaterThan(0);
  });

  test('primary keyword ranks above secondary keyword on tie', async ({ page }) => {
    const autocompleteResponses = [];
    page.on('response', async (res) => {
      if (res.url().includes('/autocomplete')) {
        try { autocompleteResponses.push(await res.json()); } catch {}
      }
    });

    const textarea = await setupPage(page, server.url);
    // "heart" is the PRIMARY keyword for ❤️ but a SECONDARY keyword for 💌
    // (love_letter, email, envelope, heart). Both score identically on
    // tier/longestRun/span/length (the keyword "heart" is identical), so
    // the primary-vs-secondary tiebreaker (kwIdx) decides — ❤️ should win.
    await typeAndWait(page, textarea, ':heart');

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    const optionEls = dropdown.locator('.ac-option');
    expect(await optionEls.count()).toBeGreaterThan(0);

    // First option should be ❤️ (red heart), not 💌 (love letter)
    const firstOption = await optionEls.first().textContent();
    expect(firstOption).toContain('❤');
  });
});

test.describe('Autocomplete error handling', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => {
    // Start server with a custom trigger pointing to a broken endpoint
    server = await startServer([
      '-autocomplete-triggers', '@=builtin:filepath,:=builtin:emoji,/=http://localhost:1/broken',
    ]);
  });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('failing autocomplete endpoint shows error in dropdown', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    // Type "/" trigger which points to broken endpoint
    await typeAndWait(page, textarea, '/test');

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    // Should show error status, not selectable options
    const optionEls = dropdown.locator('.ac-option');
    expect(await optionEls.count()).toBe(0);

    const statusEl = dropdown.locator('.ac-status');
    expect(await statusEl.count()).toBe(1);
    const statusText = await statusEl.textContent();
    expect(statusText).toMatch(/Error/i);
  });
});
