import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

// Former /settings/agent page was removed; egress proxy lives under My services.
test('legacy /settings/agent redirects to my services', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings/agent')
  await expect(page).toHaveURL(/\/settings\/slots/)
  await expect(page.getByText(/我的服务|My services/)).toBeVisible()
})

test('my services selects an agent egress proxy', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings/slots')

  const group = page.getByRole('group', { name: /Agent 出口代理|Agent egress/ })
  await expect(group).toBeVisible()
  const select = group.locator('.semi-select').first()
  await select.click()
  await page.getByRole('option', { name: /US egress/ }).click()

  await expect(page.getByText(/已保存|Selection saved/)).toBeVisible()
  await expect(select).toContainText('US egress')
})
