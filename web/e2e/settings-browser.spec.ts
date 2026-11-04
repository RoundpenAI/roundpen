import { expect, test } from './fixtures'
import { loginViaApi, resetSmoke, skipIfNoLivePassword } from './helpers'

test('browser add form is docker vs remote URL', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/admin/settings/browser')

  await page.getByRole('button', { name: '新增条目' }).click()
  const dialog = page.getByRole('dialog')

  await expect(dialog.getByText('运行方式')).toBeVisible()
  await expect(dialog.getByText('本机 Docker（browserless）')).toBeVisible()
  await expect(dialog.getByText('远程 browserless')).toBeVisible()
  await expect(dialog.getByText('Roundpen 会拉取 browserless 镜像')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '安装并启用' })).toBeVisible()

  // Remote only asks for a URL — no id / name / token / port fields.
  await dialog.getByText('远程 browserless').click()
  await expect(dialog.getByText('Browserless URL')).toBeVisible()
  await expect(dialog.getByPlaceholder('wss://browserless.example.com/chrome')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '保存' })).toBeVisible()
  await expect(dialog.getByLabel('ID')).toHaveCount(0)
})
