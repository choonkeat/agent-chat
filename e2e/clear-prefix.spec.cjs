// @ts-check
// Pins the `/clear <instruction>` sequence.
//
// Motivation: wiping the agent's memory and handing it a new instruction are
// two halves of one action, and the only thing that survives between them is
// the chat log. The order is load-bearing — wipe, then record, then point the
// agent at the file — and each half is a separate message to the parent frame,
// so a regression here is silent: the chat simply stops answering.
//
// The agent-chat page is loaded inside a parent page that records every
// postMessage it receives, which is exactly what swe-swe's terminal pane does
// with them.
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

// Starts a fresh agent-chat server with the streaming chat-log export on —
// without it there is no file for the resume line to name.
function startServer(extraArgs = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-clear-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';
    cleanEnv.AGENT_CHAT_EXPORT_DIR = 'agent-chats';

    const proc = spawn(bin, ['-no-stdio-mcp', ...extraArgs], {
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

// Loads agent-chat in an iframe under a parent that records postMessages.
async function embed(page, url) {
  await gotoRetry(page, url); // proves the server answers before we frame it
  await page.setContent(
    `<iframe id="chat" src="${url}" style="width:100%;height:600px;border:0"></iframe>`
  );
  await page.evaluate(() => {
    window.__msgs = [];
    window.addEventListener('message', (e) => {
      if (e.data && e.data.type) window.__msgs.push(e.data);
    });
  });
  const frame = page.frameLocator('#chat');
  await expect(frame.locator('#chat-input')).toBeEnabled({ timeout: 10000 });
  return frame;
}

const interrupts = (page) =>
  page.evaluate(() => window.__msgs.filter((m) => m.type === 'agent-chat-interrupt').map((m) => m.text));

test.describe('/clear prefix', () => {
  test('wipes first, records the stripped instruction, then names the log file', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#chat-input').fill('/clear now fix the logout bug');
      await frame.locator('#chat-input').press('Enter');

      // 1. The wipe goes out immediately — before anything is recorded, so a
      //    still-running agent cannot eat the instruction and then be erased.
      await expect.poll(() => interrupts(page), { timeout: 5000 }).toEqual(['/clear']);

      // 2. The boundary marker and the stripped instruction land in the chat.
      //    The `/clear ` prefix itself is not part of the recorded message.
      await expect(frame.locator('.bubble.agent', { hasText: 'context cleared' })).toBeVisible({ timeout: 10000 });
      const userBubble = frame.locator('.bubble.user', { hasText: 'now fix the logout bug' });
      await expect(userBubble).toBeVisible();
      await expect(userBubble).not.toContainText('/clear');

      // 3. Only then is the resume line typed, naming the file by its current
      //    path — and pointing the agent at the file, not at check_messages.
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      const [, resume] = await interrupts(page);
      expect(resume).toMatch(/^resume agent-chats\/[\d-]+-untitled.*\.md /);
      expect(resume).toContain('last USER entry');
      expect(resume).toContain('send_message');
      // An `@` would open the agent CLI's file picker and the trailing Enter
      // would pick an entry instead of submitting the line.
      expect(resume).not.toContain('@');

      // The file the resume line names must exist and hold both halves.
      const named = resume.slice('resume '.length).split(' ')[0];
      const md = fs.readFileSync(path.join(server.dir, named), 'utf8');
      expect(md).toContain('context cleared');
      expect(md).toContain('now fix the logout bug');
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('bare /clear wipes and resumes with no instruction', async ({ page }) => {
    const server = await startServer();
    try {
      const frame = await embed(page, server.url);

      await frame.locator('#chat-input').fill('/clear');
      await frame.locator('#chat-input').press('Enter');

      // No instruction to record, but the resume line must still be typed —
      // otherwise the wiped agent sits there and the chat looks dead.
      await expect(frame.locator('.bubble.agent', { hasText: 'context cleared' })).toBeVisible({ timeout: 10000 });
      await expect(frame.locator('.bubble.user')).toHaveCount(0);
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      const [wipe, resume] = await interrupts(page);
      expect(wipe).toBe('/clear');
      expect(resume).toMatch(/^resume agent-chats\//);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  // With `/` registered as an autocomplete trigger — which is how agent-chat
  // runs inside swe-swe — a bare `/clear` still has its dropdown open when
  // Enter is pressed, because the trigger only dies on the first space. The
  // dropdown must not eat that Enter: a status-only dropdown ("No results")
  // has nothing to select, so the message would simply never send.
  test('bare /clear submits even with the autocomplete dropdown open', async ({ page }) => {
    const server = await startServer(['-autocomplete-triggers', '/=builtin:filepath']);
    try {
      const frame = await embed(page, server.url);

      // Typed character by character so the trigger fires exactly as it would
      // for a real user; fill() would set the value in one shot.
      await frame.locator('#chat-input').pressSequentially('/clear', { delay: 30 });
      await expect(frame.locator('#autocomplete-dropdown')).toHaveClass(/visible/, { timeout: 5000 });

      await frame.locator('#chat-input').press('Enter');
      await expect.poll(() => interrupts(page), { timeout: 10000 }).toHaveLength(2);
      expect(await frame.locator('#chat-input').inputValue()).toBe('');
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });
});
