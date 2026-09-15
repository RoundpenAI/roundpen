import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('browser page selects an egress proxy', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/browser')

  const select = page.locator('.semi-select').first()
  await expect(select).toBeVisible()
  await select.click()
  await page.getByRole('option', { name: /JP egress/ }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()

  await expect(select).toContainText('JP egress')
})
