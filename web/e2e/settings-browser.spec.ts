import { expect, test } from './fixtures'
import { loginViaApi, resetSmoke, skipIfNoLivePassword } from './helpers'

test('browser item editor explains every provider', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/admin/settings/browser')

  await page.getByRole('button', { name: '新增条目' }).click()
  const dialog = page.getByRole('dialog')

  // Nothing picked yet: the generic hint stands in for the per-provider text.
  await expect(dialog.getByText('决定浏览器跑在哪里')).toBeVisible()

  const select = dialog.locator('.semi-select').first()
  await select.click()
  // Readable names instead of the raw auto/docker/host/remote/cloud ids.
  await expect(page.getByRole('option', { name: '平台托管容器（browserless）' })).toBeVisible()
  await expect(page.getByRole('option', { name: '局域网 / 自建 browserless' })).toBeVisible()

  await page.getByRole('option', { name: '本机 Chrome（仅调试）' }).click()
  await expect(dialog.getByText('没有实时视图，只能用截图接管面板操作')).toBeVisible()

  await select.click()
  await page.getByRole('option', { name: '云浏览器服务' }).click()
  await expect(dialog.getByText('endpoint 必填：粘贴服务商给的 CDP')).toBeVisible()
})
