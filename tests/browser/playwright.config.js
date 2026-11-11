const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
  testDir: __dirname,
  workers: 1,
  use: { baseURL: 'http://127.0.0.1:32179', headless: true },
  webServer: {
    cwd: require('node:path').resolve(__dirname, '../..'),
    command: 'go build -o /tmp/dapperdox-browser-server ./tests/browser/server && /tmp/dapperdox-browser-server',
    url: 'http://127.0.0.1:32179/readyz',
    reuseExistingServer: false,
    timeout: 120000,
  },
});
