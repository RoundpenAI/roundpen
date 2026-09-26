import type { WebSocketRoute } from '@playwright/test'
import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

type Frame = { type?: string; text?: string }

const SESSION_PATH = '/a/as-seed/s/sess-seed'

// Semi's own send key only clears the composer when the generating prop flips,
// and the console turns that off so a mid-turn send cannot wipe text typed
// after it. The hand-off therefore has to empty the box itself, or the sent
// text stays in the input while the session works on it.
test('sending with the send key empties the composer', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)

  const frames: Frame[] = []
  await page.routeWebSocket('**/v1/agent-sessions/*/ws', (ws: WebSocketRoute) => {
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

  await editor.click()
  await page.keyboard.type('再看下日志')
  await expect(editor).toHaveText('再看下日志')
  await page.keyboard.press('Enter')

  await expect
    .poll(() => frames.find((f) => f.type === 'prompt'))
    .toMatchObject({ text: '再看下日志' })
  await expect(editor).toHaveText('')
})
