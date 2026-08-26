// @ts-check
const { defineConfig } = require('@playwright/test');
const { isPinned } = require('./e2e/server-port.cjs');

module.exports = defineConfig({
  testDir: './e2e',
  // Fails fast with one actionable message if the lazy CDP endpoint is cold,
  // instead of letting every spec fail at connect with ECONNREFUSED.
  globalSetup: require.resolve('./e2e/global-setup.cjs'),
  // Every spec's server binds the one port the CDP browser can reach (see
  // e2e/server-port.cjs), so two workers would fight over it. Parallel workers
  // are only safe when servers get their own ephemeral ports.
  workers: isPinned() ? 1 : undefined,
  // 60s, not 30s: every test server rebinds the one forwarded port, and the
  // tunnel occasionally refuses the first navigations after a rebind. gotoRetry
  // rides that out in seconds, but a test that opens three sessions could spend
  // its whole 30s budget in one retry loop and time out mid-navigation — seen
  // once in a 167-test run (msg-style-persist, "deleting a saved style"), and
  // that same test takes ~4s when it lands first time.
  timeout: 60000,
  // One retry while pinned: the shared port means constant rebinding, and about
  // one navigation per full 167-test run hangs or is refused past gotoRetry's
  // deadline — a different test each time. A retry absorbs that without hiding
  // anything: a real failure fails twice, and an environment hiccup is reported
  // as flaky rather than passing silently.
  retries: isPinned() ? 1 : 0,
  reporter: 'html',
  use: {
    screenshot: 'on',
  },
});
