import { expect, test } from './fixtures'
import {
  loginViaApi,
  resetSmoke,
  setSmokeProviders,
  skipIfNoLivePassword,
} from './helpers'

test('login page offers the configured providers', async ({ page }) => {
  await page.goto('/login')
  const button = page.getByRole('button', { name: '使用 Gitea 登录' })
  await expect(button).toBeVisible()

  // /v1/auth/oauth/{id}/start redirects to the remote authorize page; the
  // smoke server stands in for it and serves a marker we can wait for.
  await button.click()
  await expect(page.locator('#oauth-authorize-stub')).toBeVisible()
  expect(page.url()).toContain('provider=gitea-git-eaxi-com')
})

test('login page surfaces oauth_error from the callback', async ({ page }) => {
  await page.goto('/login?oauth_error=registration_disabled')
  await expect(page.getByRole('alert').first()).toContainText(
    '没有匹配的 Roundpen 账号',
  )
})

test('connected apps binds, re-authorizes and unbinds oauth', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/apps')

  await expect(page.getByText('连接应用').first()).toBeVisible()
  const row = page.getByTestId('app-installed-oauth-gitea-git-eaxi-com')
  await expect(row).toContainText('Gitea')
  await expect(row).toContainText('octocat')
  await expect(row.getByRole('button', { name: '已添加' })).toBeDisabled()

  // Detail sheet: re-authorize
  await row.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: '重新授权' }).click()
  await expect(page.locator('#oauth-authorize-stub')).toBeVisible()
  expect(page.url()).toContain('provider=gitea-git-eaxi-com')

  await page.goto('/settings/apps')
  await row.click()
  await page.getByRole('dialog').getByRole('button', { name: '卸载' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()

  await expect(page.getByText('已断开连接。')).toBeVisible()
  await expect(page.getByTestId('app-installed-oauth-gitea-git-eaxi-com')).toHaveCount(0)

  // Gitea is added via host + PAT (not admin-pinned OAuth).
  const available = page.getByTestId('app-available-git-gitea')
  await expect(available).toBeVisible()
  await available.getByRole('button', { name: '添加' }).click()
  const addDialog = page.getByRole('dialog')
  await expect(addDialog.getByText(/头像 → 设置 → 应用/)).toBeVisible()
  await expect(addDialog.getByPlaceholder('git.example.com')).toBeVisible()
  await expect(addDialog.getByPlaceholder(/仓库/)).toBeVisible()
})

test('connected apps keeps oauth entry when its provider is disabled', async ({
  page,
}) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await setSmokeProviders(page, [])
  await page.goto('/settings/apps')

  const row = page.getByTestId('app-installed-oauth-gitea-git-eaxi-com')
  await expect(row).toContainText('octocat')
  await row.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('button', { name: '重新授权' })).toHaveCount(0)
  await dialog.getByRole('button', { name: '卸载' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()
  await expect(page.getByText('已断开连接。')).toBeVisible()
  await expect(page.getByTestId('app-installed-oauth-gitea-git-eaxi-com')).toHaveCount(0)
})

test('admin can add an oauth provider after enabling the plugin', async ({
  page,
}) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/admin/settings/plugins')
  await page.getByRole('button', { name: '启用' }).click()
  await expect(page.getByRole('button', { name: '停用' })).toBeVisible()

  await page.goto('/admin/settings/oauth')
  await expect(page.getByText('smoke-client')).toBeVisible()
  await expect(page.getByText('/v1/auth/oauth/gitea-git-eaxi-com/callback')).toBeVisible()

  await page.getByRole('combobox').filter({ hasText: /^GitHub$/ }).click()
  await page.getByRole('option', { name: 'Gitea' }).click()
  await page.getByLabel('主机').fill('git2.example.test')
  await page.getByLabel('Client ID').fill('new-client')
  await page.getByLabel('Client secret').fill('new-secret')
  await page.getByRole('button', { name: '新增 provider' }).click()

  await expect(page.getByText('已保存。')).toBeVisible()
})

test('admin can add GitHub with only client id and secret', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/admin/settings/oauth')

  await expect(page.getByRole('button', { name: '一键在 GitHub 创建应用' })).toBeVisible()
  await expect(page.getByRole('button', { name: '打开插件' })).toBeVisible()
  await expect(page.getByLabel('主机')).toBeHidden()
  await page.getByLabel('Client ID').fill('gh-client')
  await page.getByLabel('Client secret').fill('gh-secret')
  await page.getByRole('button', { name: '新增 provider' }).click()
  await expect(page.getByText('已保存。')).toBeVisible()
})

test('admin one-click GitHub App starts the remote form', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/admin/settings/oauth')

  // Smoke's authorize stub is a GET route; the real GitHub handshake is POST.
  // Intercept so the button's form submit still lands on the local marker.
  await page.route('**/v1/auth/oauth/dev/authorize**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body: '<!doctype html><meta charset="utf-8"><title>authorize stub</title><div id="oauth-authorize-stub">authorize stub</div>',
    })
  })
  await page.getByRole('button', { name: '一键在 GitHub 创建应用' }).click()
  await expect(page.locator('#oauth-authorize-stub')).toBeVisible()
})
