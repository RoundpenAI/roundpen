import { expect, test } from '@playwright/test'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('chats landing lists agent and composer', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/chats')
  await expect(page.getByRole('heading', { name: 'What are we working on?' })).toBeVisible()
  await expect(page.getByPlaceholder('Message the agent…')).toBeVisible()
  await page.getByPlaceholder('Message the agent…').click()
  await expect(page.getByRole('combobox')).toContainText(/System Agent|Sysadmin|Claude/)
})
