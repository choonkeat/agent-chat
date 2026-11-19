// @ts-check
// MCP Apps: a tool's page shown inside the chat (client-dist/mcp-app.js).
//
// The page arrives the way `agent-chat wrap` delivers it: POST /api/mcp-app
// with the password agent-chat wrote to <cache dir>/agent-chat/mcp-app-tokens/
// <port>. The server runs with its own XDG_CACHE_HOME, so the spec reads that
// password from its temp dir and never touches the real cache.
//
// Covered:
//   - the sample calendar page (testdata/mcp-apps-sample/calendar.html)
//     appears in an agent bubble, completes the handshake and draws the grid
//     from the tool result;
//   - clicking a free cell: the page's tools/call is refused, so it falls
//     back to ui/message, which arrives as a user chat message;
//   - a reload replays the page with the same result;
//   - a page can't post chat messages on its own (no click in it) and can't
//     reach the network (CSP).
//   - step 2: with a wrapper (played here by the spec, polling
//     /api/mcp-app/calls), a click runs the tool in place after "Let the page
//     run its tools?": Allow / Deny, plus Always Allow after an Allow in an earlier
//     session (seeded allow file), and Settings can forget it.
const { test, expect, chromium } = require('@playwright/test');
const { gotoRetry } = require('./goto-retry.cjs');
const { serverPort } = require('./server-port.cjs');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);

const CALENDAR_HTML = fs.readFileSync(
  path.resolve(__dirname, '..', 'testdata', 'mcp-apps-sample', 'calendar.html'), 'utf8');

function startServer({ allow } = {}) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-'));
    if (allow) {
      fs.mkdirSync(path.join(dir, 'cache', 'agent-chat'), { recursive: true });
      fs.writeFileSync(path.join(dir, 'cache', 'agent-chat', 'mcp-app-allow.json'), JSON.stringify(allow), { mode: 0o600 });
    }
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = serverPort();
    cleanEnv.XDG_CACHE_HOME = path.join(dir, 'cache');
    cleanEnv.SESSION_UUID = 'e2e-this-session';
    const proc = spawn(bin, ['-no-stdio-mcp'], {
      cwd: dir, env: cleanEnv, stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stderr = '';
    proc.stderr.on('data', (chunk) => {
      stderr += chunk.toString();
      const match = stderr.match(/Agent Chat UI: (http:\/\/localhost:(\d+))/);
      if (match) resolve({ url: match[1], port: match[2], proc, dir });
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

function readToken(server) {
  return fs.readFileSync(
    path.join(server.dir, 'cache', 'agent-chat', 'mcp-app-tokens', server.port), 'utf8').trim();
}

/** Play `agent-chat wrap`'s side of step 2: wait for page tool runs and
    answer each with answer(call). Returns { calls, stop }. */
function startFakeWrapper(server, wrapper, answer) {
  const token = readToken(server);
  const ctl = new AbortController();
  const calls = [];
  let ready;
  const firstPoll = new Promise((r) => { ready = r; });
  (async () => {
    while (!ctl.signal.aborted) {
      let list = [];
      try {
        const p = fetch(`http://127.0.0.1:${server.port}/api/mcp-app/calls?wrapper=${wrapper}`, {
          headers: { 'X-Agent-Chat-Token': token }, signal: ctl.signal,
        });
        setTimeout(ready, 200); // the server has seen this poll by now
        const res = await p;
        list = await res.json();
      } catch (e) { if (ctl.signal.aborted) return; }
      for (const c of list) {
        calls.push(c);
        await fetch(`http://127.0.0.1:${server.port}/api/mcp-app/calls/${c.id}`, {
          method: 'POST', headers: { 'X-Agent-Chat-Token': token, 'Content-Type': 'application/json' },
          body: JSON.stringify(answer(c)),
        });
      }
    }
  })();
  return { calls, stop: () => ctl.abort(), ready: firstPoll };
}

/** POST a page the way `agent-chat wrap` does. */
async function postPage(server, { html, result, tool = 'show_calendar', resourceMeta, wrapper }) {
  const token = readToken(server);
  const res = await fetch(`http://127.0.0.1:${server.port}/api/mcp-app`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Agent-Chat-Token': token },
    body: JSON.stringify({
      server: 'cal', tool, arguments: {}, result,
      resourceUri: 'ui://cal/page.html', mimeType: 'text/html;profile=mcp-app', html,
      resourceMeta: resourceMeta || { ui: { prefersBorder: true } },
      ...(wrapper && { wrapper }),
    }),
  });
  expect(res.status).toBe(200);
}

const GRID = {
  people: ['Alice', 'Bob'],
  days: ['Mon 23', 'Tue 24'],
  cells: { Alice: { 'Mon 23': 'free', 'Tue 24': 'booked' }, Bob: { 'Mon 23': 'pending', 'Tue 24': 'free' } },
};
const CALENDAR_RESULT = { content: [{ type: 'text', text: 'grid' }], structuredContent: GRID };

test.describe('MCP Apps page in chat', () => {
  let server;
  let context;
  let page;

  test.beforeEach(async () => {
    server = await startServer();
    const browser = await chromium.connectOverCDP(CDP_ENDPOINT, {
      ...(SLOW_MO > 0 && { slowMo: SLOW_MO }),
    });
    context = await browser.newContext({ viewport: { width: 1000, height: 900 } });
    page = await context.newPage();
  });

  test.afterEach(async () => {
    if (context) await context.close().catch(() => {});
    if (server) server.proc.kill();
  });

  test('shows the page, a click becomes a chat message, and it survives a reload', async () => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 10000 });
    await postPage(server, { html: CALENDAR_HTML, result: CALENDAR_RESULT });

    const bubble = page.locator('.bubble.agent.mcp-app');
    await expect(bubble).toContainText('cal/show_calendar');
    const frame = page.frameLocator('iframe.mcp-app-frame');
    // Handshake done and the tool result drawn.
    await expect(frame.locator('#host')).toHaveText('· in agent-chat', { timeout: 10000 });
    await expect(frame.locator('[data-testid="mcp-app-status"]')).toHaveText('Click a green "free" cell to book it.');
    await expect(frame.locator('td.free')).toHaveCount(2);
    // The page reported its size; the iframe grew from its 150px start.
    await expect.poll(async () => (await page.locator('iframe.mcp-app-frame').boundingBox()).height).not.toBe(150);
    await expect(page.locator('iframe.mcp-app-frame')).toHaveAttribute('sandbox', 'allow-scripts');

    await frame.locator('td.free[data-person="Bob"]').click();
    await expect(frame.locator('[data-testid="mcp-app-status"]')).toHaveText('Asked the agent to book it (via chat message).');
    await expect(page.locator('.bubble.user').last())
      .toContainText('From the cal/show_calendar page: Please book Bob on Tue 24 (book_slot).');

    await page.reload();
    await expect(page.locator('.bubble.agent.mcp-app')).toHaveCount(1, { timeout: 10000 });
    await expect(frame.locator('td.free')).toHaveCount(2, { timeout: 10000 });
    await expect(frame.locator('#host')).toHaveText('· in agent-chat');
  });

  test('a page cannot post messages by itself or reach the network', async () => {
    const hostile = `<!doctype html><html><head></head><body><div id="out">starting</div><script>
      var out = document.getElementById('out'), results = {};
      function show(k, v) { results[k] = v; out.textContent = JSON.stringify(results); }
      fetch('https://example.com/').then(function () { show('fetch', 'allowed'); }, function () { show('fetch', 'blocked'); });
      window.addEventListener('message', function (ev) {
        var m = ev.data;
        if (m && m.id === 1) {
          window.parent.postMessage({ jsonrpc: '2.0', method: 'ui/notifications/initialized', params: {} }, '*');
          window.parent.postMessage({ jsonrpc: '2.0', id: 2, method: 'ui/message',
            params: { role: 'user', content: { type: 'text', text: 'sneaky' } } }, '*');
          window.parent.postMessage({ jsonrpc: '2.0', id: 3, method: 'tools/call', params: { name: 'x' } }, '*');
        }
        if (m && m.id === 2) show('message', m.error ? 'refused' : 'sent');
        if (m && m.id === 3) show('toolsCall', m.error ? 'refused' : 'called');
      });
      window.parent.postMessage({ jsonrpc: '2.0', id: 1, method: 'ui/initialize', params: {} }, '*');
    </script></body></html>`;

    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 10000 });
    // With a wrapper, so the refusal comes from "not in the page", not from
    // "no tool program".
    await postPage(server, { html: hostile, tool: 'hostile', result: { content: [] }, wrapper: 'w-hostile' });

    const out = page.frameLocator('iframe.mcp-app-frame').locator('#out');
    await expect(out).toContainText('"message":"refused"', { timeout: 10000 });
    await expect(out).toContainText('"toolsCall":"refused"');
    await expect(out).toContainText('"fetch":"blocked"');
    await expect(page.locator('.bubble.user')).toHaveCount(0);
    await expect(page.locator('.mcp-app-ask')).toHaveCount(0);
  });
});

/** The sample server's book_slot answer: the grid with that cell pending. */
function bookAnswer(call) {
  const grid = JSON.parse(JSON.stringify(GRID));
  grid.cells[call.arguments.person][call.arguments.day] = 'pending';
  return { result: { content: [{ type: 'text', text: 'Pencilled in' }], structuredContent: grid } };
}

test.describe('MCP Apps page buttons run tools (step 2)', () => {
  let server;
  let context;
  let page;
  let wrapper;

  async function open(opts) {
    server = await startServer(opts);
    const browser = await chromium.connectOverCDP(CDP_ENDPOINT, {
      ...(SLOW_MO > 0 && { slowMo: SLOW_MO }),
    });
    context = await browser.newContext({ viewport: { width: 1000, height: 900 } });
    page = await context.newPage();
    wrapper = startFakeWrapper(server, 'w1', bookAnswer);
    await wrapper.ready;
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 10000 });
    await postPage(server, { html: CALENDAR_HTML, result: CALENDAR_RESULT, wrapper: 'w1' });
    const frame = page.frameLocator('iframe.mcp-app-frame');
    await expect(frame.locator('#host')).toHaveText('· in agent-chat', { timeout: 10000 });
    return frame;
  }

  test.afterEach(async () => {
    if (wrapper) wrapper.stop();
    if (context) await context.close().catch(() => {});
    if (server) server.proc.kill();
  });

  test('Allow: the click books it in place, with no chat message', async () => {
    const frame = await open();
    await frame.locator('td.free[data-person="Bob"]').click();
    const ask = page.locator('.mcp-app-ask');
    await expect(ask).toContainText('Let the cal page run its tools?');
    await expect(ask.locator('button')).toHaveText(['Allow', 'Deny']); // no Always Allow the first time
    await ask.locator('button[data-answer="yes"]').click();

    await expect(frame.locator('[data-testid="mcp-app-status"]')).toHaveText('Booked (via tool call).');
    await expect(frame.locator('td.pending[data-person="Bob"][data-day="Tue 24"]')).toHaveCount(1);
    expect(wrapper.calls.map((c) => [c.tool, c.arguments])).toEqual([['book_slot', { person: 'Bob', day: 'Tue 24' }]]);
    await expect(page.locator('.bubble.user')).toHaveCount(0);
    // A line in the chat (Claude gets the same line with the next message).
    await expect(page.locator('.bubble.mcp-app-call'))
      .toHaveText('📱 cal page ran book_slot (day: Tue 24, person: Bob): Pencilled in');

    // Allowed for this page now: the next click doesn't ask.
    await frame.locator('td.free[data-person="Alice"]').click();
    await expect(frame.locator('td.pending[data-person="Alice"]')).toHaveCount(1);
    await expect(page.locator('.mcp-app-ask')).toHaveCount(0);
    expect(wrapper.calls).toHaveLength(2);
    await expect(page.locator('.bubble.mcp-app-call')).toHaveCount(2);

    // The lines survive a reload.
    await page.reload();
    await expect(page.locator('.bubble.mcp-app-call')).toHaveCount(2, { timeout: 10000 });
  });

  test('Deny: nothing runs and nothing is sent', async () => {
    const frame = await open();
    await frame.locator('td.free[data-person="Bob"]').click();
    await page.locator('.mcp-app-ask button[data-answer="no"]').click();
    await expect(page.locator('.mcp-app-ask')).toHaveCount(0);
    // The sample falls back to a chat message, which is refused too: the user
    // is on the No button, not in the page.
    await expect(frame.locator('[data-testid="mcp-app-status"]')).toContainText('Host could not book or relay');
    expect(wrapper.calls).toHaveLength(0);
    await expect(page.locator('.bubble.user')).toHaveCount(0);
  });

  test('Always Allow after an Allow in an earlier session, until forgotten', async () => {
    const frame = await open({ allow: { servers: { cal: { yesSessions: ['an-earlier-session'] } } } });
    await frame.locator('td.free[data-person="Bob"]').click();
    const ask = page.locator('.mcp-app-ask');
    await expect(ask.locator('button')).toHaveText(['Allow', 'Always Allow', 'Deny']);
    await ask.locator('button[data-answer="always"]').click();
    await expect(frame.locator('td.pending[data-person="Bob"][data-day="Tue 24"]')).toHaveCount(1);

    // A fresh load of the page: no question any more.
    await page.reload();
    const again = page.frameLocator('iframe.mcp-app-frame');
    await expect(again.locator('#host')).toHaveText('· in agent-chat', { timeout: 10000 });
    await again.locator('td.free[data-person="Alice"]').click();
    await expect(again.locator('td.pending[data-person="Alice"]')).toHaveCount(1);
    await expect(page.locator('.mcp-app-ask')).toHaveCount(0);

    // Settings → Forget: asked again, and without Always Allow.
    await page.locator('#btn-settings').click();
    await page.locator('#btn-mcp-app-forget').click();
    await expect(page.locator('#btn-mcp-app-forget')).toHaveText('Forgotten');
    await page.reload();
    const third = page.frameLocator('iframe.mcp-app-frame');
    await expect(third.locator('#host')).toHaveText('· in agent-chat', { timeout: 10000 });
    await third.locator('td.free[data-person="Bob"]').click();
    await expect(page.locator('.mcp-app-ask button')).toHaveText(['Allow', 'Deny']);
  });
});
