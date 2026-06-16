// @ts-check
// Pins the blank-chat welcome quick replies.
//
// Motivation: a chat that opens with only a send_progress (or no agent message
// at all) showed neither a loading indicator nor quick replies, so the opening
// state read as frozen — the user couldn't tell whether it was their turn. The
// fix seeds hardcoded "welcome" quick replies on a genuinely empty chat (zero
// events), overridable via the -welcome-replies flag. They are suppressed the
// moment any history exists.
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);

// Starts a fresh agent-chat server with the given extra CLI args. Each call
// gets its own temp cwd so the in-memory event log starts empty.
function startServer(extraArgs = []) {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-welcome-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';

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

test.describe('Blank-chat welcome quick replies', () => {
  test('default welcome replies appear on a genuinely empty chat', async ({ page }) => {
    const server = await startServer();
    try {
      await page.goto(server.url);
      await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

      const chips = page.locator('#quick-replies .chip');
      await expect(chips).toHaveCount(3, { timeout: 5000 });
      await expect(chips.nth(0)).toHaveText('What can you help me with?');
      await expect(chips.nth(1)).toHaveText('Give me an overview of this project');
      await expect(chips.nth(2)).toHaveText("What's changed recently?");
      await expect(page.locator('#quick-replies')).toHaveClass(/visible/);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('-welcome-replies overrides the defaults', async ({ page }) => {
    const server = await startServer(['-welcome-replies', 'Start a task,Ask a question']);
    try {
      await page.goto(server.url);
      await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

      const chips = page.locator('#quick-replies .chip');
      await expect(chips).toHaveCount(2, { timeout: 5000 });
      await expect(chips.nth(0)).toHaveText('Start a task');
      await expect(chips.nth(1)).toHaveText('Ask a question');
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('-welcome-replies="" disables welcome replies entirely', async ({ page }) => {
    const server = await startServer(['-welcome-replies', '']);
    try {
      await page.goto(server.url);
      await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });

      // Give the historyEnd handler a beat to run; no chips should appear.
      await page.waitForTimeout(500);
      await expect(page.locator('#quick-replies .chip')).toHaveCount(0);
    } finally {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });
});
