import { expect, test } from './fixtures'
import { loginViaApi, resetSmoke, skipIfNoLivePassword } from './helpers'

test('mcp settings connects an API key server and manages the connection', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/apps')

  await expect(page.getByText('连接应用')).toBeVisible()
  await expect(page.getByTestId('mcp-server-notion')).toBeVisible()
  await expect(page.getByTestId('mcp-server-exa')).toBeVisible()
  await expect(page.getByTestId('mcp-server-linear')).toBeVisible()

  // Exa is key-only: open detail, paste key, save.
  const exa = page.getByTestId('mcp-server-exa')
  await exa.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('button', { name: '使用 OAuth 连接' })).toHaveCount(0)
  await dialog.getByPlaceholder('粘贴 API key').fill('demo-key')
  await dialog.getByRole('button', { name: '保存 key' }).click()

  const conn = page.getByTestId('mcp-conn-exa')
  await expect(conn).toBeVisible()
  await expect(conn).toContainText('2 个工具')
  await expect(conn.getByRole('button', { name: '已添加' })).toBeDisabled()

  await conn.click()
  const installed = page.getByRole('dialog')
  await installed.getByRole('button', { name: '卸载' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'confirm' }).click()
  await expect(page.getByText('已断开连接。')).toBeVisible()
  await expect(page.getByTestId('mcp-conn-exa')).toHaveCount(0)
})

test('mcp settings rejects a bad API key', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/apps')

  await page.getByTestId('mcp-server-exa').click()
  const dialog = page.getByRole('dialog')
  await dialog.getByPlaceholder('粘贴 API key').fill('bad')
  await dialog.getByRole('button', { name: '保存 key' }).click()

  await expect(page.getByText(/authentication with the MCP server failed/)).toBeVisible()
  await expect(page.getByTestId('mcp-conn-exa')).toHaveCount(0)
})

test('mcp dual-auth server switches between OAuth and API key', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/apps')

  await page.getByTestId('mcp-server-linear').click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('button', { name: '使用 OAuth 连接' })).toBeVisible()
  await expect(dialog.getByPlaceholder('粘贴 API key')).toHaveCount(0)

  await dialog.getByRole('button', { name: '改用 API key' }).click()
  await expect(dialog.getByPlaceholder('粘贴 API key')).toBeVisible()

  await dialog.getByRole('button', { name: '改用 OAuth' }).click()
  await expect(dialog.getByPlaceholder('粘贴 API key')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '使用 OAuth 连接' })).toBeVisible()
})

test('mcp oauth connect round-trips to the settings banner', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/apps')

  const notion = page.getByTestId('mcp-server-notion')
  await notion.getByRole('button', { name: '添加' }).click()
  // Add on OAuth MCP opens authorize immediately
  await page.waitForURL(/mcpResult=linked/)
  await expect(page.getByText('应用已连接。')).toBeVisible()
  await expect(page).toHaveURL(/\/settings\/apps$/)
})
