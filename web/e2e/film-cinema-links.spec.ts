import { test, expect, openPage } from './fixtures'
import { movie, theater } from './data.mjs'

test('film headings separate the chain film link from the cinema link', async ({
  page,
}, testInfo) => {
  await openPage(page, `/film/${movie.slug}`)
  const heading = page.locator(`#theater-${theater.id}`)
  const logo = heading.getByRole('link', {
    name: `Voir ${movie.title} sur UGC`,
  })
  const cinema = heading.getByRole('link', { name: theater.name, exact: true })
  await expect(logo).toHaveAttribute(
    'href',
    'https://www.ugc.fr/film.html?id=17747',
  )
  await expect(logo.locator('img')).toHaveCount(1)
  await expect(cinema).toHaveAttribute('href', `/cinema/${theater.slug}`)
  await expect(heading.locator('a a')).toHaveCount(0)
  const logoBounds = await logo.boundingBox()
  const nameBounds = await cinema.boundingBox()
  expect(
    nameBounds!.x - logoBounds!.x - logoBounds!.width,
  ).toBeGreaterThanOrEqual(6)
  await logo.focus()
  await logo.press('Tab')
  await expect(cinema).toBeFocused()
  // Other fixture cinemas have no source film URL: the name remains navigable.
  const fallback = page.locator('#theater-fixture-second')
  await expect(fallback.getByRole('link')).toHaveCount(1)
  await expect(fallback.locator('img')).toHaveCount(1)
  await page.screenshot({
    path: testInfo.outputPath('film-cinema-links.png'),
    fullPage: true,
  })
  await cinema.click()
  await expect(page).toHaveURL(new RegExp(`/cinema/${theater.slug}$`))
  await expect(
    page.getByRole('heading', { name: new RegExp(theater.name) }),
  ).toBeVisible()
})
