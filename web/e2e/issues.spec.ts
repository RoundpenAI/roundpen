import { expect, test } from './fixtures'
import { live, loginViaUi, skipIfNoLivePassword } from './helpers'

// The issue tracker needs the real control plane: the uismoke stub server
// (started by playwright.config for non-live runs) has no /v1/issues routes,
// and stubbing them would test the stub rather than the API. Run this file
// against a running stack: PLAYWRIGHT_BASE_URL=http://127.0.0.1:19001 \
//   E2E_PASSWORD=... npx playwright test e2e/issues.spec.ts
test.describe('issues', () => {
  test.skip(!live, 'needs a live roundpend serving /v1/issues')

  test('create an issue, write a spec, derive a task and finish it', async ({
    page,
  }) => {
    skipIfNoLivePassword()
    await loginViaUi(page)

    // Create from the console.
    await page.goto('/issues')
    await page.getByRole('button', { name: '新建议题' }).click()
    await page.getByLabel('标题').fill('e2e 议题')
    await page.getByRole('button', { name: '创建' }).click()

    // Land on the detail page, clarifying and with no documents yet.
    await expect(page).toHaveURL(/\/issues\/ISS-\d+/)
    const status = page.getByLabel('状态')
    await expect(status).toBeVisible()
    await expect(page.getByText('还没有文档。')).toBeVisible()

    // Spec v1 (current) advances the issue to specced.
    await page.getByRole('button', { name: '新版本' }).click()
    await page
      .getByPlaceholder('用 Markdown 编写正文…')
      .fill('# 目标\n\n验证议题闭环。')
    await page.getByRole('button', { name: '创建' }).click()
    await expect(page.getByText('当前版本')).toBeVisible()
    await expect(status.locator('..').getByText('Spec 已定稿')).toBeVisible()

    // A second revision supersedes the first rather than overwriting it.
    await page.getByRole('button', { name: '新版本' }).click()
    await page.getByPlaceholder('用 Markdown 编写正文…').fill('# 目标 v2')
    await page.getByRole('button', { name: '创建' }).click()
    await expect(page.getByText('已被取代')).toBeVisible()

    // One task, ticked to done: the issue closes itself.
    await page.getByRole('button', { name: '新增任务' }).click()
    await page.getByPlaceholder('标题').fill('验证任务闭环')
    await page.getByRole('button', { name: '创建' }).click()
    await expect(page.getByText('验证任务闭环')).toBeVisible()

    await page.getByRole('checkbox').first().click()
    await expect(page.getByText('已完成').first()).toBeVisible()
  })
})
