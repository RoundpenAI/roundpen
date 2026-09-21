import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('personal settings pick a provider for a slot', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings/slots')

  await expect(page.getByRole('heading', { name: '我的服务' })).toBeVisible()

  // The sandbox agent slot lists the platform providers; pick the second one.
  const card = page.getByRole('group', { name: '沙箱 Agent' })
  const select = card.locator('.semi-select').first()
  await select.click()
  await page.getByRole('option', { name: /openai$/ }).click()
  await expect(select).toContainText('openai')

  // The fake environment reports no live sandbox, so the save lands as a
  // deferred rebuild.
  await expect(page.getByText('已保存；将在创建环境时生效。')).toBeVisible()
})
