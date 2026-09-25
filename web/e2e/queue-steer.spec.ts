import type { WebSocketRoute } from '@playwright/test'
import { expect, test } from './fixtures'
import { loginViaApi, skipIfNoLivePassword } from './helpers'

type Frame = {
  type?: string
  text?: string
  id?: string
  clientMsgId?: string
}

const SESSION_PATH = '/a/as-seed/s/sess-seed'

// The agent socket is mocked: the runner's queue/steer protocol is asserted
// without a live agent, and the UI is driven through the busy state.
test('busy composer steers, queues on shift, and pulls queued messages back', async ({ page }) => {
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
    // Busy from the start so the mid-turn send key is on screen.
    ws.send(JSON.stringify({ type: 'status', busy: true, steer: true, queueVersion: 0 }))
  })

  await page.goto(SESSION_PATH)
  const editor = page.locator('.chat-composer-dock [contenteditable="true"]')
  await expect(editor).toBeVisible()

  // Click = steer into the running turn.
  await editor.click()
  await page.keyboard.type('插话一句')
  await page.locator('.chat-midturn-send').click()
  await expect
    .poll(() => frames.find((f) => f.type === 'steer'))
    .toMatchObject({ text: '插话一句' })
  const steerFrame = frames.find((f) => f.type === 'steer')
  expect(steerFrame?.clientMsgId).toBeTruthy()

  // Shift+click = queue instead.
  await editor.click()
  await page.keyboard.type('排队一句')
  await page.locator('.chat-midturn-send').click({ modifiers: ['Shift'] })
  await expect
    .poll(() => frames.find((f) => f.type === 'prompt'))
    .toMatchObject({ text: '排队一句' })
  const queuedFrame = frames.find((f) => f.type === 'prompt')

  // The server acknowledges with a queue snapshot carrying the row id; the
  // optimistic bubble then shows the queued badge and a pull-back action.
  await expect.poll(() => socket !== null).toBe(true)
  socket!.send(
    JSON.stringify({
      type: 'queue',
      steer: true,
      version: 1,
      items: [{ id: 'db-queued-1', text: '排队一句', clientMsgId: queuedFrame?.clientMsgId }],
    }),
  )
  await expect(page.getByText('排队中')).toBeVisible()
  await expect(page.getByText('已排队 1 条')).toBeVisible()

  // Pull it back: the client sends unqueue with the persisted row id.
  await page.getByRole('button', { name: '撤回' }).click()
  await expect.poll(() => frames.find((f) => f.type === 'unqueue')).toMatchObject({ id: 'db-queued-1' })

  // The server confirms with an empty snapshot; the bubble flips to withdrawn.
  socket!.send(JSON.stringify({ type: 'queue', steer: true, version: 2, items: [] }))
  await expect(page.getByText('已撤回')).toBeVisible()
})

test('a server-queued message arriving after reconnect shows the badge', async ({ page }) => {
  skipIfNoLivePassword()
  await loginViaApi(page)

  // The history row comes from the stub transcript; queue it via the status
  // snapshot to prove a reconnecting client rebuilds queue UI from the server.
  const res = await page.request.get('/v1/agent-sessions/sess-seed/messages')
  const body = (await res.json()) as { messages?: { id: string; role: string; content: string }[] }
  const userRow = (body.messages ?? []).find((m) => m.role === 'user')
  test.skip(!userRow, 'stub transcript has no user row to queue')

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
    // Idle first so the transcript loads; the queue snapshot follows like a
    // reconnect into a session with a running turn and queued messages.
    ws.send(JSON.stringify({ type: 'status', busy: false, steer: false, queueVersion: 0 }))
  })

  await page.goto(SESSION_PATH)
  await expect.poll(() => socket !== null).toBe(true)
  await expect(page.getByText(userRow!.content, { exact: false })).toBeVisible()

  socket!.send(
    JSON.stringify({
      type: 'status',
      busy: true,
      steer: false,
      queueVersion: 5,
      pending: [{ id: userRow!.id, text: userRow!.content }],
    }),
  )
  await expect(page.getByText('排队中')).toBeVisible()

  // steer=false arrived with the snapshot: the send key must queue via a
  // prompt frame instead of promising mid-turn injection.
  const editor = page.locator('.chat-composer-dock [contenteditable="true"]')
  await editor.click()
  await page.keyboard.type('只能排队')
  await page.locator('.chat-midturn-send').click()

  await expect.poll(() => frames.find((f) => f.type === 'prompt')).toMatchObject({ text: '只能排队' })
  expect(frames.find((f) => f.type === 'steer')).toBeUndefined()
})
