import { test as base, expect } from '@playwright/test'

// Set PLAYWRIGHT_CDP_ENDPOINT (e.g. ws://10.10.1.3:3000/chrome for the
// intranet browserless) to run tests on a remote CDP browser instead of
// launching Chromium locally. Playwright config connectOptions only speaks the
// Playwright server protocol, so CDP needs this fixture override.
const cdpEndpoint = process.env.PLAYWRIGHT_CDP_ENDPOINT

export const test = cdpEndpoint
  ? base.extend({
      browser: async ({ playwright }, use) => {
        const browser = await playwright.chromium.connectOverCDP(cdpEndpoint)
        await use(browser)
        await browser.close()
      },
    })
  : base

export { expect }
