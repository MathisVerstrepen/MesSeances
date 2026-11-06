import { test, expect, openPage } from './fixtures'
import { movie, generatedAt } from './data.mjs'

test('film catalog excludes the coming Wednesday from Monday presales', async ({
  page,
}, testInfo) => {
  await page.clock.setFixedTime(new Date('2026-11-09T12:00:00Z'))
  const items = [
    {
      ...movie,
      slug: 'presale',
      title: 'Film en prévente',
      french_release_date: '2026-11-18',
      showtime_count: 8,
    },
    {
      ...movie,
      slug: 'released',
      title: 'Sortie ce mercredi',
      french_release_date: '2026-11-11',
      showtime_count: 4,
    },
    {
      ...movie,
      slug: 'unscheduled',
      title: 'Film sans séance',
      french_release_date: '2026-11-18',
      showtime_count: 0,
    },
  ]
  await page.route(/\/api\/v1\/movies(?:\?|$)/, (route) =>
    route.fulfill({
      json: {
        items,
        available_genres: [],
        page: 1,
        page_size: 24,
        total: items.length,
        generated_at: generatedAt,
        catalog_revision: 'presale-fixture',
      },
    }),
  )
  await openPage(page, '/films')
  const presale = page.locator('[data-catalog-movie="presale"]')
  await expect(presale.getByText('Prévente', { exact: true })).toBeVisible()
  await expect(presale).toHaveAttribute('href', '/film/presale')
  await expect(
    page
      .locator('[data-catalog-movie="released"]')
      .getByText('Prévente', { exact: true }),
  ).toHaveCount(0)
  await expect(
    page
      .locator('[data-catalog-movie="unscheduled"]')
      .getByText('Prévente', { exact: true }),
  ).toHaveCount(0)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy()
  await page.screenshot({
    path: testInfo.outputPath('films-presale.png'),
    fullPage: true,
  })
})
