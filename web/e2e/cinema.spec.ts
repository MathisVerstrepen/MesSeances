import type { Locator } from '@playwright/test'
import { test, expect, openPage } from './fixtures'
import { date, movie, showtimes, theater } from './data.mjs'

const path = `/cinema/${theater.slug}?date=${date}`

async function expectHeaderLayout(
  header: Locator,
  desktop: boolean,
  expectedAddress = theater.address,
) {
  await expect(header.getByText('Programmation', { exact: true })).toHaveCount(
    0,
  )
  await expect(header.getByText(/dates? disponibles?/)).toHaveCount(0)
  const statistics = header.getByRole('link', {
    name: 'Statistiques',
    exact: true,
  })
  await expect(statistics).toHaveCount(1)
  await expect(statistics).toBeVisible()
  await expect(statistics).toHaveAttribute(
    'href',
    `/statistiques?period=all&theater=${encodeURIComponent(theater.id)}`,
  )
  const heroElement = header.locator(':scope > div').first()
  const stripElement = header.locator(':scope > div').last()
  await expect(heroElement.locator('p:visible')).toContainText(
    desktop ? expectedAddress || theater.city : theater.city,
  )
  const hero = await heroElement.boundingBox()
  const button = await statistics.boundingBox()
  const follow = header.getByRole('button', {
    name: 'Suivre ce cinéma',
    exact: true,
  })
  const followBox = (await follow.count()) ? await follow.boundingBox() : null
  if (followBox) {
    expect(followBox.width).toBeGreaterThanOrEqual(44)
    expect(followBox.height).toBeGreaterThanOrEqual(44)
    expect(button!.x - followBox.x - followBox.width).toBeCloseTo(8, 0)
  }
  expect(hero).not.toBeNull()
  expect(button).not.toBeNull()
  expect(button!.width).toBeGreaterThanOrEqual(44)
  expect(button!.height).toBeGreaterThanOrEqual(44)
  if (desktop) {
    await expect(stripElement).toBeHidden()
    await statistics.focus()
    await header.page().keyboard.press('Shift+Tab')
    await header.page().keyboard.press('Tab')
    await expect(statistics).toBeFocused()
    expect(
      await statistics.evaluate((link) => getComputedStyle(link).outlineStyle),
    ).toBe('solid')
    const headerBox = await header.boundingBox()
    expect(headerBox!.height).toBeCloseTo(hero!.height + 4, 0)
    expect(hero!.x + hero!.width - button!.x - button!.width).toBeCloseTo(32, 0)
    expect(hero!.y + hero!.height - button!.y - button!.height).toBeCloseTo(
      32,
      0,
    )
    for (const text of [
      heroElement.locator('h1'),
      heroElement.locator('p:visible'),
    ]) {
      const box = await text.boundingBox()
      expect(box!.x + box!.width).toBeLessThanOrEqual(followBox?.x ?? button!.x)
    }
  } else {
    await expect(stripElement).toBeVisible()
    await expect(stripElement.locator('dd')).toContainText(
      expectedAddress || theater.city,
    )
    const strip = await stripElement.boundingBox()
    const address = await stripElement.locator('dd').boundingBox()
    expect(strip!.width).toBeCloseTo(hero!.width, 0)
    expect(strip!.y).toBeCloseTo(hero!.y + hero!.height, 0)
    expect(address!.x + address!.width).toBeLessThanOrEqual(
      followBox?.x ?? button!.x,
    )
    expect(strip!.x + strip!.width - button!.x - button!.width).toBeCloseTo(
      16,
      0,
    )
  }
}

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
  const header = page.locator('main > header')
  const hero = header.locator(':scope > div').first()
  expect((await hero.boundingBox())!.height).toBeLessThanOrEqual(
    testInfo.project.name === 'mobile' ? 220 : 320,
  )
  await expectHeaderLayout(header, testInfo.project.name === 'desktop')
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

for (const imageState of ['photo', 'missing', 'failed']) {
  test(`cinema ${imageState} hero preserves long identity`, async ({
    page,
    request,
  }, testInfo) => {
    await request.post('/__playwright/scenario', {
      data: { enabled: true, state: 'complete' },
    })
    await openPage(page, path)
    const imageLoads = imageState === 'photo'
    const longName =
      'Cinéma des Grandes Salles et des Rencontres Internationales'
    const longAddress = `12 rue des Rencontres ${'QuartierInternational'.repeat(5)}`
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
              address: longAddress,
              image:
                imageState === 'missing'
                  ? null
                  : { url: imageUrl, width: 1200, height: 800 },
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
    await expectHeaderLayout(
      header,
      testInfo.project.name === 'desktop',
      longAddress,
    )
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBeTruthy()
    await page.screenshot({
      path: testInfo.outputPath(`cinema-${imageState}.png`),
      fullPage: true,
    })
  })
}

test('cinema without an address keeps city identity', async ({
  page,
  request,
}, testInfo) => {
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'complete' },
  })
  await openPage(page, path)
  await page.route(`**/api/v1/theaters/${theater.slug}/showtimes*`, (route) => {
    const payload = showtimes()
    return route.fulfill({
      json: { ...payload, theater: { ...payload.theater, address: '' } },
    })
  })
  await page.setViewportSize({ width: 390, height: 844 })
  const updated = page.waitForResponse(
    `**/api/v1/theaters/${theater.slug}/showtimes*`,
  )
  await page.getByRole('tab', { name: 'Demain', exact: true }).click()
  await updated
  await expect(page.locator('main > header dd')).not.toContainText(
    theater.address,
  )
  await page.setViewportSize(testInfo.project.use.viewport!)
  await expectHeaderLayout(
    page.locator('main > header'),
    testInfo.project.name === 'desktop',
    '',
  )
})

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
