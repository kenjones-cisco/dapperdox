const { defineConfig, devices } = require('@playwright/test');

module.exports = defineConfig({
  testDir: __dirname,
  outputDir: `../../test-results/${process.env.BROWSER_TEST_THEME || 'default'}`,
  workers: 1,
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
    { name: 'firefox', use: { browserName: 'firefox' } },
    { name: 'webkit', use: { browserName: 'webkit' } },
    { name: 'mobile-safari', use: { ...devices['iPhone 13'] } },
  ],
  use: { baseURL: 'http://127.0.0.1:32179', headless: true },
  webServer: {
    cwd: require('node:path').resolve(__dirname, '../..'),
    command: 'go build -o /tmp/dapperdox-browser-server ./tests/browser/server && /tmp/dapperdox-browser-server',
    url: 'http://127.0.0.1:32179/readyz',
    reuseExistingServer: false,
    timeout: 120000,
  },
});
