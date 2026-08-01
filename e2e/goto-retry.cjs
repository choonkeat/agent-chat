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

const DEADLINE_MS = 20000;
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
      return await page.goto(url, { timeout: ATTEMPT_TIMEOUT_MS, ...options });
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
