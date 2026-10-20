import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('shows notifications settings when Web Push is available', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings/apps')
  await expect(page.getByRole('button', { name: '通知' })).toBeVisible()
  await page.getByRole('button', { name: '通知' }).click()
  await expect(page).toHaveURL(/\/settings\/notifications$/)
})

test('hides notifications settings when Web Push is unavailable', async ({ page }) => {
  skipIfNoLivePassword()
  await page.addInitScript(() => {
    Object.defineProperty(globalThis, 'Notification', {
      get() {
        return undefined
      },
      configurable: true,
    })
    Reflect.deleteProperty(window, 'PushManager')
  })
  await loginViaApi(page)
  await page.goto('/settings/apps')
  await expect(page.getByRole('button', { name: '通知' })).toHaveCount(0)

  await page.goto('/settings/notifications')
  await expect(page).toHaveURL(/\/settings\/accounts$/)
  await expect(page.getByRole('button', { name: '通知' })).toHaveCount(0)
})
