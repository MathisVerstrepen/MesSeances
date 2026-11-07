import { mkdir, readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import process from 'node:process'
import { chromium } from '@playwright/test'

// Static, local-only artwork: no application server, API or external fonts.
const asset = async (path, mime) =>
  `data:${mime};base64,${(await readFile(new URL(path, import.meta.url))).toString('base64')}`
const logo = await asset(
  '../app/assets/imgs/imax_logo_large.webp',
  'image/webp',
)
const font = await asset(
  '../node_modules/@fontsource-variable/noto-sans/files/noto-sans-latin-wght-normal.woff2',
  'font/woff2',
)
const output = new URL('../public/images/social/', import.meta.url)
await mkdir(output, { recursive: true })
const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN,
})

try {
  for (const variant of ['imax', 'articles']) {
    const imax = variant === 'imax'
    const page = await browser.newPage({
      viewport: { width: 1200, height: 630 },
      deviceScaleFactor: 1,
    })
    await page.route('**/*', (route) => route.abort())
    await page.setContent(`<!doctype html><html lang="fr"><head><meta charset="utf-8">
      <style>
        @font-face { font-family: Noto; src: url('${font}'); font-weight: 100 900; }
        * { box-sizing: border-box; }
        body { margin: 0; width: 1200px; height: 630px; background: #f8f7f2; color: #27272a; font-family: Noto, sans-serif; }
        .card { display: flex; min-height: 630px; align-items: center; justify-content: center; padding: 64px; text-align: center; }
        .logo { display: block; width: 560px; height: auto; margin-inline: auto; }
        h1 { margin: 44px 0 0; font-size: 112px; line-height: 1.08; letter-spacing: -5px; font-weight: 900; }
        .index-title { margin: 0; font-size: 144px; line-height: 1.05; }
        .index-title span { color: #1f6f78; }
      </style></head><body><div class="card">
        <div>${imax ? `<img class="logo" src="${logo}" alt="IMAX"><h1>le guide complet</h1>` : '<h1 class="index-title">Articles<br><span>cinéma</span></h1>'}</div>
      </div></body></html>`)
    await page.evaluate(() => document.fonts.ready)
    await page
      .locator('img')
      .evaluateAll((images) =>
        Promise.all(images.map((image) => image.decode())),
      )
    const name = imax ? 'imax-guide-v2.jpg' : 'articles-v2.jpg'
    await page.screenshot({
      path: fileURLToPath(new URL(name, output)),
      type: 'jpeg',
      quality: 92,
    })
    await page.close()
    process.stdout.write(`Created ${name} (1200 × 630)\n`)
  }
} finally {
  await browser.close()
}
