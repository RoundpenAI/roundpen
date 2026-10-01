import { expect, test } from './fixtures'
import { live, loginViaUi, skipIfNoLivePassword } from './helpers'

// Creating a routine needs the real control plane. The uismoke stub has no
// /v1/routines routes. Run against a live stack:
// PLAYWRIGHT_BASE_URL=http://127.0.0.1:19001 E2E_PASSWORD=... \
//   npx playwright test e2e/routines.spec.ts
test.describe('standing routines', () => {
  test.skip(!live, 'needs a live roundpend serving /v1/routines')

  test('create a weekly routine from the assistant page', async ({ page }) => {
    skipIfNoLivePassword()
    await loginViaUi(page)
    // /a opens the home assistant's chat. Standing routines live on the detail page.
    await page.goto('/a')
    await page.getByRole('button', { name: '详情' }).click()
    await page.waitForURL(/\/a\/[^/]+$/)

    await page.getByRole('button', { name: '新建常驻任务' }).click()
    await page.getByLabel('标题').fill('论文周报')
    await page.getByLabel('说明').fill('汇总过去七天的 AI Agent 论文')
    await page.getByRole('button', { name: '创建' }).click()

    await expect(page.getByText(/RTN-/)).toBeVisible()
    await expect(page.getByText('论文周报')).toBeVisible()
  })
})
