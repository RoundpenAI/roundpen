import { test, type Page } from '@playwright/test'

export const live = Boolean(process.env.PLAYWRIGHT_BASE_URL)

export const ADMIN = {
  user: process.env.E2E_USER || 'admin',
  password: process.env.E2E_PASSWORD || (live ? '' : 'adminadmin'),
}

export function skipIfNoLivePassword() {
  test.skip(live && !ADMIN.password, 'set E2E_PASSWORD to log into the running make dev admin')
}

export async function loginViaUi(page: Page) {
  await page.goto('/login')
  await page.getByLabel('Username or email').fill(ADMIN.user)
  await page.getByLabel('Password').fill(ADMIN.password)
  await page.getByRole('button', { name: 'Sign in' }).click()
}

export async function loginViaApi(page: Page) {
  const res = await page.request.post('/v1/auth/login', {
    data: { user: ADMIN.user, password: ADMIN.password },
  })
  if (!res.ok()) {
    throw new Error(`login: ${res.status()} ${await res.text()}`)
  }
}

export async function resetSmoke(page: Page) {
  const res = await page.request.post('/v1/test/reset')
  if (res.status() === 404) return
  if (!res.ok()) {
    throw new Error(`reset: ${res.status()} ${await res.text()}`)
  }
}

export async function hasSmokeHooks(page: Page): Promise<boolean> {
  const res = await page.request.put('/v1/test/ensure-error', { data: { error: '' } })
  if (!res.ok()) return false
  try {
    const body = (await res.json()) as { ok?: boolean }
    return body.ok === true
  } catch {
    return false
  }
}

export async function desktopWsUrl(page: Page): Promise<string> {
  await loginViaApi(page)
  const ensure = await page.request.post('/v1/me/environments/browser/ensure')
  if (!ensure.ok()) {
    throw new Error(`ensure: ${ensure.status()} ${await ensure.text()}`)
  }
  const link = await page.request.get('/v1/me/environments/browser/desktop')
  if (!link.ok()) {
    throw new Error(`desktop: ${link.status()} ${await link.text()}`)
  }
  const body = (await link.json()) as { wsUrl: string }
  if (!body.wsUrl) {
    throw new Error('desktop link missing wsUrl')
  }
  return body.wsUrl
}
