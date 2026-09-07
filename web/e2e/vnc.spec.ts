import { expect, test } from '@playwright/test'
import { desktopWsUrl, live, skipIfNoLivePassword } from './helpers'

test('vnc.html completes RFB handshake on the real desktop URL', async ({ page }) => {
  skipIfNoLivePassword()
  const wsUrl = await desktopWsUrl(page)
  expect(wsUrl).toMatch(/\/v1\/me\/environments\/browser\/desktop\/ws\?token=/)

  await page.goto('/vnc.html?url=' + encodeURIComponent(wsUrl))
  await expect(page.locator('#status')).toHaveAttribute('data-rfb', 'ready', {
    timeout: 10_000,
  })
  await expect(page.locator('#status')).toHaveText('Connected', {
    timeout: live ? 60_000 : 15_000,
  })
  await expect(page.locator('#status')).not.toContainText('Failed to load VNC client')
  await expect(page.locator('#status')).not.toContainText('RFB is not a constructor')

  const canvas = page.locator('#screen canvas')
  await expect(canvas).toBeVisible()
  if (live) {
    return
  }
  await expect
    .poll(async () => {
      return canvas.evaluate((el) => {
        const c = el as HTMLCanvasElement
        if (c.width < 8 || c.height < 8) return null
        const ctx = c.getContext('2d')
        if (!ctx) return null
        const { data } = ctx.getImageData(8, 8, 1, 1)
        return [data[0], data[1], data[2]]
      })
    }, { timeout: 10_000 })
    .toEqual([0xcc, 0x22, 0x44])
})

test('vnc.html without url explains the missing query', async ({ page }) => {
  await page.goto('/vnc.html')
  await expect(page.locator('#status')).toHaveText('Missing ?url= websocket')
})
