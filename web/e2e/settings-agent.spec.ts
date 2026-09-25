import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('agent env panel switches the model source', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings/agent')

  const gateway = page.getByRole('radio', { name: '平台网关' })
  const own = page.getByRole('radio', { name: '自己的账号' })
  await expect(gateway).toBeVisible()
  await expect(gateway).toBeChecked()

  await own.click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()

  // The smoke fixture reports no live Agent sandbox, so the switch reports
  // that it will apply on next creation.
  await expect(page.getByText('模型来源已更新，将在创建 Agent 环境时生效。')).toBeVisible()
  await expect(own).toBeChecked()
})

test('agent env panel selects an egress proxy', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings/agent')

  const select = page.locator('.semi-select').first()
  await select.click()
  await page.getByRole('option', { name: /US egress/ }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()

  await expect(page.getByText('代理已更新，将在创建 Agent 环境时生效。')).toBeVisible()
  await expect(select).toContainText('US egress')
})
