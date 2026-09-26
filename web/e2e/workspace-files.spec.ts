import type { Page, Route } from '@playwright/test'
import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

type Call = { method: string; path: string; dest?: string }

const ROOT_ROWS = [
  { name: 'docs', is_dir: true, size: 0, mod_time: '2026-09-20T08:30:00Z' },
  { name: 'notes.txt', is_dir: false, size: 2048, mod_time: '2026-09-21T10:05:00Z' },
  { name: 'pic.png', is_dir: false, size: 4096, mod_time: '2026-09-22T11:00:00Z' },
  { name: 'blob.bin', is_dir: false, size: 512, mod_time: '2026-09-23T12:00:00Z' },
]

const DOCS_ROWS = [
  { name: 'inner.md', is_dir: false, size: 12, mod_time: '2026-09-19T09:00:00Z' },
]

/**
 * Serves every workspace file API from one handler: list per directory,
 * content for preview, and 204 for delete/move/copy. Every call is recorded
 * so the tests can assert what the UI actually sent.
 */
async function mockWorkspace(page: Page): Promise<Call[]> {
  const calls: Call[] = []
  await page.route('**/v1/me/workspace/files**', async (route: Route) => {
    const req = route.request()
    const url = new URL(req.url())
    const rel = url.searchParams.get('path') ?? '.'
    const method = req.method()
    if (url.pathname.endsWith('/content')) {
      calls.push({ method, path: rel })
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'hello from ' + rel,
      })
      return
    }
    let dest: string | undefined
    if (method === 'POST') {
      try {
        dest = (JSON.parse(req.postData() ?? '{}') as { dest?: string }).dest
      } catch {
        /* uploads have no JSON body */
      }
    }
    calls.push({ method, path: rel, dest })
    if (method === 'GET') {
      const entries = rel === 'docs' ? DOCS_ROWS : ROOT_ROWS
      await route.fulfill({ json: { entries, path: rel } })
      return
    }
    await route.fulfill({ status: 204, body: '' })
  })
  return calls
}

test('file manager lists four columns, navigates via breadcrumbs and batch-deletes', async ({
  page,
}) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  const calls = await mockWorkspace(page)

  await page.goto('/workspace')
  // Columns.
  for (const head of ['名称', '大小', '类型', '修改时间']) {
    await expect(page.getByRole('columnheader', { name: head })).toBeVisible()
  }
  await expect(page.getByText('notes.txt')).toBeVisible()
  await expect(page.getByText('2.00 KB')).toBeVisible()
  await expect(page.getByText('文本', { exact: true })).toBeVisible()

  // Double-click enters a directory and the breadcrumb walks back.
  await page.getByText('docs').dblclick()
  await expect.poll(() => calls.filter((c) => c.method === 'GET').at(-1)?.path).toBe('docs')
  await expect(page.getByText('inner.md')).toBeVisible()
  await page.getByRole('button', { name: '工作区' }).click()
  await expect(page.getByText('notes.txt')).toBeVisible()

  // Tick two rows, use the selection dropdown next to Refresh, confirm the
  // dialog, expect two DELETEs.
  // (Semi paints the checkbox with an overlay, so click its wrapper.)
  const checkboxFor = (name: string) =>
    page
      .locator('.rp-ws-table .semi-table-row')
      .filter({ hasText: name })
      .locator('.semi-checkbox')
      .first()
  await checkboxFor('notes.txt').click()
  await checkboxFor('blob.bin').click()
  await expect(page.getByText('已选 2 项')).toBeVisible()
  await page.getByRole('button', { name: '已选 2 项' }).click()
  const menu = page.getByRole('menu')
  for (const item of ['剪切', '复制', '删除', '取消选择']) {
    await expect(menu.getByRole('menuitem', { name: item })).toBeVisible()
  }
  await menu.getByRole('menuitem', { name: '删除' }).click()
  await expect(page.getByText('删除选中的 2 项？')).toBeVisible()
  await page.locator('.semi-modal-footer .semi-button-danger').click()
  await expect
    .poll(() => calls.filter((c) => c.method === 'DELETE').map((c) => c.path).sort())
    .toEqual(['blob.bin', 'notes.txt'])
})

test('file manager renames inline and multi-selects with modifiers', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  const calls = await mockWorkspace(page)

  await page.goto('/workspace')
  await expect(page.getByText('notes.txt')).toBeVisible()

  // Right-click → Rename turns the name into an input; Enter commits a move.
  await page.getByText('notes.txt').click({ button: 'right' })
  await page.getByRole('menu').getByRole('menuitem', { name: '重命名' }).click()
  const input = page.locator('.rp-ws-table input.semi-input')
  await expect(input).toBeVisible()
  await input.fill('renamed.txt')
  await input.press('Enter')
  await expect
    .poll(() => calls.find((c) => c.method === 'POST' && c.path === 'notes.txt'))
    .toMatchObject({ dest: 'renamed.txt' })

  // Plain click replaces the selection; Ctrl+click adds a row.
  await page.getByText('blob.bin').click()
  await expect(page.getByText('已选 1 项')).toBeVisible()
  await page.getByText('pic.png').click({ modifiers: ['Control'] })
  await expect(page.getByText('已选 2 项')).toBeVisible()
})

test('file manager cuts, pastes, and previews through the context menu', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  const calls = await mockWorkspace(page)

  await page.goto('/workspace')
  await expect(page.getByText('notes.txt')).toBeVisible()

  // Right-click a row: the menu offers file actions, cut puts it aside.
  await page.getByText('notes.txt').click({ button: 'right' })
  const menu = page.getByRole('menu')
  await expect(menu).toBeVisible()
  for (const item of ['预览', '下载', '剪切', '复制', '重命名', '删除']) {
    await expect(menu.getByRole('menuitem', { name: item })).toBeVisible()
  }
  await menu.getByRole('menuitem', { name: '剪切' }).click()
  await expect(page.getByText('已剪切 1 项')).toBeVisible()
  // A cut row dims until it is pasted.
  await expect(
    page.locator('.rp-ws-table .semi-table-row').filter({ hasText: 'notes.txt' }),
  ).toHaveCSS('opacity', '0.5')

  // Right-click empty space (page padding, not a row): paste moves the cut
  // entry into this directory.
  await page.locator('.rp-ws-page').click({ button: 'right', position: { x: 8, y: 8 } })
  await page.getByRole('menu').getByRole('menuitem', { name: '粘贴' }).click()
  await expect
    .poll(() => calls.find((c) => c.method === 'POST' && c.path === 'notes.txt'))
    .toMatchObject({ dest: '.' })

  // Double-click a previewable file → dialog; unsupported type → hint.
  await page.getByText('pic.png').dblclick()
  await expect(page.locator('.semi-modal img')).toHaveAttribute('src', /pic\.png/)
  await page.getByRole('button', { name: '关闭' }).click()
  await expect(page.locator('.semi-modal')).not.toBeVisible()

  await page.getByText('blob.bin').dblclick()
  await expect(page.getByText('该类型暂不支持预览')).toBeVisible()
})
