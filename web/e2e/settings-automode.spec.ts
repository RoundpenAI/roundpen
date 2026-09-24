import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('auto mode editor shows built-in rules', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/automode')

  await expect(page.getByText('会话打开 Auto 后', { exact: false })).toBeVisible()

  await page.getByRole('button', { name: '查看内置规则' }).click()
  await expect(
    page.getByText('Force-pushing or rewriting remote git history.'),
  ).toBeVisible()
  await expect(page.getByText('Reaching cloud metadata or control-plane addresses.')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(
    page.getByText('Force-pushing or rewriting remote git history.'),
  ).toBeHidden()
})

test('auto mode custom rule round-trips through save', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/automode')

  // The first "新增规则" button belongs to the environment list; its entry
  // input is the first text input on the page.
  await page.getByRole('button', { name: '新增规则' }).first().click()
  const rule = `Trusted staging bucket ${Date.now()}`
  await page.locator('input.semi-input').first().fill(rule)

  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByText('设置已保存。')).toBeVisible()

  await page.reload()
  await expect(page.locator('input.semi-input').first()).toHaveValue(rule)
})
