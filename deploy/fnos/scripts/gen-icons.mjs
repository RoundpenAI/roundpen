// Renders web/public/favicon.svg into the fnOS app icons (64 / 256 px PNG).
// The Playwright client comes from web/node_modules; the browser is the remote
// browserless at browserless.eaxi.com (no local browser install). Run from the
// repo root:
//   node deploy/fnos/scripts/gen-icons.mjs
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '../../../web/node_modules/playwright/index.mjs'

const browserWS = process.env.ROUNDPEN_BROWSERLESS_WS
  ?? 'wss://browserless.eaxi.com/chrome/playwright'

const here = path.dirname(fileURLToPath(import.meta.url))
const repo = path.resolve(here, '../../..')
const svg = await readFile(path.join(repo, 'web/public/favicon.svg'), 'utf8')

const targets = [
  [64, 'ICON.PNG'],
  [256, 'ICON_256.PNG'],
  [64, 'app/ui/images/icon_64.png'],
  [256, 'app/ui/images/icon_256.png'],
]

const browser = await chromium.connect(browserWS)
for (const [size, rel] of targets) {
  const page = await browser.newPage({
    viewport: { width: size, height: size },
    deviceScaleFactor: 1,
  })
  await page.setContent(
    `<!doctype html><style>html,body{margin:0;width:${size}px;height:${size}px;background:transparent}svg{width:${size}px;height:${size}px;display:block}</style>${svg}`,
  )
  const out = path.join(here, '..', rel)
  // omitBackground keeps the rounded-rect corners transparent. An opaque white
  // corner shows through fnOS's tighter icon mask as light leak.
  await page.screenshot({ path: out, type: 'png', omitBackground: true })
  await page.close()
  console.log(`${rel} (${size}x${size})`)
}
await browser.close()
