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
  await expect(logo).toHaveAttribute('target', '_blank')
  await expect(logo).toHaveAttribute('rel', 'noopener noreferrer')
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
  // Fulfil the destination locally so this verifies a real new tab without
  // contacting the provider or relaxing the fixture's external-request guard.
  await page.context().route('https://www.ugc.fr/film.html?id=17747', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<title>Film UGC</title>',
    }),
  )
  const popupPromise = page.waitForEvent('popup')
  await logo.click()
  const popup = await popupPromise
  await expect(popup).toHaveURL('https://www.ugc.fr/film.html?id=17747')
  await expect(page).toHaveURL(new RegExp(`/film/${movie.slug}$`))
  await popup.close()
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
