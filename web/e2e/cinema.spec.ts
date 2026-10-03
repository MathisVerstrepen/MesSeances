import { test, expect, openPage } from './fixtures'
import { date, movie, showtimes, theater } from './data.mjs'

const path = `/cinema/${theater.slug}?date=${date}`

test('cinema identity and programme render on the server', async ({
  request,
}) => {
  const response = await request.get(path)
  expect(response.status()).toBe(200)
  const html = await response.text()
  expect(html).toContain(theater.name)
  expect(html).toContain(theater.address)
  expect(html).toContain(movie.title)
  expect(html).toContain(
    `/statistiques?period=all&amp;theater=${encodeURIComponent(theater.id)}`,
  )
})

test('cinema is usable without horizontal overflow', async ({
  page,
}, testInfo) => {
  await openPage(page, path)
  await expect(page.getByRole('heading', { level: 1 })).toContainText(
    theater.name,
  )
  await expect(
    page.getByRole('heading', { name: 'Séances', exact: true }),
  ).toBeVisible()
  await expect(page.getByText(theater.address, { exact: true })).toBeVisible()
  const header = page.locator('main > header')
  const hero = header.locator(':scope > div > div').first()
  expect((await hero.boundingBox())!.height).toBeLessThanOrEqual(
    testInfo.project.name === 'mobile' ? 220 : 320,
  )
  const statistics = header.getByRole('link', {
    name: 'Statistiques',
    exact: true,
  })
  await expect(statistics).toBeVisible()
  await expect(statistics).toHaveAttribute(
    'href',
    `/statistiques?period=all&theater=${encodeURIComponent(theater.id)}`,
  )
  await expect(header.locator('dl + a')).toHaveAccessibleName('Statistiques')
  const sidebar = await statistics.locator('..').boundingBox()
  const button = await statistics.boundingBox()
  expect(sidebar).not.toBeNull()
  expect(button).not.toBeNull()
  expect(button!.width).toBeGreaterThanOrEqual(44)
  expect(button!.height).toBeGreaterThanOrEqual(44)
  expect(
    sidebar!.y + sidebar!.height - (button!.y + button!.height),
  ).toBeLessThanOrEqual(24)
  await expect(
    page.getByText(movie.title, { exact: true }).first(),
  ).toBeVisible()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy()
  await page.screenshot({
    path: testInfo.outputPath('cinema.png'),
    fullPage: true,
  })
})

for (const imageLoads of [true, false]) {
  test(`cinema ${imageLoads ? 'photo layout' : 'failed photo fallback'} preserves long identity`, async ({
    page,
  }, testInfo) => {
    await openPage(page, path)
    const longName =
      'Cinéma des Grandes Salles et des Rencontres Internationales'
    const imageUrl = '/api/v1/theaters/ugc/fixture-cinema/image/1'
    await page.route(`**${imageUrl}`, (route) =>
      imageLoads
        ? route.fulfill({
            contentType: 'image/svg+xml',
            body: '<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="800"><rect width="1200" height="800" fill="#54636e"/></svg>',
          })
        : route.abort(),
    )
    await page.route(
      `**/api/v1/theaters/${theater.slug}/showtimes*`,
      (route) => {
        const payload = showtimes()
        return route.fulfill({
          json: {
            ...payload,
            theater: {
              ...payload.theater,
              name: longName,
              image: { url: imageUrl, width: 1200, height: 800 },
            },
          },
        })
      },
    )
    // Tomorrow triggers a client fetch; browser interception cannot mock SSR.
    await page.setViewportSize({ width: 390, height: 844 })
    await page.getByRole('tab', { name: 'Demain', exact: true }).click()
    await page.setViewportSize(testInfo.project.use.viewport!)
    const header = page.locator('main > header')
    await expect(header.getByRole('heading', { level: 1 })).toContainText(
      longName,
    )
    const photo = header.locator('img[fetchpriority="high"]')
    if (imageLoads) {
      await expect(photo).toBeVisible()
      await expect(photo).toHaveJSProperty('complete', true)
      expect(
        await photo.evaluate((image) =>
          image instanceof HTMLImageElement ? image.naturalWidth : 0,
        ),
      ).toBeGreaterThan(0)
    } else {
      await expect(photo).toHaveCount(0)
    }
    await expect(
      header.getByRole('link', { name: 'Statistiques', exact: true }),
    ).toHaveAttribute(
      'href',
      `/statistiques?period=all&theater=${encodeURIComponent(theater.id)}`,
    )
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBeTruthy()
    await page.screenshot({
      path: testInfo.outputPath(
        imageLoads ? 'cinema-photo.png' : 'cinema-fallback.png',
      ),
      fullPage: true,
    })
  })
}

test('films tab, search and browser history preserve the programme', async ({
  page,
}) => {
  await openPage(page, path)
  await page
    .getByRole('navigation', { name: 'Vue de la programmation' })
    .getByRole('link', { name: 'Films', exact: true })
    .click()
  await expect(page).toHaveURL(/view=films/)
  await expect(
    page.getByRole('heading', { name: 'Films', exact: true }),
  ).toBeVisible()
  const search = page.getByRole('searchbox', { name: 'Rechercher un film' })
  await search.fill('absent')
  await search.press('Enter')
  await expect(page).toHaveURL(/q=absent/)
  await expect(
    page.getByRole('heading', { name: movie.title, level: 3, exact: true }),
  ).toHaveCount(0)
  await page.goBack()
  await expect(
    page.getByRole('heading', { name: 'Séances', exact: true }),
  ).toBeVisible()
  await page.goForward()
  await expect(page).toHaveURL(/q=absent/)
  await expect(search).toHaveValue('absent')
  await search.fill('')
  await search.press('Enter')
  await expect(page).not.toHaveURL(/q=absent/)
  await expect(
    page
      .getByRole('heading', { name: movie.title, level: 3, exact: true })
      .first(),
  ).toBeVisible()
})

test('missing cinema returns an SSR 404 and useful page', async ({ page }) => {
  const response = await openPage(page, '/cinema/missing')
  expect(response?.status()).toBe(404)
  await expect(
    page.getByRole('heading', { name: 'Cinéma introuvable', exact: true }),
  ).toBeVisible()
})
