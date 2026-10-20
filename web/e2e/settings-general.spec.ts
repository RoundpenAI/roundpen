import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('admin general only exposes public registration', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/general')

  await expect(page.getByText('允许公开注册')).toBeVisible()
  await expect(page.getByText(/ROUNDPEN_DEFAULT_IMAGE/)).toHaveCount(0)
  await expect(page.getByPlaceholder('ghcr.io/roundpenai/code-agent:0.1.0')).toHaveCount(0)
  await expect(page.getByText('默认沙箱 TTL')).toHaveCount(0)
})

test('admin env catalog lists configurable variables', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/env')

  await expect(page.getByText(/在安装目录的 \.env/)).toBeVisible()
  await expect(page.getByText('ROUNDPEN_DEFAULT_IMAGE')).toBeVisible()
  await expect(page.getByText('ROUNDPEN_AGENT_IMAGE')).toBeVisible()
  await expect(page.getByText('ROUNDPEN_DEFAULT_TTL')).toBeVisible()
})
