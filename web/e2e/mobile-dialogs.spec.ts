import { expect, test } from './fixtures'
import type { Page } from '@playwright/test'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

// Semi gives every dialog a fixed width (448 by default, 520/720 for the
// settings forms), which pushed the right edge past the screen on a phone.
const PHONE = { width: 390, height: 844 }

async function expectDialogFitsPhone(page: Page) {
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  const box = await dialog.boundingBox()
  expect(box).not.toBeNull()
  const viewport = page.viewportSize()
  expect(viewport).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewport!.width)
}

test('auto mode rules dialog fits a phone screen', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.setViewportSize(PHONE)
  await page.goto('/admin/settings/automode')

  await page.getByRole('button', { name: '查看内置规则' }).click()
  await expect(page.getByText('Force-pushing or rewriting remote git history.')).toBeVisible()
  await expectDialogFitsPhone(page)
})

test('setting item editor fits a phone screen', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.setViewportSize(PHONE)
  await page.goto('/admin/settings/proxy')

  await page.getByRole('button', { name: '新增条目' }).click()
  await expect(page.getByText('socks5h，可带 user:pass', { exact: false })).toBeVisible()
  await expectDialogFitsPhone(page)
})
