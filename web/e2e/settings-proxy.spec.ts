import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('admin proxy items round-trip through save', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/proxy')

  // Adding happens in an overlay; the page keeps a summary list.
  await page.getByRole('button', { name: '新增条目' }).click()
  const dialog = page.getByRole('dialog')
  const id = `zz-egress-${Date.now()}`
  await dialog.getByPlaceholder('us-egress').fill(id)
  await dialog.locator('input.semi-input').nth(1).fill('ZZ egress')
  await dialog.locator('input[type="password"]').fill('http://127.0.0.1:3128')
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(page.getByText('条目已保存。')).toBeVisible()
  await expect(dialog).toBeHidden()

  const row = page.getByRole('group', { name: 'ZZ egress' })
  await expect(row).toBeVisible()
  // Secret fields never come back in the clear.
  await expect(page.getByText('127.0.0.1:3128')).toHaveCount(0)

  await page.reload()
  await row.getByRole('button', { name: '编辑' }).click()
  await expect(page.getByRole('dialog').locator('input[type="password"]')).toHaveValue('●●●●●●●●')
})
