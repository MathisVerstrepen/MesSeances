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
  for (const viewport of ['mobile', 'desktop']) {
    expect(html).toMatch(
      new RegExp(`id="cinema-${viewport}-result-layout"[\\s\\S]*?</button>`),
    )
    const selector = html.match(
      new RegExp(`id="cinema-${viewport}-result-layout"[\\s\\S]*?</button>`),
    )![0]
    expect(selector).toContain('Boîtes')
    expect(selector).not.toContain('Lignes')
  }
  expect(html).toContain(
    `/statistiques?period=all&amp;theater=${encodeURIComponent(theater.id)}`,
  )
})

test('cinema boxes default and explicit lines survive date, grouping, tabs and history', async ({
  page,
}, testInfo) => {
  await openPage(page, `${path}&other=keep`)
  const layout = page.getByRole('button', { name: 'Vue Boîtes', exact: true })
  await expect(layout).toBeVisible()
  const sessions = page.getByRole('list', {
    name: `Séances de ${movie.title}`,
    exact: true,
  })
  await expect(sessions).toHaveCSS('display', 'grid')
  await layout.click()
  await expect(
    page.getByRole('menuitemradio', { name: 'Boîtes', exact: true }),
  ).toHaveAttribute('aria-checked', 'true')
  await page.getByRole('menuitemradio', { name: 'Lignes', exact: true }).click()
  await expect(page).toHaveURL(/layout=lines/)
  await expect(
    page.getByRole('button', { name: 'Vue Lignes', exact: true }),
  ).toBeVisible()
  await expect(sessions).not.toHaveCSS('display', 'grid')
  const preserved = () => {
    const query = new URL(page.url()).searchParams
    expect(query.get('layout')).toBe('lines')
    expect(query.get('other')).toBe('keep')
  }
  await page
    .getByRole('button', { name: 'Groupement Par film', exact: true })
    .click()
  await page
    .getByRole('menuitemradio', { name: 'Chronologique', exact: true })
    .click()
  await expect(page).toHaveURL(/grouping=chronological/)
  preserved()
  const nextDate = new Date(`${date}T12:00:00Z`)
  nextDate.setUTCDate(nextDate.getUTCDate() + 1)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('tab', { name: 'Demain', exact: true }).click()
  await expect(page).toHaveURL(
    new RegExp(`date=${nextDate.toISOString().slice(0, 10)}`),
  )
  preserved()
  await page.getByRole('tab', { name: 'Aujourd’hui', exact: true }).click()
  await page.setViewportSize(testInfo.project.use.viewport!)
  await expect(
    page.getByRole('button', { name: 'Vue Lignes', exact: true }),
  ).toBeVisible()
  preserved()
  const navigation = page.getByRole('navigation', {
    name: 'Vue de la programmation',
  })
  for (const tab of ['Films', 'Activité', 'Séances']) {
    await navigation.getByRole('link', { name: tab, exact: true }).click()
    await expect(
      navigation.getByRole('link', { name: tab, exact: true }),
    ).toHaveAttribute('aria-current', 'page')
    preserved()
  }
  await page.getByRole('button', { name: 'Vue Lignes', exact: true }).click()
  await page.getByRole('menuitemradio', { name: 'Boîtes', exact: true }).click()
  await expect(page).not.toHaveURL(/layout=/)
  await expect(layout).toBeVisible()
  await expect(
    page.getByRole('list', {
      name: 'Séances par ordre chronologique',
      exact: true,
    }),
  ).toHaveCSS('display', 'grid')
  expect(new URL(page.url()).searchParams.get('other')).toBe('keep')
  expect(new URL(page.url()).searchParams.get('grouping')).toBe('chronological')
  await page.goBack()
  await expect(
    page.getByRole('button', { name: 'Vue Lignes', exact: true }),
  ).toBeVisible()
  preserved()
  await page.goForward()
  await expect(layout).toBeVisible()
  await expect(page).not.toHaveURL(/layout=/)
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

test('ICE showtime logo loads without duplicate labels or overflow', async ({
  page,
}, info) => {
  await openPage(page, path)
  await page.route(`**/api/v1/theaters/${theater.slug}/showtimes*`, (route) => {
    const payload = showtimes()
    return route.fulfill({
      json: {
        ...payload,
        date: new URL(route.request().url()).searchParams.get('date'),
        showtimes: payload.showtimes.map((showtime) => ({
          ...showtime,
          format: 'ICE',
        })),
      },
    })
  })
  // Trigger a client fetch, reusing the synthetic SSR fixture for page identity.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('tab', { name: 'Demain', exact: true }).click()
  await page.setViewportSize(info.project.use.viewport!)
  const logo = page.locator('main img[src*="ice_logo_small"]:visible')
  await expect(logo).toHaveCount(1)
  await expect(logo).toHaveAttribute('alt', '')
  await expect(logo).toHaveAttribute('aria-hidden', 'true')
  await expect(logo.locator('..').locator('.sr-only')).toHaveText('ICE')
  await expect(logo).toHaveJSProperty('complete', true)
  expect(
    await logo.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  const box = await logo.boundingBox()
  expect(box!.height).toBeGreaterThanOrEqual(18)
  expect(box!.width / box!.height).toBeCloseTo(
    await logo.evaluate(
      (image: HTMLImageElement) => image.naturalWidth / image.naturalHeight,
    ),
    2,
  )
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
  await page.screenshot({
    path: info.outputPath('ice-showtime.png'),
    fullPage: true,
  })
})

test('ICE planning filter preserves selected inversion and canonical value', async ({
  page,
}, info) => {
  await page.route('**/api/v1/timeline?**', (route) =>
    route.fulfill({
      json: {
        date,
        timezone: 'Europe/Paris',
        window_start_time: `${date}T06:00:00+02:00`,
        window_end_time: `${date}T23:59:00+02:00`,
        theaters: [],
      },
    }),
  )
  await openPage(page, '/planning')
  const button = page.getByRole('button', { name: 'ICE', exact: true })
  const logo = button.locator('img')
  await expect(button).toHaveCount(1)
  await expect(button).toHaveAttribute('aria-pressed', 'false')
  await expect(logo).toHaveAttribute('src', /ice_logo_small/)
  await expect(logo).toHaveAttribute('alt', '')
  await expect(logo).toHaveAttribute('aria-hidden', 'true')
  await expect(logo).toHaveJSProperty('complete', true)
  expect(
    await logo.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  await expect(logo).not.toHaveClass(/invert/)
  await button.scrollIntoViewIfNeeded()
  await button.screenshot({ path: info.outputPath('ice-filter-default.png') })
  await button.click()
  await expect(button).toHaveAttribute('aria-pressed', 'true')
  await expect(page).toHaveURL(/format=ICE/)
  await expect(logo).toHaveClass(/brightness-0 invert/)
  expect(await logo.evaluate((image) => getComputedStyle(image).filter)).toBe(
    'brightness(0) invert(1)',
  )
  const box = await logo.boundingBox()
  expect(box!.height).toBeCloseTo(24, 0)
  expect(box!.width / box!.height).toBeCloseTo(
    await logo.evaluate(
      (image: HTMLImageElement) => image.naturalWidth / image.naturalHeight,
    ),
    2,
  )
  await button.screenshot({ path: info.outputPath('ice-filter-selected.png') })
  await page
    .getByRole('button', { name: 'Tous les formats', exact: true })
    .click()
  await expect(button).toHaveAttribute('aria-pressed', 'false')
  await expect(logo).not.toHaveClass(/invert/)
  await expect(page).not.toHaveURL(/format=ICE/)
})

test('ICE technology credit uses supplied display logo and official attribution', async ({
  page,
}, info) => {
  await openPage(page, '/credits')
  const credit = page.locator('section[aria-labelledby="credit-ICE"]')
  const link = credit.getByRole('link', {
    name: 'Site officiel ICE, ouverture dans un nouvel onglet',
    exact: true,
  })
  await expect(link).toHaveAttribute('href', 'https://www.icetheaters.com/')
  await expect(link).toHaveAttribute('target', '_blank')
  await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  const logo = link.locator('img')
  await expect(logo).toHaveAttribute('src', /ice_logo_large/)
  await expect(logo).toHaveAttribute('alt', '')
  await expect(logo).toHaveAttribute('aria-hidden', 'true')
  await expect(logo).toHaveJSProperty('complete', true)
  expect(
    await logo.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  const box = await logo.boundingBox()
  expect(box!.height).toBeCloseTo(64, 0)
  expect(box!.width / box!.height).toBeCloseTo(
    await logo.evaluate(
      (image: HTMLImageElement) => image.naturalWidth / image.naturalHeight,
    ),
    2,
  )
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
  await credit.screenshot({ path: info.outputPath('ice-credit.png') })
})
