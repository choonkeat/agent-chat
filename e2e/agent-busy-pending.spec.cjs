// @ts-check
// e2e coverage for:
//   1. #btn-send turns yolo orange whenever the agent typing/loading
//      indicator (#loading-bubble) is in the DOM
//   2. A user speech bubble is rendered "pending" (dim + tooltip + below
//      the loader) until the server emits userMessagesConsumed, at which
//      point it moves above the loader and loses the pending styling
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');

const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);

// Generous waits to avoid timing flakiness (per user request).
const SETTLE_MS = 800;

function startServer(extraFlags = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-busy-'));
    fs.writeFileSync(path.join(dir, 'README.md'), '# README\n');

    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
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

async function setupPage(page, url) {
  await page.goto(url);
  const textarea = page.locator('#chat-input');
  await expect(textarea).toBeEnabled({ timeout: 5000 });
  await textarea.click();
  // Give the WebSocket a moment to fully establish so the first send doesn't
  // race the connection.
  await page.waitForTimeout(SETTLE_MS);
  return textarea;
}

// Call an MCP tool over the Streamable HTTP transport. The agent-facing
// server is mounted at /mcp; the orchestrator server at /mcp/orchestrator.
// Stateless mode lets us POST a single JSON-RPC request without a session.
async function mcpCall(baseUrl, mountPath, toolName, args) {
  const body = {
    jsonrpc: '2.0',
    method: 'tools/call',
    params: { name: toolName, arguments: args || {} },
    id: Date.now(),
  };
  const res = await fetch(`${baseUrl}${mountPath}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Accept': 'application/json, text/event-stream',
    },
    body: JSON.stringify(body),
  });
  return res.text();
}

// Parse an rgb()/rgba() string into [r,g,b] integers.
function rgbToTriplet(rgb) {
  const m = rgb.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/);
  if (!m) return null;
  return [parseInt(m[1], 10), parseInt(m[2], 10), parseInt(m[3], 10)];
}

// Yolo orange from the screenshot is in the #f97316 family (Tailwind orange-500).
// Allow some wiggle: just require red > 200, green between 80 and 160, blue < 60.
function isYoloOrange(rgb) {
  const t = rgbToTriplet(rgb);
  if (!t) return false;
  const [r, g, b] = t;
  return r > 200 && g >= 80 && g <= 170 && b < 80;
}

test.describe('Agent-busy send button', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('#btn-send turns orange while #loading-bubble is present, reverts when it leaves', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    const sendBtn = page.locator('#btn-send');
    const loader = page.locator('#loading-bubble');

    // Baseline: no loader, button should NOT be orange.
    await expect(loader).toHaveCount(0);
    const baselineBg = await sendBtn.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(isYoloOrange(baselineBg)).toBe(false);

    // Send a message — handleSend calls showLoading(), which inserts
    // #loading-bubble; the server will broadcast userMessage which keeps the
    // loader alive.
    await textarea.fill('busy check');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);

    await expect(loader).toHaveCount(1);
    const busyBg = await sendBtn.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(isYoloOrange(busyBg)).toBe(true);
    await page.screenshot({ path: 'test-results/screenshots/01-send-btn-orange-while-busy.png', fullPage: true });

    // Remove the loader directly to drive the "agent is no longer busy"
    // transition without needing a full MCP round-trip; the class flip is the
    // unit under test here.
    await page.evaluate(() => { window.removeLoading && window.removeLoading(); });
    await page.waitForTimeout(SETTLE_MS);

    await expect(loader).toHaveCount(0);
    const afterBg = await sendBtn.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(isYoloOrange(afterBg)).toBe(false);
    await page.screenshot({ path: 'test-results/screenshots/02-send-btn-blue-when-idle.png', fullPage: true });
  });
});

test.describe('Pending user bubble until consumed', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => { server = await startServer(); });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('user bubble is dim + below loader until userMessagesConsumed; then normal + above', async ({ page }) => {
    const textarea = await setupPage(page, server.url);
    const sendBtn = page.locator('#btn-send');

    await textarea.fill('pending check');
    await sendBtn.click();
    await page.waitForTimeout(SETTLE_MS);

    // The user bubble exists, has .pending-agent + a tooltip, and is rendered
    // AFTER #loading-bubble in DOM order.
    const userBubble = page.locator('.bubble.user').last();
    await expect(userBubble).toHaveClass(/pending-agent/);
    await expect(userBubble).toHaveAttribute('title', /agent/i);

    const beforeOrder = await page.evaluate(() => {
      const loader = document.getElementById('loading-bubble');
      const bubbles = Array.from(document.querySelectorAll('.bubble.user'));
      const bubble = bubbles[bubbles.length - 1];
      if (!loader || !bubble) return 'missing';
      const pos = loader.compareDocumentPosition(bubble);
      // Node.DOCUMENT_POSITION_FOLLOWING === 4 means bubble comes AFTER loader.
      return (pos & 4) ? 'after' : 'before';
    });
    expect(beforeOrder).toBe('after');
    await page.screenshot({ path: 'test-results/screenshots/03-user-bubble-pending-below-loader.png', fullPage: true });

    // Drain the queue by calling the MCP `check_messages` tool — this is what
    // a real agent would do. The server should publish userMessagesConsumed.
    await mcpCall(server.url, '/mcp', 'check_messages');
    await page.waitForTimeout(SETTLE_MS);

    // After consume: class is gone, and bubble is now BEFORE the loader.
    await expect(userBubble).not.toHaveClass(/pending-agent/);
    const afterOrder = await page.evaluate(() => {
      const loader = document.getElementById('loading-bubble');
      const bubbles = Array.from(document.querySelectorAll('.bubble.user'));
      const bubble = bubbles[bubbles.length - 1];
      if (!loader || !bubble) return 'missing';
      const pos = loader.compareDocumentPosition(bubble);
      // Node.DOCUMENT_POSITION_PRECEDING === 2 means bubble comes BEFORE loader.
      return (pos & 2) ? 'before' : 'after';
    });
    expect(afterOrder).toBe('before');
    await page.screenshot({ path: 'test-results/screenshots/04-user-bubble-consumed-above-loader.png', fullPage: true });
  });
});
