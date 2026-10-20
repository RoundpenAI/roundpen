import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

test('settings page loads for admin', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/llmgw')
  await expect(page.getByText('模型配置')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('admin settings keeps a mobile path back to the primary menu', async ({
  page,
}) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/settings/llmgw')
  // "/admin" must not be treated as "/a…" and hide the shell mobile bar.
  await page.getByRole('button', { name: '打开菜单' }).click()
  await expect(page.getByRole('button', { name: '助手' })).toBeVisible()
  await expect(page.getByRole('button', { name: '系统管理' })).toBeVisible()
})

test('nav reaches settings from assistants', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/a')
  await page.getByRole('button', { name: '设置' }).first().click()
  await expect(page).toHaveURL(/\/settings/)
  // Both the desktop sider and the mobile tab strip render this label; the
  // role locator skips the CSS-hidden copy that is out of the a11y tree.
  await expect(page.getByRole('button', { name: 'Git 个人令牌' })).toBeVisible()
})

test('platform settings bookmarks move admins to the admin area', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings/automode')
  await expect(page).toHaveURL(/\/admin\/settings\/automode$/)
  await expect(page.getByText('沙箱与运行时')).toBeVisible()
})
