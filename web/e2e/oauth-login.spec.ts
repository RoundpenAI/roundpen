import { expect, test } from './fixtures'
import { loginViaApi, resetSmoke, skipIfNoLivePassword } from './helpers'

test('login page offers the configured providers', async ({ page }) => {
  await page.goto('/login')
  const button = page.getByRole('button', { name: 'Sign in with Gitea' })
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
    'No Roundpen account matches this identity',
  )
})

test('linked accounts lists and disconnects an identity', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/accounts')

  await expect(page.getByText('Gitea · git.eaxi.com')).toBeVisible()
  await expect(page.getByText('octocat', { exact: false })).toBeVisible()

  await page.getByRole('button', { name: '解绑' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()

  await expect(page.getByText('账号已解绑。')).toBeVisible()
  await expect(page.getByText('octocat', { exact: false })).toHaveCount(0)
})

test('admin can add an oauth provider', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/settings/oauth')

  await expect(page.getByText('smoke-client')).toBeVisible()
  await expect(page.getByText('/v1/auth/oauth/gitea-git-eaxi-com/callback')).toBeVisible()

  await page.getByLabel('主机').fill('git2.example.test')
  await page.getByLabel('Client ID').fill('new-client')
  await page.getByLabel('Client secret').fill('new-secret')
  await page.getByRole('button', { name: '新增 provider' }).click()

  await expect(page.getByText('已保存。')).toBeVisible()
})
