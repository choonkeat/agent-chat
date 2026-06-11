// Playwright globalSetup: verify the remote Chrome CDP endpoint is reachable
// before any spec runs.
//
// Why this exists: in the swe-swe dev container the CDP endpoint
// (BROWSER_CDP_PORT, e.g. http://localhost:6001) is LAZY — nothing listens on
// it until an MCP Playwright browser session has opened a page. Chrome is owned
// by the swe-swe MCP/screencast layer, not by this test runner, so a plain
// `connectOverCDP` cannot start it. If CDP is cold, every spec fails at the
// connect step with a confusing `ECONNREFUSED`. This setup turns those 25
// cryptic stack traces into one actionable message.
//
// Resolution order matches the specs: CDP_ENDPOINT, then BROWSER_CDP_PORT,
// then the legacy chrome:9223 default.

const http = require('http');

const CDP_ENDPOINT = process.env.CDP_ENDPOINT
  || (process.env.BROWSER_CDP_PORT ? `http://localhost:${process.env.BROWSER_CDP_PORT}` : 'http://chrome:9223');

function probe(url) {
  return new Promise((resolve) => {
    const req = http.get(url + '/json/version', { timeout: 2000 }, (res) => {
      let body = '';
      res.on('data', (c) => { body += c; });
      res.on('end', () => {
        try { resolve(JSON.parse(body).Browser || true); }
        catch { resolve(res.statusCode === 200); }
      });
    });
    req.on('error', () => resolve(false));
    req.on('timeout', () => { req.destroy(); resolve(false); });
  });
}

module.exports = async () => {
  // A few quick retries to absorb a Chrome that is still booting after a warm.
  const ATTEMPTS = 5;
  for (let i = 0; i < ATTEMPTS; i++) {
    const ok = await probe(CDP_ENDPOINT);
    if (ok) {
      console.log(`[e2e] CDP reachable at ${CDP_ENDPOINT}` + (typeof ok === 'string' ? ` (${ok})` : ''));
      return;
    }
    if (i < ATTEMPTS - 1) await new Promise((r) => setTimeout(r, 1000));
  }

  throw new Error(
    `\n\n[e2e] Remote Chrome CDP is not reachable at ${CDP_ENDPOINT}.\n` +
    `\nIn the swe-swe container this endpoint is LAZY: nothing listens on it\n` +
    `until an MCP Playwright session opens a page. Warm it once, then re-run:\n` +
    `\n  1. Use the MCP browser tool to navigate anywhere, e.g.\n` +
    `       mcp__swe-swe-playwright__browser_navigate { url: "https://example.com" }\n` +
    `  2. Then run:  make e2e-test\n` +
    `\nOverride the endpoint with CDP_ENDPOINT=... if Chrome is elsewhere.\n`
  );
};
