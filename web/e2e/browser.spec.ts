import { expect, test } from './fixtures'
import { hasSmokeHooks, live, loginViaApi, resetSmoke, skipIfNoLivePassword } from './helpers'

test.describe.configure({ mode: 'serial' })

test('browser start / resume is idempotent in the UI', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/browser')
  await expect(page.getByRole('heading', { name: 'Browser' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Explore & verify' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Start explore' })).toBeVisible()
  if (!live) {
    await expect(page.locator('dt', { hasText: 'Status' }).locator('+ dd')).toHaveText('absent')
  }

  await page.getByRole('button', { name: 'Start / resume' }).click()
  await expect(page.locator('dt', { hasText: 'Status' }).locator('+ dd')).toHaveText('running', {
    timeout: live ? 120_000 : 10_000,
  })
  await expect(page.getByRole('alert')).toHaveCount(0)

  await page.getByRole('button', { name: 'Start / resume' }).click()
  await expect(page.locator('dt', { hasText: 'Status' }).locator('+ dd')).toHaveText('running', {
    timeout: live ? 120_000 : 10_000,
  })
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('browser start surfaces {error} from the API', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  test.skip(!(await hasSmokeHooks(page)), 'ensure-error hook is only on uismoke-api')
  await page.request.put('/v1/test/ensure-error', {
    data: { error: 'conflict: sandbox name already exists' },
  })
  await page.goto('/browser')
  await page.getByRole('button', { name: 'Start / resume' }).click()
  await expect(page.getByRole('alert').first()).toContainText(
    'conflict: sandbox name already exists',
  )
  await page.request.put('/v1/test/ensure-error', { data: { error: '' } })
})

test('browser page exposes the live view entry', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.goto('/browser')
  await expect(page.getByRole('button', { name: 'Open live view' })).toBeVisible()
})
