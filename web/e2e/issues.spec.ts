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

    // Land on the detail page, clarifying and with no documents yet. The status
    // assertions go through the visible copy: Semi's Select does not forward an
    // accessible name, and what matters is the status the user reads.
    await expect(page).toHaveURL(/\/issues\/ISS-\d+/)
    await expect(page.getByText('澄清中').first()).toBeVisible()
    await expect(page.getByText('还没有文档。')).toBeVisible()

    // Spec v1 (current) advances the issue to specced.
    await page.getByRole('button', { name: '新版本' }).click()
    await page
      .getByPlaceholder('用 Markdown 编写正文…')
      .fill('# 目标\n\n验证议题闭环。')
    await page.getByRole('button', { name: '创建' }).click()
    await expect(page.getByText('当前版本')).toBeVisible()
    await expect(page.getByText('Spec 已定稿').first()).toBeVisible()

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

    // Semi paints a display span over the input, so a pointer click never
    // reaches it; a real user's click toggles through the wrapping label.
    await page
      .getByRole('checkbox', { name: '验证任务闭环' })
      .press('Space')

    // The issue closes itself. Assert on the issue's own status, not on any
    // "已完成" in the task row — the loose version passed while the issue was
    // still open.
    await expect(page.getByTestId('issue-status')).toContainText('已完成')
  })
})
