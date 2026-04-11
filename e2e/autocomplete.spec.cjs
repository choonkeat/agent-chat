// @ts-check
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { spawn } = require('child_process');
const http = require('http');
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

// --- /slash-command ranking regression test ---
//
// See .swe-swe/docs/autocomplete-rank-bug.md. Reproduces the swe-swe
// scenario where `/run` sinks value-prefix matches (run-elm-demo, run-www)
// below hint-fuzzy matches because the client was reusing an unsorted
// empty-query cache without re-ranking.

/**
 * Start a tiny fake /slash-command provider that mirrors the real
 * swe-swe autocomplete server: empty query returns items in
 * discovery order (unsorted), non-empty query sorts by the same
 * tier-based ranking as the server. Listens on a random port.
 */
function startSlashProvider() {
  // Items in intentional provider/discovery order — system commands
  // first (whose hints fuzzy-match "run"), project-level commands
  // (run-elm-demo, run-www) last. Mirrors the swe-swe bug repro.
  const items = [
    { v: 'ck:draft-pr', h: 'Draft a pull request' },
    { v: 'ck:execute-step-by-step', h: 'Execute step by step' },
    { v: 'ck:plan-carefully', h: 'Plan carefully before running any commands' },
    { v: 'ck:plan-simpler', h: 'Plan a simpler version of a running task' },
    { v: 'ck:research', h: 'Research a topic or unknown area' },
    { v: 'ck:resume-session', h: 'Resume a previous session' },
    { v: 'ck:save-session', h: 'Save the running session to disk' },
    { v: 'ck:update-docs', h: 'Update documentation' },
    { v: 'swe-swe:debug-preview-page', h: 'Debug a running preview page' },
    { v: 'swe-swe:debug-with-app-preview', h: 'Debug with app preview, no running server needed' },
    { v: 'swe-swe:execute-in-worktree', h: 'Execute inside a worktree, running steps' },
    { v: 'swe-swe:extract-skills', h: 'Extract reusable skills from a running task' },
    { v: 'swe-swe:merge-worktree', h: 'Merge a worktree branch' },
    { v: 'swe-swe:plan-carefully', h: 'Plan before running anything' },
    { v: 'swe-swe:setup', h: 'Configure a running setup' },
    { v: 'tdspec:audit', h: 'Audit the running spec against code' },
    { v: 'tdspec:docs', h: 'Generate spec HTML docs, no running server needed' },
    { v: 'tdspec:help', h: 'Help running tdspec methodology' },
    { v: 'tdspec:init', h: 'Initialize a tdspec project (nothing running yet)' },
    { v: 'tdspec:serve', h: 'Serve tdspec docs by running the server' },
    { v: 'run-elm-demo', h: 'Run the Elm demo server' },
    { v: 'run-www', h: 'Run the www dev server' },
  ];

  // Reference client-side ranking, duplicated here so the provider
  // behaves like the real swe-swe server (which sorts non-empty queries).
  function fuzzyMetrics(s, q) {
    let qi = 0, first = -1, prev = -2, curRun = 0, longest = 0;
    for (let i = 0; i < s.length && qi < q.length; i++) {
      if (s.charCodeAt(i) === q.charCodeAt(qi)) {
        if (first < 0) first = i;
        curRun = (i === prev + 1) ? curRun + 1 : 1;
        if (curRun > longest) longest = curRun;
        prev = i;
        qi++;
      }
    }
    if (qi === q.length) return { ok: true, longestRun: longest, span: prev - first + 1 };
    return { ok: false, longestRun: 0, span: 0 };
  }
  function score(it, q) {
    const lv = it.v.toLowerCase();
    const lh = (it.h || '').toLowerCase();
    const length = it.v.length;
    if (lv === q) return [5, q.length, q.length, length];
    if (lv.indexOf(q) === 0) return [4, q.length, q.length, length];
    if (lv.indexOf(q) >= 0) return [3, q.length, q.length, length];
    const mv = fuzzyMetrics(lv, q);
    if (mv.ok) return [2, mv.longestRun, mv.span, length];
    if (lh === '') return [-1, 0, 0, length];
    if (lh.indexOf(q) >= 0) return [1, q.length, q.length, length];
    const mh = fuzzyMetrics(lh, q);
    if (mh.ok) return [0, mh.longestRun, mh.span, length];
    return [-1, 0, 0, length];
  }
  function filterAndSort(q) {
    if (!q) return items.slice(); // empty query — discovery order, unsorted.
    const lq = q.toLowerCase();
    const kept = items
      .map((it, i) => ({ it, s: score(it, lq), i }))
      .filter((d) => d.s[0] >= 0);
    kept.sort((a, b) => {
      if (a.s[0] !== b.s[0]) return b.s[0] - a.s[0];
      if (a.s[1] !== b.s[1]) return b.s[1] - a.s[1];
      if (a.s[2] !== b.s[2]) return a.s[2] - b.s[2];
      if (a.s[3] !== b.s[3]) return a.s[3] - b.s[3];
      return a.i - b.i;
    });
    return kept.map((d) => d.it);
  }

  return new Promise((resolve) => {
    const server = http.createServer((req, res) => {
      let body = '';
      req.on('data', (c) => { body += c.toString(); });
      req.on('end', () => {
        let q = '';
        try { q = (JSON.parse(body || '{}').query) || ''; } catch {}
        const results = filterAndSort(q);
        res.setHeader('Content-Type', 'application/json');
        res.end(JSON.stringify({ results, has_more: false }));
      });
    });
    server.listen(0, '127.0.0.1', () => {
      const addr = server.address();
      const port = typeof addr === 'object' && addr ? addr.port : 0;
      resolve({ server, url: `http://127.0.0.1:${port}/slash` });
    });
  });
}

test.describe('Autocomplete /slash-command ranking', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;
  /** @type {{ server: import('http').Server, url: string } | null} */
  let provider = null;

  test.beforeAll(async () => {
    provider = await startSlashProvider();
    server = await startServer([
      '-autocomplete-triggers', '@=builtin:filepath,:=builtin:emoji,/=' + provider.url,
    ]);
  });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
    if (provider?.server) provider.server.close();
  });

  test('/run ranks value-prefix matches above hint-fuzzy matches', async ({ page }) => {
    const textarea = await setupPage(page, server.url);

    // Type "/" to trigger the empty-query fetch (seeds the cache with the
    // unsorted discovery-order list) then type "run" one char at a time.
    // Prior to the fix the client filtered the empty-query cache without
    // re-ranking, so run-elm-demo / run-www landed at the bottom.
    await textarea.pressSequentially('/', { delay: 50 });
    await page.waitForTimeout(500);
    await textarea.pressSequentially('run', { delay: 80 });
    await page.waitForTimeout(600);

    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    const optionEls = dropdown.locator('.ac-option');
    expect(await optionEls.count()).toBeGreaterThan(0);

    // The top two rows should be the value-prefix matches (in either
    // order — they tie on all score dimensions except stable insertion,
    // which favors run-elm-demo first because length 12 < 7… actually
    // run-www wins on length. Accept either order by name.)
    const texts = await optionEls.allTextContents();
    const top = (texts[0] || '').toLowerCase();
    const second = (texts[1] || '').toLowerCase();
    const prefixNames = ['run-elm-demo', 'run-www'];
    expect(prefixNames.some((n) => top.includes(n))).toBe(true);
    expect(prefixNames.some((n) => second.includes(n))).toBe(true);

    // And specifically: neither hint-fuzzy match should be ranked above
    // a value-prefix match.
    const firstHintFuzzyIdx = texts.findIndex((t) =>
      /ck:|swe-swe:|tdspec:/.test(t)
    );
    const firstValuePrefixIdx = texts.findIndex((t) =>
      /run-elm-demo|run-www/.test(t)
    );
    expect(firstValuePrefixIdx).toBeLessThan(firstHintFuzzyIdx);
  });

  test('acSortByQuery tiers and tiebreaks (unit)', async ({ page }) => {
    // Unit-test the client ranking function directly via page.evaluate.
    // app.js declares acSortByQuery at top level so it's on window.
    await setupPage(page, server.url);

    const result = await page.evaluate(() => {
      // eslint-disable-next-line no-undef
      const sort = acSortByQuery;
      const cases = [];

      // Tier 5: exact value match beats all.
      let a = [{ v: 'run', h: '' }, { v: 'runner', h: '' }, { v: 'rerun', h: '' }];
      sort(a, 'run');
      cases.push({ name: 'tier5-exact', got: a.map((x) => x.v) });

      // Tier 4: value prefix beats tier 3 (contains).
      a = [{ v: 'prerun', h: '' }, { v: 'run-www', h: '' }, { v: 'rerun-thing', h: '' }];
      sort(a, 'run');
      cases.push({ name: 'tier4-prefix', got: a.map((x) => x.v) });

      // Tier 3 (value contains) beats tier 2 (value fuzzy non-contiguous).
      a = [{ v: 'r-u-n', h: '' }, { v: 'arunx', h: '' }];
      sort(a, 'run');
      cases.push({ name: 'tier3-contains', got: a.map((x) => x.v) });

      // Any value tier beats any hint tier.
      a = [{ v: 'zzz', h: 'run now' }, { v: 'arunx', h: '' }];
      sort(a, 'run');
      cases.push({ name: 'value-beats-hint', got: a.map((x) => x.v) });

      // Tier 1 (hint contains) beats tier 0 (hint fuzzy).
      a = [{ v: 'zzz', h: 'sparsely r u n' }, { v: 'yyy', h: 'has run word' }];
      sort(a, 'run');
      cases.push({ name: 'tier1-hint-contains', got: a.map((x) => x.v) });

      // Tiebreak by longestRun inside tier 2 (value fuzzy, non-contiguous).
      // "r_un_x" has a 2-run (u,n consecutive); "r_u_n_x" only 1-runs.
      // Both avoid indexOf('run') matching, so they stay in tier 2.
      a = [{ v: 'r_u_n_x', h: '' }, { v: 'r_un_x', h: '' }];
      sort(a, 'run');
      cases.push({ name: 'tier2-longestRun', got: a.map((x) => x.v) });

      // Tiebreak by length when runs + spans tie (both tier-4 prefix).
      a = [{ v: 'run-www', h: '' }, { v: 'run-elm-demo', h: '' }];
      sort(a, 'run');
      cases.push({ name: 'tier4-length', got: a.map((x) => x.v) });

      // Stability: equal scores preserve input order.
      a = [{ v: 'aaa', h: 'no match at all' }, { v: 'bbb', h: 'also nothing' }];
      sort(a, 'run');
      cases.push({ name: 'stable-no-match', got: a.map((x) => x.v) });

      return cases;
    });

    const byName = Object.fromEntries(result.map((c) => [c.name, c.got]));
    expect(byName['tier5-exact'][0]).toBe('run');
    expect(byName['tier4-prefix'][0]).toBe('run-www');
    expect(byName['tier3-contains'][0]).toBe('arunx');
    expect(byName['value-beats-hint'][0]).toBe('arunx');
    expect(byName['tier1-hint-contains'][0]).toBe('yyy');
    expect(byName['tier2-longestRun'][0]).toBe('r_un_x');
    expect(byName['tier4-length']).toEqual(['run-www', 'run-elm-demo']);
    expect(byName['stable-no-match']).toEqual(['aaa', 'bbb']);
  });
});
