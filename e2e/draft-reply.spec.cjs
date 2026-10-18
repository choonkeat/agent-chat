// @ts-check
// "Draft reply": select text inside a bubble, press the button that appears
// under the end of the selection, and the selection lands at the end of the
// text box as a markdown quote ("> " on every line) followed by a blank line,
// with the cursor on that blank line — ready for the answer.
//
// The blank line after each quote is the point: without it the answer typed
// under a quote is pulled into the quote, and two quotes merge into one.
// A partial selection is turned back into markdown (list marks, bold, code);
// a whole-bubble selection quotes the bubble's own markdown source exactly.
const { test: base, expect } = require('@playwright/test');
const { chromium } = require('@playwright/test');
const { gotoRetry } = require('./goto-retry.cjs');
const { serverPort } = require('./server-port.cjs');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');
const SLOW_MO = parseInt(process.env.SLOW_MO || '0', 10);

function startServer() {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-draftreply-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = serverPort();

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

const SHOT_DIR = process.env.DRAFT_REPLY_SHOT_DIR || '';

async function shot(page, testInfo, name) {
  const file = SHOT_DIR ? path.join(SHOT_DIR, name + '.png') : testInfo.outputPath(name + '.png');
  await page.screenshot({ path: file });
  await testInfo.attach(name, { path: file, contentType: 'image/png' });
}

// Select from the first occurrence of `from` to the end of `to`, both inside
// the bubble containing `bubbleText`. Text nodes only, like a drag would.
async function selectInBubble(page, bubbleText, from, to) {
  await page.evaluate(([bubbleText, from, to]) => {
    const bubble = [...document.querySelectorAll('.bubble')].find(b => b.textContent.includes(bubbleText));
    const walker = document.createTreeWalker(bubble, NodeFilter.SHOW_TEXT);
    const nodes = [];
    for (let n = walker.nextNode(); n; n = walker.nextNode()) nodes.push(n);
    const find = (s) => {
      const n = nodes.find(t => t.nodeValue.includes(s));
      if (!n) throw new Error('not found: ' + s);
      return [n, n.nodeValue.indexOf(s)];
    };
    const [sn, so] = find(from);
    const [en, eo] = find(to);
    const r = document.createRange();
    r.setStart(sn, so);
    r.setEnd(en, eo + to.length);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(r);
  }, [bubbleText, from, to]);
}

async function selectWholeBubble(page, bubbleText) {
  await page.evaluate((bubbleText) => {
    const bubble = [...document.querySelectorAll('.bubble')].find(b => b.textContent.includes(bubbleText));
    const r = document.createRange();
    r.selectNodeContents(bubble);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(r);
  }, bubbleText);
}

const PLAN = [
  'Two ways to fix the upload:',
  '',
  '- **Retry** the request with `backoff`',
  '- Show an error and keep the file',
  '- Drop the file silently',
  '',
  'I recommend the first.',
].join('\n');

const QUESTION = 'Should the retry give up after **3** tries?';

test.describe('Draft reply', () => {
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

  test('quotes twice: list marks survive, a blank line follows each quote', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 480, height: 760 });
    await gotoRetry(page, server.url);
    const input = page.locator('#chat-input');
    await expect(input).toBeEnabled({ timeout: 5000 });
    await page.evaluate(([a, b]) => {
      window.addAgentMessage(a, null, null, Date.now());
      window.addAgentMessage(b, null, null, Date.now());
    }, [PLAN, QUESTION]);

    const btn = page.locator('#draft-reply-btn');
    await expect(btn).toBeHidden();

    // 1. Part of the list: from "Retry" into the second item.
    await selectInBubble(page, 'Two ways', 'Retry', 'Show an error');
    await expect(btn).toBeVisible();
    // The button sits under where the selection ends.
    const pos = await page.evaluate(() => {
      const r = window.getSelection().getRangeAt(0).getClientRects();
      const last = r[r.length - 1];
      const b = document.getElementById('draft-reply-btn').getBoundingClientRect();
      return { selBottom: last.bottom, btnTop: b.top, btnLeft: b.left, btnRight: b.right, vw: innerWidth };
    });
    expect(pos.btnTop).toBeGreaterThanOrEqual(pos.selBottom);
    expect(pos.btnTop - pos.selBottom).toBeLessThan(20);
    expect(pos.btnLeft).toBeGreaterThanOrEqual(0);
    expect(pos.btnRight).toBeLessThanOrEqual(pos.vw);
    await shot(page, testInfo, '1-selection-and-button');

    await btn.click();
    await expect(btn).toBeHidden();
    await expect(input).toHaveValue('> - **Retry** the request with `backoff`\n> - Show an error\n\n');
    await expect(input).toBeFocused();
    // Cursor on the blank line after the quote.
    expect(await input.evaluate(el => [el.selectionStart, el.value.length])).toEqual(
      await input.evaluate(el => [el.value.length, el.value.length]));

    await page.keyboard.type('Retry, yes.');

    // 2. A whole bubble: its markdown source, exactly.
    await selectWholeBubble(page, 'Should the retry');
    await expect(btn).toBeVisible();
    await btn.click();
    await page.keyboard.type('Yes, 3.');

    await expect(input).toHaveValue(
      '> - **Retry** the request with `backoff`\n> - Show an error\n\nRetry, yes.\n\n' +
      '> Should the retry give up after **3** tries?\n\nYes, 3.');
    await shot(page, testInfo, '2-draft-with-two-quotes');

    // Sent, the draft renders as two quotes, each followed by its answer.
    await input.press('Control+Enter');
    const sent = page.locator('.bubble.user').last();
    await expect(sent.locator('blockquote')).toHaveCount(2);
    await expect(sent.locator('blockquote').first().locator('li')).toHaveCount(2);
    await expect(sent.locator('blockquote').first().locator('strong')).toHaveText('Retry');
    await shot(page, testInfo, '3-sent-message');
  });

  test('a selection inside one list item keeps its list mark', async ({ page }) => {
    await gotoRetry(page, server.url);
    const input = page.locator('#chat-input');
    await expect(input).toBeEnabled({ timeout: 5000 });
    await page.evaluate((a) => window.addAgentMessage(a, null, null, Date.now()), PLAN);
    await selectInBubble(page, 'Two ways', 'Drop the', 'silently');
    await page.locator('#draft-reply-btn').click();
    await expect(input).toHaveValue('> - Drop the file silently\n\n');
  });

  test('typed text stays, and the quote goes after it', async ({ page }) => {
    await gotoRetry(page, server.url);
    const input = page.locator('#chat-input');
    await expect(input).toBeEnabled({ timeout: 5000 });
    await page.evaluate((a) => window.addAgentMessage(a, null, null, Date.now()), 'line one\nline two\n\nline four');
    await input.fill('Already typed');
    await input.evaluate(el => el.setSelectionRange(0, 0)); // cursor at the start
    await selectWholeBubble(page, 'line one');
    await page.locator('#draft-reply-btn').click();
    await expect(input).toHaveValue('Already typed\n\n> line one\n> line two\n>\n> line four\n\n');
  });

  test('no button for a selection across two bubbles, or outside bubbles', async ({ page }) => {
    await gotoRetry(page, server.url);
    await expect(page.locator('#chat-input')).toBeEnabled({ timeout: 5000 });
    await page.evaluate(() => {
      window.addAgentMessage('first bubble', null, null, Date.now());
      window.addAgentMessage('second bubble', null, null, Date.now());
    });
    await page.evaluate(() => {
      const bubbles = [...document.querySelectorAll('.bubble.agent')];
      const textOf = (word) => {
        const b = bubbles.find(x => x.textContent.includes(word));
        return document.createTreeWalker(b, NodeFilter.SHOW_TEXT).nextNode();
      };
      const r = document.createRange();
      r.setStart(textOf('first bubble'), 0);
      r.setEnd(textOf('second bubble'), 3);
      const sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(r);
    });
    await page.waitForTimeout(300);
    await expect(page.locator('#draft-reply-btn')).toBeHidden();

    // Selecting inside one bubble shows it; clearing the selection hides it.
    await selectInBubble(page, 'first bubble', 'first', 'bubble');
    await expect(page.locator('#draft-reply-btn')).toBeVisible();
    await page.evaluate(() => window.getSelection().removeAllRanges());
    await expect(page.locator('#draft-reply-btn')).toBeHidden();
  });
});
