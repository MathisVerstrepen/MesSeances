import { test, expect, openPage } from './fixtures'
import {
  alternativeTheater,
  date,
  discoveryCities,
  discoveryMovies,
  discoveryWindow,
  movie,
  secondTheater,
  theater,
} from './data.mjs'

const filmPath = `/film/${movie.slug}?date=${date}`
const cinemaPath = `/cinema/${theater.slug}?date=${date}`
const cityPath = '/ville/lille/cinemas'
const encoded = (value: string) => encodeURIComponent(value)
const longDate = (value: string) =>
  new Intl.DateTimeFormat('fr-FR', {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    timeZone: 'Europe/Paris',
  }).format(new Date(`${value}T12:00:00Z`))
const range = `du ${longDate(discoveryWindow.from)} au ${longDate(discoveryWindow.through)}`
const headings = {
  film: `Où voir ce film ${range}`,
  cinema: `Films ${range}`,
  city: `Cinémas ${range}`,
  alternatives: `Autres cinémas à Lille ${range}`,
}

async function noOverflow(page: import('@playwright/test').Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy()
}

async function keyboardFocus(
  page: import('@playwright/test').Page,
  link: import('@playwright/test').Locator,
) {
  await page.keyboard.press('Tab')
  await link.focus()
  await expect(link).toBeFocused()
  const indicator = await link.evaluate((element) => ({
    visible: element.matches(':focus-visible'),
    shadow: getComputedStyle(element).boxShadow,
  }))
  expect(indicator.visible).toBeTruthy()
  // Existing public focus treatment: 2px teal ring with 2px canvas offset.
  expect(indicator.shadow).toContain('rgb(31, 111, 120) 0px 0px 0px 4px')
}

async function localFilmScope(
  page: import('@playwright/test').Page,
  ids: string[],
) {
  await page.addInitScript((selected) => {
    localStorage.setItem(
      'messeances.favoriteTheaterIds.v1',
      JSON.stringify(selected),
    )
  }, ids)
}

async function closedFilmDiscovery(page: import('@playwright/test').Page) {
  const section = page.getByRole('region', { name: headings.film })
  await expect(section.locator('details')).toHaveJSProperty('open', false)
  await expect(section.getByRole('link')).toHaveCount(0)
  await expect(section.getByRole('link', { includeHidden: true })).toHaveCount(
    6,
  )
  await expect(page.locator('#film-discovery-heading')).toHaveCount(1)
  await expect(section.locator('ul')).toHaveCount(1)
  expect(
    await section.evaluate((element) =>
      element.previousElementSibling?.matches('.schedule-section'),
    ),
  ).toBeTruthy()
  return section
}

async function expandedFilmDiscovery(page: import('@playwright/test').Page) {
  const section = page.getByRole('region', { name: headings.film })
  await expect(section.getByRole('link')).toHaveCount(6)
  await expect(section.locator('details')).toHaveCount(0)
  await expect(page.locator('#film-discovery-heading')).toHaveCount(1)
  expect(
    await section.evaluate((element) =>
      element.nextElementSibling?.matches('.schedule-section'),
    ),
  ).toBeTruthy()
  return section
}

test('SSR emits bounded canonical contextual anchors, period labels and scoped counts before hydration', async ({
  request,
}) => {
  for (const [path, heading, anchors, counts] of [
    [
      filmPath,
      headings.film,
      discoveryCities.map((city) => `/ville/${encoded(city.slug)}/cinemas`),
      ['2 cinémas · 20 séances', '1 cinéma · 4 séances'],
    ],
    [
      cinemaPath,
      headings.cinema,
      [
        ...discoveryMovies.map(
          (entry) =>
            `/film/${encoded(entry.slug)}?shared_theaters=${theater.id}`,
        ),
        `/cinema/${encoded(alternativeTheater.slug)}`,
      ],
      ['12 séances', '9 séances', '3 films · 8 séances'],
    ],
    [
      cityPath,
      headings.city,
      [
        `/cinema/${theater.slug}`,
        `/cinema/${encoded(alternativeTheater.slug)}`,
      ],
      ['6 films · 29 séances', '0 film · 0 séances'],
    ],
  ] as const) {
    const response = await request.get(path)
    expect(response.status()).toBe(200)
    // Payload scripts are not HTML evidence: inspect only actual server markup.
    const html = (await response.text())
      .replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, '')
      .replace(/<!--.*?-->/g, '')
    expect(html).toContain(heading)
    for (const href of anchors) expect(html).toContain(`href="${href}"`)
    for (const count of counts) expect(html).toContain(count)
    expect(html).not.toContain('merged-film')
  }
})

test('film geography remains server ordered through hydration, preferences, filters and canonical redirects', async ({
  page,
  request,
}, testInfo) => {
  // Different browser time must not recompute serialized discovery dates.
  await page.clock.setFixedTime(new Date('2032-01-01T12:00:00Z'))
  await page.addInitScript(() => {
    localStorage.setItem(
      'messeances.favoriteTheaterIds.v1',
      JSON.stringify(['fixture-second']),
    )
  })
  await openPage(page, `${filmPath}&shared_theaters=fixture-second`)
  const section = await closedFilmDiscovery(page)
  await section.locator('summary').click()
  await expect(section.getByRole('link')).toHaveCount(6)
  for (const [index, city] of discoveryCities.entries()) {
    const link = section.getByRole('link').nth(index)
    await expect(link).toHaveAttribute(
      'href',
      `/ville/${encoded(city.slug)}/cinemas`,
    )
    await expect(link).toContainText(city.name)
    await expect(link).toContainText(
      `${city.theater_count} cinéma${city.theater_count > 1 ? 's' : ''} · ${city.showtime_count} séances`,
    )
  }
  await noOverflow(page)
  await keyboardFocus(page, section.getByRole('link').first())
  await page.screenshot({
    path: testInfo.outputPath('film-discovery.png'),
    fullPage: true,
  })
  await section.getByRole('link').first().click()
  await expect(
    page.getByRole('heading', { name: 'Lille.', exact: true }),
  ).toBeVisible()
  const redirected = await request.get(`/film/merged-film?date=${date}`, {
    maxRedirects: 0,
  })
  expect(redirected.status()).toBe(308)
  expect(redirected.headers().location).toContain(`/film/${movie.slug}`)
  await openPage(page, `${filmPath}&language=VOF&page=2`)
  await (await closedFilmDiscovery(page)).locator('summary').click()
  await expect(
    page.getByRole('region', { name: headings.film }).getByRole('link'),
  ).toHaveCount(6)
})

test('saved film cinemas show sessions first and open nationwide links without fetching', async ({
  page,
}, testInfo) => {
  await localFilmScope(page, [theater.id])
  let scheduleReads = 0
  page.on('request', (request) => {
    if (
      /\/api\/v1\/movies\/[^/]+\/showtimes/.test(
        new URL(request.url()).pathname,
      )
    )
      scheduleReads++
  })
  await openPage(page, filmPath)
  await expect(page.locator('.theater-section')).toHaveCount(1)
  await expect(
    page.locator('.theater-section').getByRole('heading', { level: 3 }),
  ).toContainText(theater.name)
  const section = await closedFilmDiscovery(page)
  await noOverflow(page)
  await page.screenshot({
    path: testInfo.outputPath('film-scoped-closed.png'),
    fullPage: true,
  })
  const beforeOpen = scheduleReads
  await keyboardFocus(page, section.locator('summary'))
  await page.keyboard.press('Enter')
  await expect(section.getByRole('link')).toHaveCount(6)
  await expect(section).toContainText('2 cinémas · 20 séances')
  expect(scheduleReads).toBe(beforeOpen)
  await noOverflow(page)
  await page.screenshot({
    path: testInfo.outputPath('film-scoped-open.png'),
    fullPage: true,
  })
  await section.locator('summary').click()
  await closedFilmDiscovery(page)
  await section.locator('summary').click()
  await keyboardFocus(page, section.getByRole('link').nth(4))
  await section.getByRole('link').nth(4).click()
  await expect(page).toHaveURL(/\/ville\/paris%20%26%20proche\/cinemas/)
})

test('shared film cinemas restore saved scope with disclosure closed again', async ({
  page,
}) => {
  await localFilmScope(page, [theater.id])
  await openPage(page, `${filmPath}&shared_theaters=${secondTheater.id}`)
  await expect(page.locator('.theater-section')).toHaveCount(1)
  await expect(
    page.locator('.theater-section').getByRole('heading', { level: 3 }),
  ).toContainText(secondTheater.name)
  const section = await closedFilmDiscovery(page)
  await section.locator('summary').click()
  await expect(section.getByRole('link')).toHaveCount(6)
  await page
    .locator('.schedule-section')
    .getByRole('button', { name: 'Utiliser mes cinémas', exact: true })
    .click()
  await expect(page).not.toHaveURL(/shared_theaters/)
  await expect(
    page.locator('.theater-section').getByRole('heading', { level: 3 }),
  ).toContainText(theater.name)
  await closedFilmDiscovery(page)
  await noOverflow(page)
})

test('shared film scope switches to all cinemas with expanded discovery above sessions', async ({
  page,
}) => {
  await localFilmScope(page, [])
  await openPage(page, `${filmPath}&shared_theaters=${secondTheater.id}`)
  const section = await closedFilmDiscovery(page)
  await section.locator('summary').click()
  await page
    .locator('.schedule-section')
    .getByRole('button', { name: 'Utiliser mes cinémas', exact: true })
    .click()
  await expect(page).not.toHaveURL(/shared_theaters/)
  await expandedFilmDiscovery(page)
  await expect(page.locator('.theater-section')).toHaveCount(3)
  await noOverflow(page)
})

test('film empty local programme broadens explicitly without hiding nationwide discovery', async ({
  page,
  request,
}, testInfo) => {
  await request.post('/__playwright/scenario', {
    data: { discovery: 'no-local-programme' },
  })
  await localFilmScope(page, [secondTheater.id])
  await openPage(page, filmPath)
  const broaden = page.getByRole('button', {
    name: 'Rechercher dans toute la France',
    exact: true,
  })
  await expect(broaden).toBeVisible()
  await expect(page.locator('.theater-section')).toHaveCount(0)
  await closedFilmDiscovery(page)
  await broaden.click()
  await expandedFilmDiscovery(page)
  await expect(page.locator('.theater-section')).toHaveCount(2)
  await noOverflow(page)
  await page.screenshot({
    path: testInfo.outputPath('film-broadened.png'),
    fullPage: true,
  })
})

test('scoped film omits both disclosure and links when server discovery is missing', async ({
  page,
  request,
}) => {
  await localFilmScope(page, [theater.id])
  for (const mode of ['empty', 'null-window']) {
    await request.post('/__playwright/scenario', { data: { discovery: mode } })
    await openPage(page, filmPath)
    await expect(page.locator('.schedule-section')).toBeVisible()
    await expect(page.locator('#film-discovery-heading')).toHaveCount(0)
    await expect(page.locator('main details')).toHaveCount(0)
    await noOverflow(page)
  }
})

test('cinema teaser exposes unknown-runtime counts, canonical scoped targets and only same-city alternatives', async ({
  page,
}, testInfo) => {
  await openPage(page, cinemaPath)
  const teaser = page.getByRole('region', { name: headings.cinema })
  await expect(teaser.getByRole('link')).toHaveCount(6)
  await expect(
    teaser
      .getByRole('heading', { level: 3 })
      .filter({ hasNotText: headings.cinema }),
  ).toHaveText(discoveryMovies.map((entry) => entry.title))
  await expect(teaser.getByText('9 séances', { exact: true })).toBeVisible()
  await expect(teaser.getByRole('link').nth(1)).toHaveAttribute(
    'href',
    `/film/${encoded(discoveryMovies[1].slug)}?shared_theaters=${theater.id}`,
  )
  const alternatives = page.getByRole('region', { name: headings.alternatives })
  await expect(alternatives.getByRole('link')).toHaveCount(1)
  await expect(alternatives.getByRole('link')).toHaveAttribute(
    'href',
    `/cinema/${encoded(alternativeTheater.slug)}`,
  )
  await expect(alternatives).toContainText('3 films · 8 séances')
  await expect(alternatives).not.toContainText('Roubaix')
  await noOverflow(page)
  await page.screenshot({
    path: testInfo.outputPath('cinema-discovery.png'),
    fullPage: true,
  })
  await teaser.getByRole('link').nth(1).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(
    discoveryMovies[1].title,
  )
  await expect(page).toHaveURL(/shared_theaters=fixture-cinema/)
  await openPage(page, cinemaPath)
  await page
    .getByRole('region', { name: headings.alternatives })
    .getByRole('link')
    .click()
  await expect(page.getByRole('heading', { level: 1 })).toContainText(
    alternativeTheater.name,
  )
})

test('cinema discovery scrolls one poster row on mobile and tablet, reveals keyboard focus and retains desktop grid', async ({
  page,
}, testInfo) => {
  await openPage(page, cinemaPath)
  const teaser = page.getByRole('region', { name: headings.cinema })
  const list = teaser.getByRole('list')
  const links = list.getByRole('link')
  await expect(links).toHaveCount(6)
  for (const [index, entry] of discoveryMovies.entries()) {
    await expect(links.nth(index)).toHaveAttribute(
      'href',
      `/film/${encoded(entry.slug)}?shared_theaters=${theater.id}`,
    )
  }

  const geometry = () =>
    list.evaluate((element) => {
      const style = getComputedStyle(element)
      return {
        display: style.display,
        columns: style.gridTemplateColumns.split(' ').length,
        snap: style.scrollSnapType,
        width: element.clientWidth,
        scrollWidth: element.scrollWidth,
        tops: Array.from(
          element.children,
          (card) => card.getBoundingClientRect().top,
        ),
        lefts: Array.from(
          element.children,
          (card) => card.getBoundingClientRect().left,
        ),
      }
    })
  const fullyRevealed = (link: import('@playwright/test').Locator) =>
    link.evaluate((link) => {
      const card = link.getBoundingClientRect()
      const row = link.closest('ul')!.getBoundingClientRect()
      return card.left >= row.left && card.right <= row.right
    })

  for (const width of [320, 390, 768, 1023]) {
    await links.last().blur()
    await page.setViewportSize({ width, height: 900 })
    await list.evaluate((element) => element.scrollTo({ left: 0 }))
    await expect
      .poll(() => list.evaluate((element) => element.scrollLeft))
      .toBe(0)
    const row = await geometry()
    expect(row.display).toBe('flex')
    expect(row.snap).toBe('x mandatory')
    expect(row.scrollWidth).toBeGreaterThan(row.width)
    expect(Math.max(...row.tops) - Math.min(...row.tops)).toBeLessThan(1)
    expect(row.lefts[5]).toBeGreaterThan(row.lefts[0] + row.width)
    await noOverflow(page)
    if (width === 390 || width === 768) {
      await teaser.scrollIntoViewIfNeeded()
      await page.screenshot({
        path: testInfo.outputPath(`cinema-carousel-${width}.png`),
        fullPage: true,
      })
    }

    // Native scrolling alone must expose the final card, without clicking it.
    await list.evaluate((element) =>
      element.scrollTo({ left: element.scrollWidth }),
    )
    await expect.poll(() => fullyRevealed(links.last())).toBe(true)
    await expect
      .poll(() => list.evaluate((element) => element.scrollLeft))
      .toBeGreaterThan(0)
    await list.evaluate((element) => element.scrollTo({ left: 0 }))
    await expect
      .poll(() => list.evaluate((element) => element.scrollLeft))
      .toBe(0)

    // Tab follows server order and browser focus scrolls off-screen links into view.
    await links.first().focus()
    for (let index = 1; index < 6; index++) {
      await page.keyboard.press('Tab')
      await expect(links.nth(index)).toBeFocused()
      await expect.poll(() => fullyRevealed(links.nth(index))).toBe(true)
    }
    await expect.poll(() => fullyRevealed(links.last())).toBe(true)
    expect(
      await links.last().evaluate((link) => link.matches(':focus-visible')),
    ).toBe(true)
    expect(
      await links.last().evaluate((link) => getComputedStyle(link).boxShadow),
    ).toContain('rgb(31, 111, 120)')
    await noOverflow(page)
  }

  await page.setViewportSize({ width: 1440, height: 900 })
  const grid = await geometry()
  expect(grid.display).toBe('grid')
  expect(grid.columns).toBe(6)
  expect(grid.snap).toBe('none')
  expect(grid.scrollWidth).toBeLessThanOrEqual(grid.width + 5)
  expect(Math.max(...grid.tops) - Math.min(...grid.tops)).toBeLessThan(1)
  expect(new Set(grid.lefts).size).toBe(6)
  await noOverflow(page)
  await links.last().blur()
  await page.screenshot({
    path: testInfo.outputPath('cinema-carousel-desktop.png'),
    fullPage: true,
  })
  await links.last().click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(
    discoveryMovies[5].title,
  )
  await expect(page).toHaveURL(/shared_theaters=fixture-cinema/)
})

test('city venue rows keep inventory order, addresses, scoped zero counts and real focusable links', async ({
  page,
}, testInfo) => {
  await openPage(page, cityPath)
  const venues = page
    .getByRole('heading', { name: headings.city })
    .locator('../..')
  await expect(venues.getByRole('listitem')).toHaveCount(3)
  await expect(venues.getByRole('listitem').first()).toContainText(
    '6 films · 29 séances',
  )
  await expect(venues.getByRole('listitem').last()).toContainText(
    '0 film · 0 séances',
  )
  await expect(venues.getByRole('listitem').first()).toContainText(
    theater.address,
  )
  const first = venues.getByRole('link').first()
  await keyboardFocus(page, first)
  await noOverflow(page)
  await page.screenshot({
    path: testInfo.outputPath('city-discovery.png'),
    fullPage: true,
  })
  await first.click()
  await expect(page.getByRole('heading', { level: 1 })).toContainText(
    theater.name,
  )
})

for (const mode of ['empty', 'null-window', 'single-cinema']) {
  test(`${mode} preserves main pages without fabricated optional discovery`, async ({
    page,
    request,
  }) => {
    await request.post('/__playwright/scenario', { data: { discovery: mode } })
    const cinemaHtml = (await (await request.get(cinemaPath)).text()).replace(
      /<script\b[^>]*>[\s\S]*?<\/script>/g,
      '',
    )
    expect(cinemaHtml.includes('cinema-discovery-films-heading')).toBe(
      mode === 'single-cinema',
    )
    await openPage(page, cinemaPath)
    await expect(
      page.getByRole('heading', { name: headings.alternatives }),
    ).toHaveCount(0)
    await expect(
      page.getByRole('heading', { name: headings.cinema }),
    ).toHaveCount(mode === 'single-cinema' ? 1 : 0)
    await expect(
      page.getByRole('heading', { name: 'Séances', exact: true }),
    ).toBeVisible()
    await noOverflow(page)
    await openPage(page, cityPath)
    if (mode === 'null-window') {
      await expect(
        page.getByRole('heading', { name: 'Cinémas', exact: true }),
      ).toBeVisible()
      await expect(
        page.getByText('0 film · 0 séances', { exact: true }),
      ).toHaveCount(0)
    } else if (mode === 'single-cinema')
      await expect(
        page
          .getByRole('heading', { name: headings.city })
          .locator('../..')
          .getByRole('listitem'),
      ).toHaveCount(1)
    else
      await expect(
        page.getByText('0 film · 0 séances', { exact: true }),
      ).toHaveCount(3)
    await openPage(page, filmPath)
    await expect(
      page.getByRole('heading', { name: headings.film }),
    ).toHaveCount(mode === 'single-cinema' ? 1 : 0)
    await noOverflow(page)
  })
}

test('ended and upcoming film details omit empty geography without dropping detail identity', async ({
  page,
  request,
}) => {
  for (const [slug, title] of [
    ['film-ended', 'Film terminé'],
    ['film-upcoming', 'Film à venir'],
  ]) {
    const response = await request.get(`/film/${slug}`)
    expect(response.status()).toBe(200)
    const html = (await response.text()).replace(
      /<script\b[^>]*>[\s\S]*?<\/script>/g,
      '',
    )
    expect(html).toContain(title)
    expect(html).not.toContain('film-discovery-heading')
    await openPage(page, `/film/${slug}`)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(title)
    await expect(page.locator('#film-discovery-heading')).toHaveCount(0)
  }
})

test('same-entity cinema refresh failure retains discovery; failed entity navigation clears it', async ({
  page,
}) => {
  await openPage(page, cinemaPath)
  await page.route(`**/api/v1/theaters/${theater.slug}/showtimes*`, (route) =>
    route.fulfill({
      status: 503,
      json: { error: { code: 'schedule_unavailable' } },
    }),
  )
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('tab', { name: 'Demain', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Impossible de charger ces séances' }),
  ).toBeVisible()
  await expect(
    page.getByRole('region', { name: headings.cinema }).getByRole('link'),
  ).toHaveCount(6)
  await page.route('**/api/v1/theaters/*/showtimes*', (route) =>
    route.fulfill({
      status: 503,
      json: { error: { code: 'schedule_unavailable' } },
    }),
  )
  await page
    .getByRole('region', { name: headings.alternatives })
    .getByRole('link')
    .click()
  await expect(page.locator('#cinema-discovery-films-heading')).toHaveCount(0)
  await expect(
    page.locator('#cinema-discovery-alternatives-heading'),
  ).toHaveCount(0)
})

test('synthetic sitemap child XML retains URLs, null baselines, observed dates and undated hubs', async ({
  request,
}) => {
  for (const family of ['films', 'cinemas', 'cities']) {
    const response = await request.get(`/sitemaps/${family}.xml`)
    expect(response.status()).toBe(200)
    const xml = await response.text()
    expect(xml).toContain('<urlset')
    expect(xml).toContain('<lastmod>')
    if (family === 'films') {
      expect(xml).toContain('/films/prochainement</loc>\n  </url>')
      expect(xml).toContain('/film/film-100</loc>\n  </url>')
      expect(xml).toContain('/film/film-106</loc>')
      expect(xml).not.toContain('/film/film-107</loc>')
      expect(xml).toContain('/films</loc>\n  </url>')
    } else if (family === 'cinemas')
      expect(xml).toContain('/cinemas</loc>\n  </url>')
  }
})
