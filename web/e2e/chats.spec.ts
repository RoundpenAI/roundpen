import { expect, test } from '@playwright/test'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('assistants create wizard is reachable', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/a/new')
  await expect(page.getByRole('heading', { name: '新建助手' })).toBeVisible()
  await expect(page.getByLabel('名称')).toBeVisible()
})
