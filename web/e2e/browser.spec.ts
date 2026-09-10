import { expect, test } from '@playwright/test'
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
  await expect(page.locator('.alert-error')).toHaveCount(0)

  await page.getByRole('button', { name: 'Start / resume' }).click()
  await expect(page.locator('dt', { hasText: 'Status' }).locator('+ dd')).toHaveText('running', {
    timeout: live ? 120_000 : 10_000,
  })
  await expect(page.locator('.alert-error')).toHaveCount(0)
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
  await expect(page.locator('.alert-error')).toContainText(
    'conflict: sandbox name already exists',
  )
  await page.request.put('/v1/test/ensure-error', { data: { error: '' } })
})

test('open desktop connects RFB over the product WebSocket', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  const ensure = await page.request.post('/v1/me/environments/browser/ensure')
  if (!ensure.ok()) {
    throw new Error(`ensure: ${ensure.status()} ${await ensure.text()}`)
  }
  await page.goto('/browser')

  const popupPromise = page.waitForEvent('popup')
  await page.getByRole('button', { name: 'Open desktop' }).click()
  const popup = await popupPromise
  await popup.waitForLoadState('domcontentloaded')
  await expect(popup.locator('#status')).toHaveAttribute('data-rfb', 'ready')
  await expect(popup.locator('#status')).toHaveText('Connected', {
    timeout: live ? 60_000 : 15_000,
  })
  await expect(popup.locator('#screen canvas')).toBeVisible()
})
