// @ts-check
// Retrying page.goto for specs that navigate to a just-spawned test server.
//
// Why: the CDP browser reaches this container's ports through a forwarder that
// takes a few seconds to notice a freshly bound port. A navigation issued
// immediately after startServer() therefore gets net::ERR_CONNECTION_REFUSED,
// even though the server is up and answering locally. Measured: 0/4 immediate
// navigations succeeded, 4/4 succeeded after an 8s delay.
//
// This was the single cause of the suite's long-standing flakiness — in a full
// run on 2026-07-31, all 29 failures were ERR_CONNECTION_REFUSED and none were
// assertion failures.
//
// Retry rather than sleep: on a warm port the first attempt succeeds and costs
// nothing.

// 30s, up from 20s: since every server binds the same forwarded port
// (server-port.cjs), a navigation issued just after the previous test's server
// was killed and a new one bound can hang rather than refuse — four 5s
// attempts were not always enough. Fits inside the 60s per-test budget.
const DEADLINE_MS = 30000;
const ATTEMPT_TIMEOUT_MS = 5000;
const RETRY_DELAY_MS = 500;

/**
 * Navigate, retrying while the browser cannot reach the server yet.
 * @param {import('@playwright/test').Page} page
 * @param {string} url
 * @param {object} [options] passed through to page.goto
 */
async function gotoRetry(page, url, options) {
  const deadline = Date.now() + DEADLINE_MS;
  for (;;) {
    try {
      // domcontentloaded, not the 'load' default: the app is usable once the
      // document is parsed (specs then wait on #chat-input), and waiting for
      // every subresource is what hangs when the tunnel is mid-hiccup.
      return await page.goto(url, {
        timeout: ATTEMPT_TIMEOUT_MS, waitUntil: 'domcontentloaded', ...options,
      });
    } catch (err) {
      // Only connection-level failures are worth retrying; a bad selector or a
      // real server error should fail loudly and immediately.
      // "interrupted by another navigation to chrome-error://chromewebdata/" is
      // the same refusal wearing a different hat: the failed load swaps in an
      // error page, which Playwright reports as a competing navigation.
      const retryable = /ERR_CONNECTION_REFUSED|ERR_EMPTY_RESPONSE|ERR_CONNECTION_RESET|Timeout .* exceeded|chrome-error:\/\//
        .test(String(err && err.message));
      if (!retryable || Date.now() > deadline) throw err;
      await page.waitForTimeout(RETRY_DELAY_MS);
    }
  }
}

module.exports = { gotoRetry };
