import { expect, test } from './fixtures'
import { ADMIN, skipIfNoLivePassword } from './helpers'

test('login page renders', async ({ page }) => {
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Roundpen' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
})

test('login failure surfaces API error field', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('Password', { exact: true }).fill('wrong-password')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('alert').first()).toContainText('invalid user or password')
})

test('successful login reaches assistants', async ({ page }) => {
  skipIfNoLivePassword()
  await page.goto('/login')
  await page.getByLabel('Password', { exact: true }).fill(ADMIN.password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByText('新建助手').first()).toBeVisible({ timeout: 30_000 })
})
