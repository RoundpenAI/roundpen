import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('images registry lists templates', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/registry')
  await expect(page.getByRole('grid').getByText('browser-desktop')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('settings page loads for admin', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings/general')
  await expect(page.getByText('允许公开注册')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('nav reaches settings from assistants', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/a')
  await page.getByRole('button', { name: '设置' }).first().click()
  await expect(page).toHaveURL(/\/settings/)
  await expect(page.getByText('Git 个人令牌')).toBeVisible()
})
