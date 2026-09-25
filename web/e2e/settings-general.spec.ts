import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('admin types a manual agent image and it survives a reload', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/general')

  const imageInput = page.getByPlaceholder('ghcr.io/roundpenai/code-agent:0.1.0')
  await expect(imageInput).toBeVisible()

  const image = `ghcr.io/example/agent:${Date.now()}`
  await imageInput.fill(image)
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByText('设置已保存。')).toBeVisible()

  await page.reload()
  await expect(page.getByPlaceholder('ghcr.io/roundpenai/code-agent:0.1.0')).toHaveValue(image)
})
