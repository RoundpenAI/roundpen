import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('browser page selects an egress proxy', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/browser')

  // The egress picker is labelled via aria-labelledby (locale toggle has no
  // label of its own), so the accessible name disambiguates it from the
  // header's language combobox.
  const select = page.getByRole('combobox', { name: /Egress proxy/ })
  await expect(select).toBeVisible()
  await select.click()
  await page.getByRole('option', { name: /JP egress/ }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()

  await expect(select).toContainText('JP egress')
})
