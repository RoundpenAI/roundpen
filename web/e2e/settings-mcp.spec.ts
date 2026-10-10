import { expect, test } from './fixtures'
import { loginViaApi, resetSmoke, skipIfNoLivePassword } from './helpers'

test('mcp settings connects an API key server and manages the connection', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/mcp')

  await expect(page.getByText('MCP 服务器')).toBeVisible()
  await expect(page.getByTestId('mcp-server-notion')).toBeVisible()
  await expect(page.getByTestId('mcp-server-exa')).toBeVisible()
  await expect(page.getByTestId('mcp-server-linear')).toBeVisible()

  // Exa is key-only: its inline key form is the only connect path.
  const exa = page.getByTestId('mcp-server-exa')
  await expect(exa.getByRole('button', { name: '使用 OAuth 连接' })).toHaveCount(0)
  await exa.getByPlaceholder('粘贴 API key').fill('demo-key')
  await exa.getByRole('button', { name: '保存 key' }).click()

  const conn = page.getByTestId('mcp-conn-exa')
  await expect(conn).toBeVisible()
  await expect(conn).toContainText('2 个工具')

  await conn.getByRole('button', { name: '刷新工具' }).click()
  await expect(page.getByText('工具列表已刷新（3 个工具）。')).toBeVisible()
  await expect(conn).toContainText('3 个工具')

  await conn.getByRole('button', { name: '断开连接' }).click()
  await expect(page.getByText('已断开连接。')).toBeVisible()
  await expect(page.getByTestId('mcp-conn-exa')).toHaveCount(0)
})

test('mcp settings rejects a bad API key', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/mcp')

  const exa = page.getByTestId('mcp-server-exa')
  await exa.getByPlaceholder('粘贴 API key').fill('bad')
  await exa.getByRole('button', { name: '保存 key' }).click()

  await expect(page.getByText(/authentication with the MCP server failed/)).toBeVisible()
  await expect(page.getByTestId('mcp-conn-exa')).toHaveCount(0)
})

test('mcp dual-auth server switches between OAuth and API key', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/mcp')

  const linear = page.getByTestId('mcp-server-linear')
  await expect(linear.getByRole('button', { name: '使用 OAuth 连接' })).toBeVisible()
  await expect(linear.getByPlaceholder('粘贴 API key')).toHaveCount(0)

  await linear.getByRole('button', { name: '改用 API key' }).click()
  await expect(linear.getByPlaceholder('粘贴 API key')).toBeVisible()

  await linear.getByRole('button', { name: '改用 OAuth' }).click()
  await expect(linear.getByPlaceholder('粘贴 API key')).toHaveCount(0)
  await expect(linear.getByRole('button', { name: '使用 OAuth 连接' })).toBeVisible()
})

test('mcp oauth connect round-trips to the settings banner', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)
  await resetSmoke(page)
  await page.goto('/settings/mcp')

  await page.getByTestId('mcp-server-notion').getByRole('button', { name: '使用 OAuth 连接' }).click()

  // The smoke authorize stub bounces straight back with the callback params.
  await page.waitForURL(/mcpResult=linked/)
  await expect(page.getByText('MCP 服务器已连接。')).toBeVisible()
  // The callback params are cleaned from the URL after the banner shows.
  await expect(page).toHaveURL(/\/settings\/mcp$/)
})
