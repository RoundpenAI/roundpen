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

test('linked accounts binds, re-authorizes and unbinds', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/accounts')

  const row = page
    .getByRole('listitem')
    .filter({ hasText: 'Gitea · git.eaxi.com' })
  await expect(row).toContainText('octocat')

  // A bound provider offers re-authorization for that same provider.
  await row.getByRole('button', { name: '重新授权' }).click()
  await expect(page.locator('#oauth-authorize-stub')).toBeVisible()
  expect(page.url()).toContain('provider=gitea-git-eaxi-com')

  await page.goto('/settings/accounts')
  await row.getByRole('button', { name: '解除绑定' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()

  await expect(page.getByText('账号已解绑。')).toBeVisible()
  await expect(page.getByText('octocat', { exact: false })).toHaveCount(0)

  // The row stays in place, now offering an entry to bind an account again.
  await expect(row).toContainText('未绑定')
  await row.getByRole('button', { name: '去绑定' }).click()
  await expect(page.locator('#oauth-authorize-stub')).toBeVisible()
})

test('linked accounts keeps an entry when no account can be bound yet', async ({
  page,
}) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await setSmokeProviders(page, [])
  await page.goto('/settings/accounts')

  // The binding outlives its provider being switched off: no re-authorization
  // (the provider is gone) but the unbind entry stays.
  const row = page
    .getByRole('listitem')
    .filter({ hasText: 'Gitea · git.eaxi.com' })
  await expect(row).toContainText('octocat')
  await expect(row.getByRole('button', { name: '重新授权' })).toHaveCount(0)
  await row.getByRole('button', { name: '解除绑定' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()
  await expect(page.getByText('账号已解绑。')).toBeVisible()

  // Nothing left to bind, so the page points an admin at provider setup.
  await expect(page.getByText('还没有可绑定的账号类型。')).toBeVisible()
  await page.getByRole('button', { name: '配置 OAuth 登录' }).click()
  await expect(page).toHaveURL(/\/admin\/settings\/oauth$/)
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
