import { expect, test } from '@playwright/test'
import { ADMIN, live, loginViaUi, skipIfNoLivePassword } from './helpers'

test.describe('assistants', () => {
  test.skip(!live && !ADMIN.password, 'needs local or live credentials')

  test('create assistant and open chat', async ({ page }) => {
    skipIfNoLivePassword()
    await loginViaUi(page)
    await page.goto('/a/new')
    await page.getByLabel('名称').fill('测试助手')
    await page.getByRole('button', { name: '下一步' }).click()
    await page.getByText('代理我', { exact: true }).click()
    await page.getByRole('button', { name: '下一步' }).click()
    await page.getByText('代码（含终端）').click()
    await page.getByRole('button', { name: '创建' }).click()
    await expect(page).toHaveURL(/\/a\/.+\/s\//, { timeout: 120_000 })
  })
})
