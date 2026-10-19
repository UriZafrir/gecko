// The web app (PWA) on phones and tablets: WebKit (iPhone) and Chromium
// (Android) with touch, against a throwaway Gecko daemon (global-setup.js).
// Run with `make test-pwa`. One worker: the tests share the daemon, and
// settings such as the theme sync to every page.
import { defineConfig, devices } from '@playwright/test';

// Chromium also reaches the daemon as devbox.test, a page opened from
// another device (the anchor test).
const remote = { launchOptions: { args: ['--host-resolver-rules=MAP devbox.test 127.0.0.1'] } };
const phone = (name, extra = {}) => ({ ...devices[name], ...(devices[name].defaultBrowserType === 'chromium' ? remote : {}), ...extra });

export default defineConfig({
  testDir: 'tests',
  timeout: 90_000,
  expect: { timeout: 10_000, toHaveScreenshot: { maxDiffPixelRatio: 0.03, animations: 'disabled' } },
  workers: 1,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  globalSetup: './global-setup.js',
  globalTeardown: './global-teardown.js',
  snapshotPathTemplate: '{testDir}/__screenshots__/{projectName}/{arg}{ext}',
  use: { trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [
    { name: 'iphone-15', use: phone('iPhone 15') },
    { name: 'iphone-se', use: phone('iPhone SE') },
    { name: 'pixel-8', use: phone('Pixel 7', { viewport: { width: 412, height: 915 }, screen: { width: 412, height: 915 } }) },
    // A small Android phone: 360 dp wide.
    { name: 'small-360', use: phone('Galaxy S9+', { viewport: { width: 360, height: 740 }, screen: { width: 360, height: 740 } }) },
    // A tablet: the sidebar beside the terminal, with touch.
    { name: 'tablet', use: phone('Galaxy Tab S4 landscape') },
    // A browser tab on a computer (Make anchor works on every device).
    { name: 'desktop-chrome', testMatch: /anchor\.spec/, use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 }, ...remote } },
    { name: 'desktop-safari', testMatch: /anchor\.spec/, use: { ...devices['Desktop Safari'], viewport: { width: 1280, height: 800 } } },
  ],
});
