import { defineConfig, devices } from '@playwright/test'
import os from 'node:os'

// Set PLAYWRIGHT_CDP_ENDPOINT (e.g. ws://10.10.1.3:3000/chrome for the
// intranet browserless) to run tests on a remote CDP browser. The remote
// browser must reach Vite over the LAN, so bind it non-locally and use this
// host's LAN address as baseURL.
const cdpEndpoint = process.env.PLAYWRIGHT_CDP_ENDPOINT
const apiListen = process.env.UISMOKE_API || '127.0.0.1:19021'
const live = Boolean(process.env.PLAYWRIGHT_BASE_URL)

function lanIPv4(): string {
  for (const infos of Object.values(os.networkInterfaces())) {
    for (const i of infos ?? []) {
      if (i.family === 'IPv4' && !i.internal) return i.address
    }
  }
  return '127.0.0.1'
}

const devHost = cdpEndpoint ? lanIPv4() : '127.0.0.1'
const baseURL = process.env.PLAYWRIGHT_BASE_URL || `http://${devHost}:4173`

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  use: {
    baseURL,
    trace: 'on-first-retry',
  },
  webServer: live
    ? undefined
    : [
        {
          command: `go run ./tests/uismoke -listen ${apiListen} -public ${baseURL}`,
          cwd: '..',
          url: `http://${apiListen}/v1/ready`,
          reuseExistingServer: !process.env.CI,
        },
        {
          command: `npm run dev -- --host ${cdpEndpoint ? '0.0.0.0' : '127.0.0.1'} --port 4173 --strictPort`,
          url: 'http://127.0.0.1:4173',
          reuseExistingServer: !process.env.CI,
          env: { ...process.env, ROUNDPEN_API_PROXY: `http://${apiListen}` },
        },
      ],
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
