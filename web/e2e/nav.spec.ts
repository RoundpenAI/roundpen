import { expect, test } from '@playwright/test'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('images registry lists templates', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/registry')
  await expect(page.getByRole('table').getByText('browser-desktop')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('settings page loads for admin', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings')
  await expect(page.getByText('Allow public registration')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('nav reaches settings from assistants', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/a')
  await page.getByRole('button', { name: '设置' }).first().click()
  await expect(page.getByText('Allow public registration')).toBeVisible()
})
