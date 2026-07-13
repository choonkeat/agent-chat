// @ts-check
// Pins the placement of frozen quick-reply chips in the message log.
//
// Symptom that motivated this test: in a permission flow where a user message
// is still pending in the queue when the agent emits a message with quick
// replies, clicking one of the chips parked the *unused* chip(s) below the
// pending user bubble — visually divorcing them from the agent bubble that
// produced them. The fix anchors `appendFrozenReplies` to the most recent
// non-loading agent bubble, so the cluster `[agent message, unused chips,
// chosen reply]` always holds.
//
// To drive only the client-side function under test we synthesize the DOM
// state directly with `page.evaluate`, bypassing the WS round-trip.
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
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-frozen-'));
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

// Returns an ordered list of identifiers describing every direct child of
// #messages (skipping the always-last #quick-replies host element). Each entry
// is one of:
//   { kind: 'bubble', role: 'agent'|'user', text, pending: bool }
//   { kind: 'frozen', chips: [string,...] }
//   { kind: 'loader' }
//   { kind: 'other', className }
async function readMessagesOrder(page) {
  return page.evaluate(() => {
    const messages = document.getElementById('messages');
    const qr = document.getElementById('quick-replies');
    const out = [];
    for (let i = 0; i < messages.children.length; i++) {
      const el = messages.children[i];
      if (el === qr) continue;
      if (el.id === 'loading-bubble') { out.push({ kind: 'loader' }); continue; }
      if (el.classList.contains('frozen-replies')) {
        const chips = Array.from(el.querySelectorAll('.chip')).map((c) => c.textContent);
        out.push({ kind: 'frozen', chips });
        continue;
      }
      if (el.classList.contains('bubble')) {
        const role = el.classList.contains('agent') ? 'agent' : el.classList.contains('user') ? 'user' : 'unknown';
        const pending = el.classList.contains('pending-agent');
        // Strip any TTS button / unsend button text from the snapshot.
        const clone = el.cloneNode(true);
        clone.querySelectorAll('.tts-btn, .bubble-pending-menu').forEach((n) => n.remove());
        out.push({ kind: 'bubble', role, text: clone.textContent.trim(), pending });
        continue;
      }
      out.push({ kind: 'other', className: el.className });
    }
    return out;
  });
}

test.describe('Frozen quick-reply placement', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;
  test.beforeAll(async () => { server = await startServer(); });
  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('unused chips land immediately after the originating agent bubble, even when a pending user bubble exists', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // Build the exact DOM shape that the permission-while-pending scenario
    // produces:
    //   [agent question] [pending user bubble] [#quick-replies with Allow/Deny]
    // The agent bubble is the chips' originator; the user bubble was already
    // pending when the chips arrived. Reproduces the same state the WS path
    // produces without coupling the test to channel/permission internals.
    await page.evaluate(() => {
      window.addBubble('agent question', 'agent', null, null, Date.now());
      window.addBubble('pending msg', 'user', null, null, Date.now(), 'pending-id-123');
      window.setQuickReplies(['Allow', 'Deny']);
      window.quickReplies.classList.add('visible');
    });

    // Sanity check: the chips are visible before we freeze.
    await expect(page.locator('#quick-replies .chip')).toHaveCount(2);

    // Simulate the user clicking "Allow": the chosen text becomes a user
    // bubble (the WS broadcast does that asynchronously in production), but
    // the placement we care about is the FROZEN remainder.
    await page.evaluate(() => window.freezeCurrentReplies('Allow'));

    const order = await readMessagesOrder(page);

    // Locate the agent bubble and the frozen-replies block; the frozen block
    // must sit immediately after the agent bubble — not after the pending
    // user bubble (which is the regression we are pinning).
    const agentIdx = order.findIndex((e) => e.kind === 'bubble' && e.role === 'agent' && e.text === 'agent question');
    const frozenIdx = order.findIndex((e) => e.kind === 'frozen');
    const pendingIdx = order.findIndex((e) => e.kind === 'bubble' && e.role === 'user' && e.pending);

    expect(agentIdx, 'agent bubble must be present').toBeGreaterThanOrEqual(0);
    expect(frozenIdx, 'frozen-replies block must be present').toBeGreaterThanOrEqual(0);
    expect(pendingIdx, 'pending user bubble must be present').toBeGreaterThanOrEqual(0);

    expect(frozenIdx, 'frozen block must directly follow the originating agent bubble').toBe(agentIdx + 1);
    expect(pendingIdx, 'pending user bubble must follow the frozen block, not precede it').toBeGreaterThan(frozenIdx);

    // The frozen block contains only the unselected chip.
    const frozen = order[frozenIdx];
    expect(frozen.chips).toEqual(['Deny']);
  });

  test('with no pre-existing pending bubble, frozen chips still land after the agent bubble', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    await page.evaluate(() => {
      window.addBubble('agent question', 'agent', null, null, Date.now());
      window.setQuickReplies(['Yes', 'No', 'Maybe']);
      window.quickReplies.classList.add('visible');
    });

    await expect(page.locator('#quick-replies .chip')).toHaveCount(3);

    await page.evaluate(() => window.freezeCurrentReplies('Yes'));

    const order = await readMessagesOrder(page);
    const agentIdx = order.findIndex((e) => e.kind === 'bubble' && e.role === 'agent' && e.text === 'agent question');
    const frozenIdx = order.findIndex((e) => e.kind === 'frozen');

    expect(agentIdx).toBeGreaterThanOrEqual(0);
    expect(frozenIdx).toBe(agentIdx + 1);
    expect(order[frozenIdx].chips).toEqual(['No', 'Maybe']);
  });

  test('no agent bubble present → frozen chips fall back to appendMessage placement', async ({ page }) => {
    await page.goto(server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

    // Edge case: chips are active but no agent bubble exists in #messages.
    // (Possible on a fresh chat where the very first agent event is a draw
    // canvas or similar non-bubble.) The fallback path must still append.
    await page.evaluate(() => {
      window.setQuickReplies(['A', 'B']);
      window.quickReplies.classList.add('visible');
    });

    await page.evaluate(() => window.freezeCurrentReplies('A'));

    const order = await readMessagesOrder(page);
    const frozenIdx = order.findIndex((e) => e.kind === 'frozen');
    expect(frozenIdx, 'frozen-replies must still be inserted even with no agent bubble').toBeGreaterThanOrEqual(0);
    expect(order[frozenIdx].chips).toEqual(['B']);
  });
});
