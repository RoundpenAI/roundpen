import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('admin general redirects; registration is env-only', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/general')

  await expect(page).toHaveURL(/\/admin\/settings\/llmgw$/)
  await expect(page.getByText('允许公开注册')).toHaveCount(0)
})

test('admin env catalog lists configurable variables', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/env')

  await expect(page.getByText(/在安装目录的 \.env/)).toBeVisible()
  await expect(page.getByText('ROUNDPEN_DEFAULT_IMAGE')).toBeVisible()
  await expect(page.getByText('ROUNDPEN_AGENT_IMAGE')).toBeVisible()
  await expect(page.getByText('ROUNDPEN_DEFAULT_TTL')).toBeVisible()
  await expect(page.getByText('ROUNDPEN_ALLOW_PUBLIC_REGISTRATION')).toBeVisible()
})
