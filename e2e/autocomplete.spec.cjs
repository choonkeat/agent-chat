// @ts-check
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

// CDP endpoint for the remote Chrome browser
const CDP_ENDPOINT = process.env.CDP_ENDPOINT || 'http://chrome:9223';

/**
 * Start agent-chat in a temp directory with known fixture files.
 * Returns { url, proc, dir } — caller must kill proc when done.
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

// Custom test that connects to remote Chrome via CDP
const test = base.extend({
  // Override the default page fixture to use CDP
  page: async ({}, use) => {
    const browser = await chromium.connectOverCDP(CDP_ENDPOINT);
    const context = await browser.newContext();
    const page = await context.newPage();
    await use(page);
    await context.close();
  },
});

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

  test('typing @doc shows matching file suggestions', async ({ page }) => {
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

    // Navigate to agent-chat
    await page.goto(server.url);

    // Wait for WebSocket connection (textarea becomes enabled)
    const textarea = page.locator('#chat-input');
    await expect(textarea).toBeEnabled({ timeout: 5000 });

    // Type "read @doc" with realistic delays between keystrokes
    await textarea.click();
    await textarea.pressSequentially('read @doc', { delay: 50 });

    // Wait for autocomplete dropdown to appear with actual results
    const dropdown = page.locator('#autocomplete-dropdown');
    await expect(dropdown).toHaveClass(/visible/, { timeout: 3000 });

    // Wait a moment for the fetch response to arrive
    await page.waitForTimeout(1000);

    // Assert: dropdown should have selectable options (not just a status message)
    const optionEls = dropdown.locator('.ac-option');
    const optionCount = await optionEls.count();
    expect(optionCount).toBeGreaterThan(0);

    // The dropdown should contain a file path matching "doc"
    const options = await optionEls.allTextContents();
    expect(options.some(opt => opt.includes('docs'))).toBe(true);

    // Assert request: at least one request should have query containing "doc"
    const hasDocQuery = autocompleteRequests.some(
      r => r && r.query && r.query.length > 0 && 'doc'.startsWith(r.query)
    );
    expect(hasDocQuery).toBe(true);

    // Assert response: at least one response should have results matching "docs"
    const hasDocResult = autocompleteResponses.some(
      r => r && r.results && r.results.some(f => f.includes('docs'))
    );
    expect(hasDocResult).toBe(true);
  });
});
