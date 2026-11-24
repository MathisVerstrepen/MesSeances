import type { Locator } from '@playwright/test'
import { longTitleMovie } from './data.mjs'
import { expect, openPage, test } from './fixtures'

function titleLayout(heading: Locator) {
  return heading.evaluate((element) => {
    const { fontSize, lineHeight } = getComputedStyle(element)
    return {
      fontSize: Number.parseFloat(fontSize),
      lines: Math.round(
        element.getBoundingClientRect().height / Number.parseFloat(lineHeight),
      ),
    }
  })
}

test('film titles keep their default size when possible and fit on two lines', async ({
  page,
}, testInfo) => {
  const viewport = page.viewportSize()!
  const defaultSize = viewport.width < 640 ? 48 : 100.8
  const heading = page.getByRole('heading', { level: 1 })

  await openPage(page, '/film/film-0')
  await expect(heading).toHaveText('Alpha')
  await expect(heading).toHaveCSS('font-size', `${defaultSize}px`)

  await openPage(page, `/film/${longTitleMovie.slug}`)
  await expect(heading).toHaveText(longTitleMovie.title)
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  await expect.poll(async () => (await titleLayout(heading)).lines).toBe(2)
  const initial = await titleLayout(heading)
  expect(initial.fontSize).toBeLessThan(defaultSize)
  await page.locator('.movie-hero').screenshot({
    path: testInfo.outputPath('film-title.png'),
  })

  await page.setViewportSize({ ...viewport, width: 320 })
  await expect
    .poll(async () => (await titleLayout(heading)).fontSize)
    .toBeLessThan(initial.fontSize)
  expect((await titleLayout(heading)).lines).toBe(2)

  await page.setViewportSize(viewport)
  await expect.poll(() => titleLayout(heading)).toEqual(initial)
})
