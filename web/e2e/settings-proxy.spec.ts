import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('admin proxy items round-trip through save', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/proxy')

  // The smoke fixture ships two items; a new one lands at the end.
  await page.getByRole('button', { name: '新增条目' }).click()
  const card = page.getByRole('group', { name: '条目 3' })
  const id = `zz-egress-${Date.now()}`
  await card.getByPlaceholder('us-egress').fill(id)
  await card.locator('input.semi-input').nth(1).fill('ZZ egress')
  await card.locator('input[type="password"]').fill('http://127.0.0.1:3128')
  await card.getByRole('button', { name: '保存' }).click()
  await expect(page.getByText('条目已保存。')).toBeVisible()

  await page.reload()
  const saved = page.getByRole('group', { name: '条目 3' })
  await expect(saved.getByPlaceholder('us-egress')).toHaveValue(id)
  // Secret fields come back masked, never in the clear.
  await expect(saved.locator('input[type="password"]')).toHaveValue('●●●●●●●●')
  await expect(page.getByText('127.0.0.1:3128')).toHaveCount(0)
})
