import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

// Semi's Spin without children is a fixed 20x20 box and renders `tip` inside
// it, so CJK tips wrap one character per line. Guard that tips stay on one line.
test('workspace loading tip renders on one line', async ({ page }) => {
  skipIfNoLivePassword()
  // Never resolve: hold the page in its loading state.
  await page.route('**/v1/me/workspace/files*', () => {})
  await loginViaApi(page)
  await page.goto('/workspace')
  const tip = page.getByText('正在准备工作区…')
  await expect(tip).toBeVisible()
  const box = (await tip.boundingBox())!
  await page.screenshot({ path: 'test-results/loading-tip.png' })
  expect(box.width).toBeGreaterThan(60)
  expect(box.height).toBeLessThan(30)
})
