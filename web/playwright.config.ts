import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:4173'
const apiListen = process.env.UISMOKE_API || '127.0.0.1:19021'
const live = Boolean(process.env.PLAYWRIGHT_BASE_URL)

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
          command: 'npm run dev -- --host 127.0.0.1 --port 4173 --strictPort',
          url: 'http://127.0.0.1:4173',
          reuseExistingServer: !process.env.CI,
          env: { ...process.env, ROUNDPEN_API_PROXY: `http://${apiListen}` },
        },
      ],
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
