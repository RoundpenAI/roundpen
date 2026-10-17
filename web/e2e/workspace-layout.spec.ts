import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

// The page box must not size to its content: the flex-column shell plus the
// centering auto margins made it collapse to the loading spinner and then
// stretch once the table rendered, which read as a flicker. The container now
// claims the full width (capped at 960) from the first paint.
test('workspace page keeps a stable width from loading to loaded', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)

  await page.route('**/v1/me/workspace/files**', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 1200))
    await route.fulfill({
      json: {
        entries: [
          { name: 'a'.repeat(140) + '-wide.txt', is_dir: false, size: 12 },
        ],
      },
    })
  })

  await page.goto('/workspace')
  const container = page
    .getByRole('heading', { level: 3, name: /^(文件|Files)$/ })
    .locator('..')
  await expect(container).toBeVisible()

  const loadingWidth = await container.evaluate(
    (el) => el.getBoundingClientRect().width,
  )
  // Before the fix this was the fit-content width of the title block.
  expect(loadingWidth).toBeGreaterThan(400)

  await expect(page.getByText('-wide.txt', { exact: false })).toBeVisible()
  const loadedWidth = await container.evaluate(
    (el) => el.getBoundingClientRect().width,
  )
  expect(Math.abs(loadedWidth - loadingWidth)).toBeLessThanOrEqual(1)
  expect(loadedWidth).toBeLessThanOrEqual(960)
})

// On phones the size/type/modified columns made the Semi table wider than the
// viewport; its overflow:auto body then captured vertical pans as horizontal
// swipes. Compact layout keeps only the name column (meta under the name).
test('workspace table is compact on a phone viewport', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await page.setViewportSize({ width: 390, height: 844 })

  await page.route('**/v1/me/workspace/files**', async (route) => {
    await route.fulfill({
      json: {
        entries: [
          {
            name: 'notes.txt',
            is_dir: false,
            size: 2048,
            mod_time: '2026-09-21T10:05:00Z',
          },
        ],
      },
    })
  })

  await page.goto('/workspace')
  await expect(page.getByText('notes.txt', { exact: true })).toBeVisible()
  await expect(page.locator('.rp-ws-table-narrow')).toBeVisible()
  await expect(page.locator('.rp-ws-name-meta')).toBeVisible()
  // Desktop column headers must not appear in the compact layout.
  await expect(
    page.getByRole('columnheader', { name: /^(大小|Size)$/ }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('columnheader', { name: /^(类型|Type)$/ }),
  ).toHaveCount(0)

  const bodyOverflowX = await page
    .locator('.rp-ws-table-narrow .semi-table-body')
    .evaluate((el) => getComputedStyle(el).overflowX)
  expect(bodyOverflowX).toBe('hidden')
})
