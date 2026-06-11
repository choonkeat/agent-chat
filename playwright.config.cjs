// @ts-check
const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
  testDir: './e2e',
  // Fails fast with one actionable message if the lazy CDP endpoint is cold,
  // instead of letting every spec fail at connect with ECONNREFUSED.
  globalSetup: require.resolve('./e2e/global-setup.cjs'),
  timeout: 30000,
  retries: 0,
  reporter: 'html',
  use: {
    screenshot: 'on',
  },
});
