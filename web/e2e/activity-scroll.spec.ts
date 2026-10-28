import { test, expect, openPage } from './fixtures'
import { movie, theater } from './data.mjs'

for (const privateFeed of [false, true]) {
  const path = privateFeed
    ? '/compte/activite'
    : `/cinema/${theater.slug}?view=activity`
  const endpoint = privateFeed
    ? '/api/v1/account/activity'
    : `/api/v1/theaters/${theater.slug}/activity`
  const signal = privateFeed ? '#account-activity' : '#cinema-activity'

  test(`${privateFeed ? 'account' : 'cinema'} activity restores loaded continuation and scroll on Back and Forward`, async ({
    page,
    request,
  }) => {
    await request.post('/__playwright/scenario', {
      data: {
        enabled: privateFeed,
        state: privateFeed ? 'complete' : 'anonymous',
        theater_ids: [theater.id],
        feed: 'scroll',
        publicFeed: 'scroll',
      },
    })
    const rows = page.locator('main [data-event-id]')
    await openPage(page, '/cinemas')
    if (privateFeed) {
      await page.locator('header a[href="/compte"]').click()
      await page
        .getByRole('navigation', { name: 'Rubriques du compte' })
        .getByRole('link', { name: 'Activité', exact: true })
        .click()
    } else {
      await page
        .locator(`main a[href="/cinema/${theater.slug}"]`)
        .first()
        .click()
      await page
        .getByRole('navigation', { name: 'Vue de la programmation' })
        .getByRole('link', { name: 'Activité', exact: true })
        .click()
    }
    await expect(page).toHaveURL(path)
    await expect(rows).toHaveCount(20)
    await page
      .getByRole('button', { name: 'Afficher plus', exact: true })
      .click()
    await expect(rows).toHaveCount(40)
    const lastMovie = rows
      .last()
      .getByRole('link', { name: movie.title, exact: true })
      .first()
    await lastMovie.scrollIntoViewIfNeeded()
    const saved = await page.evaluate(() => window.scrollY)
    expect(saved).toBeGreaterThan(1000)
    await lastMovie.click()
    await expect(page).toHaveURL(/\/film\/film-playwright/)

    let release!: () => void
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route(`**${endpoint}*`, async (route) => {
      await held
      await route.continue()
    })
    try {
      await page.goBack()
      await expect(page).toHaveURL(path)
      await expect(page.locator(signal)).toHaveAttribute('aria-busy', 'true')
    } finally {
      release()
    }
    await expect(rows).toHaveCount(40)
    await expect(page.locator(signal)).toHaveAttribute('aria-busy', 'false')
    await expect
      .poll(async () =>
        Math.abs((await page.evaluate(() => window.scrollY)) - saved),
      )
      .toBeLessThanOrEqual(5)

    // A second continuation remains usable after restoring the previous walk.
    await page
      .getByRole('button', { name: 'Afficher plus', exact: true })
      .click()
    await expect(rows).toHaveCount(45)
    await rows.last().scrollIntoViewIfNeeded()
    const forwardSaved = await page.evaluate(() => window.scrollY)
    await page.goBack()
    await expect(page).not.toHaveURL(path)
    await page.goForward()
    await expect(page).toHaveURL(path)
    await expect(rows).toHaveCount(45)
    await expect
      .poll(async () =>
        Math.abs((await page.evaluate(() => window.scrollY)) - forwardSaved),
      )
      .toBeLessThanOrEqual(5)
  })
}

test('personalized city catalog restores its bottom and query after movie detail', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: {
      enabled: true,
      state: 'complete',
      scrollCatalog: 'long',
      discovery: 'populated',
    },
  })
  const path = '/ville/lille/cinemas?q=Film&page=2&sort=title_asc'
  await openPage(page, path)
  const cards = page.locator('main ul a[href^="/film/"]')
  await expect(cards).toHaveCount(24)
  await cards.last().scrollIntoViewIfNeeded()
  const saved = await page.evaluate(() => window.scrollY)
  expect(saved).toBeGreaterThan(1000)
  await cards.last().click()
  await expect(page).toHaveURL(/\/film\/film-playwright/)
  await page.goBack()
  await expect(page).toHaveURL(path)
  await expect(cards).toHaveCount(24)
  await expect(page.locator('#city-catalog')).toHaveAttribute(
    'aria-busy',
    'false',
  )
  await expect
    .poll(async () =>
      Math.abs((await page.evaluate(() => window.scrollY)) - saved),
    )
    .toBeLessThanOrEqual(5)
})
