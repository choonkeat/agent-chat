// Message-style setting must survive moving to a new session.
//
// Every session runs its own server on its own port, so anything the client
// keeps in localStorage (scoped per origin, port included) would silently
// reset. The setting lives in a cookie instead — cookies ignore the port — so
// the same browser sees it on a second server on a different port.

const { test, expect } = require('@playwright/test');
const { chromium } = require('playwright');
const { spawn } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = Number(process.env.SLOW_MO || 0);

/** Start agent-chat in a temp dir on a random port. Caller kills proc. */
function startServer() {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-e2e-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';
    const proc = spawn(bin, ['-no-stdio-mcp'], {
      cwd: dir, env: cleanEnv, stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stderr = '';
    proc.stderr.on('data', (chunk) => {
      stderr += chunk.toString();
      const match = stderr.match(/Agent Chat UI: http:\/\/localhost:(\d+)/);
      // The server binds IPv4 only; `localhost` can resolve to ::1 in the
      // shared browser and intermittently refuse the connection.
      if (match) resolve({ url: `http://127.0.0.1:${match[1]}`, proc, dir });
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

/** Poll from node until the server answers, so a goto failure means Chrome. */
async function waitForServer(url) {
  for (let i = 0; i < 50; i++) {
    try {
      const res = await fetch(url);
      if (res.ok) return;
    } catch (e) { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`server never answered: ${url}`);
}

/** Untick everything, the way a user would when they want no style at all. */
async function clearStyle(page) {
  const on = page.locator('#settings-presets .preset-btn.selected');
  for (let n = await on.count(); n > 0; n = await on.count()) await on.first().click();
  await expect(page.locator('#msg-style-input')).toHaveValue('');
}

async function openSettings(page, url) {
  await waitForServer(url);
  // The shared browser occasionally refuses a freshly-bound ephemeral port;
  // node has already confirmed the server answers, so retry until it lands.
  for (let i = 0; ; i++) {
    try {
      await page.goto(url);
      break;
    } catch (e) {
      if (i >= 9) throw e;
      await page.waitForTimeout(500);
    }
  }
  await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
  await page.locator('#btn-settings').click();
  await expect(page.locator('#settings-panel')).toBeVisible();
}

test.describe('message style persistence', () => {
  let a; let b; let context;

  test.beforeEach(async () => {
    [a, b] = await Promise.all([startServer(), startServer()]);
    const browser = await chromium.connectOverCDP(CDP_ENDPOINT, {
      ...(SLOW_MO > 0 && { slowMo: SLOW_MO }),
    });
    context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  });

  test.afterEach(async () => {
    if (context) await context.close();
    for (const s of [a, b]) {
      if (s) { s.proc.kill(); fs.rmSync(s.dir, { recursive: true, force: true }); }
    }
  });

  test('a browser that never touched the panel starts on Non-technical + ADHD', async () => {
    const page = await context.newPage();
    await openSettings(page, a.url);

    await expect(page.locator('.preset-btn[data-preset="nontechnical"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.preset-btn[data-preset="adhd"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.preset-btn[data-preset="concise"]')).toHaveAttribute('aria-pressed', 'false');
    const text = await page.locator('#msg-style-input').inputValue();
    expect(text).toContain('non-technical language');
    expect(text).toContain('ADHD');

    // Unticking every pill is a real answer, and it outlasts the defaults on
    // the next session — the defaults are only for a browser that never chose.
    await clearStyle(page);
    const next = await context.newPage();
    await openSettings(next, b.url);
    await expect(next.locator('#msg-style-input')).toHaveValue('');
    await expect(next.locator('.preset-btn[data-preset="adhd"]')).toHaveAttribute('aria-pressed', 'false');
  });

  test('a preset chosen on one session applies to the next session (different port)', async () => {
    expect(new URL(a.url).port).not.toBe(new URL(b.url).port);

    const first = await context.newPage();
    await openSettings(first, a.url);
    await clearStyle(first);
    await first.locator('.preset-btn[data-preset="adhd"]').click();
    const chosen = await first.locator('#msg-style-input').inputValue();
    expect(chosen).toContain('ADHD');
    expect(chosen).not.toContain('{{message}}');
    await expect(first.locator('.preset-btn[data-preset="adhd"]')).toHaveAttribute('aria-pressed', 'true');

    // A second session: same browser, different port.
    const second = await context.newPage();
    await openSettings(second, b.url);
    await expect(second.locator('#msg-style-input')).toHaveValue(chosen);
    await expect(second.locator('.preset-btn[data-preset="adhd"]')).toHaveAttribute('aria-pressed', 'true');
  });

  test('pills are checkboxes: several combine, and each unticks on its own', async () => {
    const page = await context.newPage();
    await openSettings(page, a.url);
    await clearStyle(page);

    await page.locator('.preset-btn[data-preset="concise"]').click();
    await page.locator('.preset-btn[data-preset="adhd"]').click();
    const both = await page.locator('#msg-style-input').inputValue();
    expect(both).toContain('as concisely as possible');
    expect(both).toContain('ADHD');
    await expect(page.locator('.preset-btn[data-preset="concise"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.preset-btn[data-preset="adhd"]')).toHaveAttribute('aria-pressed', 'true');

    // Unticking one leaves the other's words untouched.
    await page.locator('.preset-btn[data-preset="concise"]').click();
    const left = await page.locator('#msg-style-input').inputValue();
    expect(left).not.toContain('as concisely as possible');
    expect(left).toContain('ADHD');
    await expect(page.locator('.preset-btn[data-preset="concise"]')).toHaveAttribute('aria-pressed', 'false');
    await expect(page.locator('.preset-btn[data-preset="adhd"]')).toHaveAttribute('aria-pressed', 'true');

    // Unticking the last pill empties the box — that is "no style".
    await page.locator('.preset-btn[data-preset="adhd"]').click();
    await expect(page.locator('#msg-style-input')).toHaveValue('');
    await expect(page.locator('.preset-btn[data-preset="adhd"]')).toHaveAttribute('aria-pressed', 'false');
  });

  test('words matching no pill are collated below the ticked styles', async () => {
    const page = await context.newPage();
    await openSettings(page, a.url);

    // Typed first, so only re-composing can move it to the bottom.
    await page.locator('#msg-style-input').fill('Always cite the file and line.');
    await expect(page.locator('.preset-btn[data-preset="adhd"]')).toHaveAttribute('aria-pressed', 'false');
    await expect(page.locator('#preset-unsaved')).toBeVisible();
    await page.locator('.preset-btn[data-preset="direct"]').click();

    const text = await page.locator('#msg-style-input').inputValue();
    expect(text.indexOf('Be direct.')).toBeLessThan(text.indexOf('Always cite'));
    expect(text.trimEnd().endsWith('Always cite the file and line.')).toBe(true);
    await expect(page.locator('#preset-unsaved')).toHaveAttribute('aria-pressed', 'true');

    // Tapping Custom takes those words out, and tapping it again puts them back.
    await page.locator('#preset-unsaved').click();
    await expect(page.locator('#msg-style-input')).not.toHaveValue(/Always cite/);
    await expect(page.locator('#preset-unsaved')).toBeVisible();
    await expect(page.locator('#preset-unsaved')).toHaveAttribute('aria-pressed', 'false');
    await page.locator('#preset-unsaved').click();
    await expect(page.locator('#msg-style-input')).toHaveValue(text);
    await expect(page.locator('#preset-unsaved')).toHaveAttribute('aria-pressed', 'true');
  });

  test('clearing the style on one session clears it on the next', async () => {
    const first = await context.newPage();
    await openSettings(first, a.url);
    await clearStyle(first);
    await first.locator('.preset-btn[data-preset="concise"]').click();
    await expect(first.locator('.preset-btn[data-preset="concise"]')).toHaveAttribute('aria-pressed', 'true');

    const second = await context.newPage();
    await openSettings(second, b.url);
    await expect(second.locator('.preset-btn[data-preset="concise"]')).toHaveAttribute('aria-pressed', 'true');
    await second.locator('#msg-style-input').fill('');
    await expect(second.locator('.preset-btn[data-preset="concise"]')).toHaveAttribute('aria-pressed', 'false');

    const third = await context.newPage();
    await openSettings(third, a.url);
    await expect(third.locator('#msg-style-input')).toHaveValue('');
    await expect(third.locator('.preset-btn[data-preset="concise"]')).toHaveAttribute('aria-pressed', 'false');
  });

  test('an edited style saved under a name becomes a pill on the next session', async () => {
    const mine = 'Answer in exactly one sentence.';

    const first = await context.newPage();
    await openSettings(first, a.url);
    await first.locator('#msg-style-input').fill(mine);
    await first.locator('#btn-style-save').click();
    await first.locator('#style-save-name').fill('One-liner');
    await first.locator('#btn-style-save-confirm').click();
    await expect(first.locator('.preset-btn[data-custom="One-liner"]')).toBeVisible();
    await expect(first.locator('.preset-btn[data-custom="One-liner"]')).toHaveAttribute('aria-pressed', 'true');

    // Next session: the pill is there, ticking a built-in preset adds to it
    // rather than replacing it, and unticking takes only its own words away.
    const second = await context.newPage();
    await openSettings(second, b.url);
    await expect(second.locator('.preset-btn[data-custom="One-liner"]')).toBeVisible();
    await second.locator('.preset-btn[data-preset="direct"]').click();
    await expect(second.locator('#msg-style-input')).toHaveValue(new RegExp('Be direct[\\s\\S]*' + mine));
    await expect(second.locator('.preset-btn[data-custom="One-liner"]')).toHaveAttribute('aria-pressed', 'true');
    await second.locator('.preset-btn[data-custom="One-liner"]').click();
    await expect(second.locator('#msg-style-input')).not.toHaveValue(new RegExp(mine));
    await expect(second.locator('.preset-btn[data-custom="One-liner"]')).toHaveAttribute('aria-pressed', 'false');
    await expect(second.locator('.preset-btn[data-preset="direct"]')).toHaveAttribute('aria-pressed', 'true');
  });

  test('text belonging to no pill shows as Custom until it is named', async () => {
    const page = await context.newPage();
    await openSettings(page, a.url);

    await clearStyle(page);
    await page.locator('.preset-btn[data-preset="adhd"]').click();
    await expect(page.locator('#preset-unsaved')).toBeHidden();
    await expect(page.locator('.preset-btn[data-preset="adhd"]')).toHaveClass(/selected/);
    await expect(page.locator('#btn-style-save')).toBeDisabled();

    await page.locator('#msg-style-input').fill('Answer in haiku.');
    await expect(page.locator('#preset-unsaved')).toBeVisible();
    await expect(page.locator('#preset-unsaved')).toHaveClass(/selected/);
    await expect(page.locator('#btn-style-save')).toBeEnabled();

    // Naming it replaces Custom with a pill of that name.
    await page.locator('#btn-style-save').click();
    await expect(page.locator('.settings-actions')).toBeHidden();
    await page.locator('#style-save-name').fill('Haiku');
    await page.locator('#btn-style-save-confirm').click();
    await expect(page.locator('#preset-unsaved')).toBeHidden();
    await expect(page.locator('.preset-btn[data-custom="Haiku"]')).toHaveClass(/selected/);
    await expect(page.locator('#btn-style-save')).toBeDisabled();
  });

  test('deleting a saved style removes it everywhere', async () => {
    const first = await context.newPage();
    await openSettings(first, a.url);
    await first.locator('#msg-style-input').fill('Be terse.');
    await first.locator('#btn-style-save').click();
    await first.locator('#style-save-name').fill('Terse');
    await first.locator('#btn-style-save-confirm').click();
    await expect(first.locator('.preset-btn[data-custom="Terse"]')).toBeVisible();

    const second = await context.newPage();
    await openSettings(second, b.url);
    await second.locator('.preset-del[data-del="Terse"]').click();
    await expect(second.locator('.preset-btn[data-custom="Terse"]')).toHaveCount(0);

    const third = await context.newPage();
    await openSettings(third, a.url);
    await expect(third.locator('.preset-btn[data-custom="Terse"]')).toHaveCount(0);
  });
});
