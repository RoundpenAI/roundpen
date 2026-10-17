import type { Page } from '@playwright/test'

export type ChatActionFrame = {
  type?: string
  text?: string
  name?: string
  args?: string
  id?: string
  clientMsgId?: string
  enabled?: boolean
}

declare global {
  interface Window {
    __rpSsePush?: (json: string) => void
    __rpSseClose?: () => void
  }
}

/**
 * Mock chat SSE + actions for Playwright. Initial frames are pushed on
 * connect; call pushChatFrame() to send more server→client events later.
 */
export async function installChatStreamMock(
  page: Page,
  opts: {
    initialFrames: Record<string, unknown>[]
    onAction: (frame: ChatActionFrame) => void
  },
) {
  const initial = opts.initialFrames
  await page.addInitScript((frames) => {
    const origFetch = window.fetch.bind(window)
    let controller: ReadableStreamDefaultController<Uint8Array> | null = null
    const enc = new TextEncoder()
    window.__rpSsePush = (json: string) => {
      controller?.enqueue(enc.encode(`data: ${json}\n\n`))
    }
    window.__rpSseClose = () => {
      try {
        controller?.close()
      } catch {
        /* ignore */
      }
    }
    window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url)
      if (url.includes('/v1/agent-sessions/') && url.includes('/events')) {
        const stream = new ReadableStream<Uint8Array>({
          start(c) {
            controller = c
            for (const f of frames as Record<string, unknown>[]) {
              c.enqueue(enc.encode(`data: ${JSON.stringify(f)}\n\n`))
            }
          },
        })
        return new Response(stream, {
          status: 200,
          headers: {
            'content-type': 'text/event-stream; charset=utf-8',
            'cache-control': 'no-cache',
          },
        })
      }
      return origFetch(input, init)
    }
  }, initial)

  await page.route('**/v1/agent-sessions/*/actions', async (route) => {
    try {
      opts.onAction(JSON.parse(route.request().postData() || '{}') as ChatActionFrame)
    } catch {
      /* ignore */
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: '{"ok":true}',
    })
  })
}

export async function pushChatFrame(page: Page, frame: Record<string, unknown>) {
  await page.evaluate((f) => {
    window.__rpSsePush?.(JSON.stringify(f))
  }, frame)
}
