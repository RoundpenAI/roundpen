import type { WebSocketRoute } from '@playwright/test'
import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

type Frame = { type?: string; name?: string; args?: string; text?: string }

const SESSION_PATH = '/a/as-seed/s/sess-seed'

// The uismoke stub serves the catalog and the transcript (including a /clear
// marker row); the agent socket is mocked so the protocol can be asserted
// without a live agent.
test('slash menu sends a command frame and renders the clear divider', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)

  const frames: Frame[] = []
  let socket: WebSocketRoute | null = null
  await page.routeWebSocket('**/v1/agent-sessions/*/ws', (ws) => {
    socket = ws
    ws.onMessage((message) => {
      const text = typeof message === 'string' ? message : message.toString()
      try {
        frames.push(JSON.parse(text) as Frame)
      } catch {
        /* ignore non-JSON frames */
      }
    })
    ws.send(JSON.stringify({ type: 'hello', message: 'ok' }))
    ws.send(JSON.stringify({ type: 'status', busy: false }))
  })

  await page.goto(SESSION_PATH)
  const editor = page.locator('.chat-composer-dock [contenteditable="true"]')
  await expect(editor).toBeVisible()

  // "/" opens the catalog menu (Semi only triggers it on an empty editor).
  await editor.click()
  await page.keyboard.press('/')
  await expect(page.getByText('/review')).toBeVisible()

  await page.getByText('/review').click()
  await page.keyboard.type('关注并发')
  await page.keyboard.press('Enter')

  await expect
    .poll(() => frames.find((f) => f.type === 'command'))
    .toMatchObject({ name: 'review', args: '关注并发' })

  // The bubble shows what was typed, not the expanded instructions.
  await expect(page.getByText('/review 关注并发')).toBeVisible()

  // The composer hands its text over to the session: nothing stays behind
  // while the answer is already being generated.
  await expect(editor).toHaveText('')

  // /clear only re-syncs: the transcript survives and gains a divider.
  expect(socket).not.toBeNull()
  const refetch = page.waitForRequest((req) => req.url().includes('/messages'))
  socket!.send(JSON.stringify({ type: 'cleared' }))
  await refetch
  await expect(page.getByText('上下文已清空')).toBeVisible()
  await expect(page.getByText('把超时改成 60 秒。')).toBeVisible()
})
