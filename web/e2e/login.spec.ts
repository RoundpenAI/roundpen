import { expect, test } from '@playwright/test'
import { ADMIN, skipIfNoLivePassword } from './helpers'

test('login page renders', async ({ page }) => {
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Roundpen' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
})

test('login failure surfaces API error field', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('Password').fill('wrong-password')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('alert')).toHaveText('invalid user or password')
})

test('successful login reaches chats', async ({ page }) => {
  skipIfNoLivePassword()
  await page.goto('/login')
  await page.getByLabel('Password').fill(ADMIN.password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('heading', { name: 'What are we working on?' })).toBeVisible()
})
