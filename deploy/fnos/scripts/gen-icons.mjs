// Renders web/public/favicon.svg into the fnOS app icons (64 / 256 px PNG).
// Playwright is borrowed from web/node_modules, so run it from the repo root:
//   node deploy/fnos/scripts/gen-icons.mjs
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '../../../web/node_modules/playwright/index.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const repo = path.resolve(here, '../../..')
const svg = await readFile(path.join(repo, 'web/public/favicon.svg'), 'utf8')

const targets = [
  [64, 'ICON.PNG'],
  [256, 'ICON_256.PNG'],
  [64, 'app/ui/images/icon_64.png'],
  [256, 'app/ui/images/icon_256.png'],
]

const browser = await chromium.launch()
for (const [size, rel] of targets) {
  const page = await browser.newPage({
    viewport: { width: size, height: size },
    deviceScaleFactor: 1,
  })
  await page.setContent(
    `<!doctype html><style>html,body{margin:0;width:${size}px;height:${size}px}svg{width:${size}px;height:${size}px;display:block}</style>${svg}`,
  )
  const out = path.join(here, '..', rel)
  await page.screenshot({ path: out, type: 'png' })
  await page.close()
  console.log(`${rel} (${size}x${size})`)
}
await browser.close()
