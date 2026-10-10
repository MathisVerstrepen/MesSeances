import { test, expect, openPage } from './fixtures'
import type { Locator } from '@playwright/test'
import {
  accountActivityItem,
  date,
  movie,
  secondTheater,
  theater,
} from './data.mjs'

const complete = {
  enabled: true,
  state: 'complete',
  revision: '1',
  theater_ids: [theater.id, secondTheater.id],
}

async function expectTopAlignedCinema(
  row: Locator,
  cinemaName: string,
  movieTitle = 'Film Playwright',
) {
  const cinemaLink = row.getByRole('link', { name: cinemaName, exact: true })
  const logo = cinemaLink.locator('img')
  await expect(logo).toHaveCount(1)
  await expect(logo).toHaveAttribute('aria-hidden', 'true')
  await expect(logo).toHaveAttribute('alt', '')
  await expect
    .poll(() =>
      logo.evaluate(
        (image: HTMLImageElement) => image.complete && image.naturalWidth > 0,
      ),
    )
    .toBe(true)
  const poster = await row
    .getByRole('link', { name: movieTitle, exact: true })
    .first()
    .boundingBox()
  const name = await cinemaLink.evaluate((element) => {
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (!node.textContent?.trim()) continue
      const range = document.createRange()
      range.selectNodeContents(node)
      const rect = range.getClientRects()[0]
      if (rect) return { top: rect.top, left: rect.left }
    }
    throw new Error('Cinema visible text missing')
  })
  const logoBox = await logo.boundingBox()
  expect(poster).not.toBeNull()
  expect(logoBox).not.toBeNull()
  expect(Math.abs(name.top - poster!.y)).toBeLessThanOrEqual(3)
  expect(logoBox!.x + logoBox!.width).toBeLessThanOrEqual(name.left + 1)
}

async function expectCompactTitleStack(row: Locator) {
  const cinema = row.locator('a[href^="/cinema/"]')
  const title = row.locator('h3 a')
  await expect(cinema).toHaveCSS('min-height', '0px')
  await expect(title).toHaveCSS('min-height', '0px')
  const geometry = await row.evaluate(async (element) => {
    await document.fonts.ready
    const cinema = element.querySelector('a[href^="/cinema/"]')
    const title = element.querySelector('h3 a')
    const badge = element.querySelector('h3 + p > span')
    if (!cinema || !title || !badge)
      throw new Error('Activity title stack missing')
    const cinemaBox = cinema.getBoundingClientRect()
    const titleBox = title.getBoundingClientRect()
    const badgeBox = badge.getBoundingClientRect()
    const range = document.createRange()
    range.selectNodeContents(title)
    return {
      before: titleBox.top - cinemaBox.bottom,
      after: badgeBox.top - titleBox.bottom,
      height: titleBox.height,
      lineHeight: Number.parseFloat(getComputedStyle(title).lineHeight),
      lines: range.getClientRects().length,
    }
  })
  expect(Math.abs(geometry.before - 4)).toBeLessThanOrEqual(1)
  expect(Math.abs(geometry.after - 4)).toBeLessThanOrEqual(1)
  expect(
    Math.abs(geometry.height - geometry.lines * geometry.lineHeight),
  ).toBeLessThanOrEqual(1)
  return geometry.lines
}

test('initial private feed skeleton stays local until first response', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', { data: complete })
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/v1/account/activity?*', async (route) => {
    await held
    await route.continue()
  })
  await openPage(page, '/compte/activite')
  await expect(
    page.getByRole('status').filter({ hasText: 'Chargement de l’activité' }),
  ).toBeAttached()
  await expect(page.locator('[data-event-id]')).toHaveCount(0)
  release()
  await expect(page.locator('[data-event-id]')).toHaveCount(2)
})

test('account home activity entry admits private stream with attribution, scoped links and deduplicated continuation', async ({
  page,
  request,
}, testInfo) => {
  await request.post('/__playwright/scenario', { data: complete })
  const ssr = await request.get('/compte/activite')
  expect(ssr.headers()['cache-control']).toContain('no-store')
  expect(ssr.headers()['x-robots-tag']).toContain('noindex')
  const html = await ssr.text()
  expect(html).not.toContain('follows_revision')
  expect(html).not.toContain('data-event-id="103"')
  await openPage(page, '/compte')
  await page
    .getByRole('navigation', { name: 'Rubriques du compte' })
    .getByRole('link', { name: 'Activité', exact: true })
    .click()
  await expect(page).toHaveURL(/\/compte\/activite$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Activité' }),
  ).toBeVisible()
  const rows = page.locator('[data-event-id]')
  await expect(rows).toHaveCount(2)
  const timeline = page.getByRole('list', {
    name: 'Activité des cinémas suivis',
  })
  await expect(timeline.locator('[data-activity-day]')).toHaveCount(1)
  await expect(timeline.getByRole('heading', { level: 2 })).toHaveCount(1)
  await expect(timeline.locator('h2 time')).toHaveAttribute('datetime', date)
  const dayGeometry = await timeline
    .locator('[data-activity-day]')
    .evaluate((element) => {
      const heading = element.querySelector('h2')!.getBoundingClientRect()
      const row = element
        .querySelector('[data-event-id]')!
        .getBoundingClientRect()
      return {
        dateRight: heading.right,
        dateBottom: heading.bottom,
        rowLeft: row.left,
        rowTop: row.top,
      }
    })
  if (testInfo.project.name === 'desktop')
    expect(dayGeometry.dateRight).toBeLessThan(dayGeometry.rowLeft)
  else expect(dayGeometry.dateBottom).toBeLessThan(dayGeometry.rowTop)
  await expect(page.locator('main details, main summary')).toHaveCount(0)
  await expect(
    page.getByText('Historique partiel', { exact: true }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Se déconnecter', exact: true }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('link', { name: 'Explorer les séances', exact: true }),
  ).toHaveCount(0)
  expect(
    await rows.evaluateAll((elements) =>
      elements.map((row) => row.getAttribute('data-event-id')),
    ),
  ).toEqual(['103', '102'])
  for (const [id, cinema] of [
    ['103', theater],
    ['102', secondTheater],
  ] as const) {
    const row = page.locator(`[data-event-id="${id}"]`)
    await expect(
      row.getByRole('link', { name: cinema.name, exact: true }),
    ).toHaveAttribute('href', `/cinema/${cinema.slug}?view=activity`)
    const cinemaLink = row.getByRole('link', { name: cinema.name, exact: true })
    await expect(cinemaLink.locator('img')).toHaveAttribute(
      'src',
      /ugc_logo_small/,
    )
    await expectTopAlignedCinema(row, cinema.name)
    expect(
      (await row
        .getByRole('link', { name: 'Film Playwright', exact: true })
        .first()
        .boundingBox())!.width,
    ).toBe(testInfo.project.name === 'desktop' ? 112 : 72)
    expect(await expectCompactTitleStack(row)).toBe(1)
    const badge = row.getByText(
      id === '103' ? 'Retour à l’affiche' : 'Ajout à la programmation',
      { exact: true },
    )
    await expect(badge).toHaveClass(
      id === '103'
        ? 'inline-block border-l-2 pl-2 align-top text-xs font-bold leading-4 border-accent text-accent'
        : 'inline-block border-l-2 pl-2 align-top text-xs font-bold leading-4 border-ink text-ink',
    )
    await expect(
      row.getByRole('link', { name: 'Voir les séances', exact: true }),
    ).toHaveAttribute(
      'href',
      `/film/film-playwright?shared_theaters=${cinema.id}&date=${date}#schedule-heading`,
    )
  }
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  await expect(rows).toHaveCount(3)
  expect(
    await rows.evaluateAll((elements) =>
      elements.map((row) => row.getAttribute('data-event-id')),
    ),
  ).toEqual(['103', '102', '101'])
  await expect(
    page.getByRole('button', { name: 'Afficher plus', exact: true }),
  ).toHaveCount(0)
  await page.evaluate(() => window.scrollTo(0, 0))
  await page.screenshot({
    path: testInfo.outputPath('account-activity.png'),
    fullPage: true,
  })
  if (testInfo.project.name === 'desktop')
    await expect(
      page
        .getByRole('navigation', { name: 'Espace personnel' })
        .getByRole('link', { name: 'Activité', exact: true }),
    ).toHaveAttribute('aria-current', 'page')
  else
    await expect(
      page.getByRole('link', { name: 'Mon compte', exact: true }).last(),
    ).toBeVisible()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)
  expect(
    await page.evaluate(() =>
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ),
  ).not.toMatch(/event_id|follows_revision|fixture-1/)
})

for (const [provider, name, asset] of [
  [
    'ugc',
    'Cinéma des grandes salles et des rencontres internationales '
      .repeat(4)
      .trim(),
    /ugc_logo_small/,
  ],
  [
    'kinepolis',
    `Kinepolis ${'QuartierInternational'.repeat(10)}`,
    /kinepolis_logo_small/,
  ],
  [
    'noecinemas',
    'Noé Cinémas de la très longue avenue des Lumières '.repeat(4).trim(),
    /noe_cinema_logo_small/,
  ],
] as const) {
  test(`activity ${provider} logo precedes full multiline cinema name without overflow`, async ({
    page,
    request,
  }, testInfo) => {
    await request.post('/__playwright/scenario', { data: complete })
    await page.route('**/api/v1/account/activity?*', async (route) => {
      const result = await route.fetch()
      const pageData = await result.json()
      await route.fulfill({
        json: {
          ...pageData,
          items: [accountActivityItem(103, { ...theater, provider, name })],
          next_cursor: null,
        },
      })
    })
    await openPage(page, '/compte/activite')
    const row = page.locator('[data-event-id="103"]')
    const cinemaLink = row.getByRole('link', { name, exact: true })
    await expect(cinemaLink).toHaveAttribute(
      'href',
      `/cinema/${theater.slug}?view=activity`,
    )
    await expect(cinemaLink).toContainText(name)
    await expect(cinemaLink.locator('img')).toHaveAttribute('src', asset)
    await expectTopAlignedCinema(row, name)
    expect(await expectCompactTitleStack(row)).toBe(1)
    expect(
      await cinemaLink.evaluate(
        (element) => element.getBoundingClientRect().height,
      ),
    ).toBeGreaterThan(44)
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true)
    await page.screenshot({
      path: testInfo.outputPath(`activity-${provider}-long-name.png`),
      fullPage: true,
    })
  })
}

test('multiline movie title keeps compact natural stack and working keyboard link', async ({
  page,
  request,
}, testInfo) => {
  const title =
    'Un grand voyage à travers les salles et les histoires du cinéma '
      .repeat(5)
      .trim()
  await request.post('/__playwright/scenario', { data: complete })
  await page.route('**/api/v1/account/activity?*', async (route) => {
    const result = await route.fetch()
    const pageData = await result.json()
    const item = accountActivityItem(103)
    await route.fulfill({
      json: {
        ...pageData,
        items: [{ ...item, movie: { ...item.movie, title } }],
        next_cursor: null,
      },
    })
  })
  await openPage(page, '/compte/activite')
  const row = page.locator('[data-event-id="103"]')
  await expectTopAlignedCinema(row, theater.name, title)
  expect(await expectCompactTitleStack(row)).toBeGreaterThan(1)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)
  const movieLink = row
    .getByRole('heading', { level: 3, name: title, exact: true })
    .getByRole('link', { name: title, exact: true })
  await expect(movieLink).toHaveAttribute(
    'href',
    `/film/film-playwright?shared_theaters=${theater.id}`,
  )
  await movieLink.focus()
  await expect(movieLink).toBeFocused()
  await page.screenshot({
    path: testInfo.outputPath('activity-multiline-title.png'),
    fullPage: true,
  })
  await movieLink.press('Enter')
  await expect(page).toHaveURL(
    new RegExp(`/film/film-playwright\\?shared_theaters=${theater.id}$`),
  )
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(movie.title)
  await expect(page.locator('.schedule-section')).toBeVisible()
  await expect(page.locator('.theater-section')).toHaveCount(1)
  await expect(
    page.locator('.theater-section').getByRole('heading', { level: 3 }),
  ).toContainText(theater.name)
  await expect(page.locator('#film-discovery-heading')).toHaveCount(1)
})

for (const [mode, heading] of [
  ['no-follows', 'Aucun cinéma suivi'],
  ['initializing', 'Historique en cours d’initialisation'],
  ['empty', 'Aucune nouvelle programmation détectée.'],
]) {
  test(`activity ${mode} has intentional empty state`, async ({
    page,
    request,
  }) => {
    await request.post('/__playwright/scenario', {
      data: {
        ...complete,
        feed: mode,
        theater_ids: mode === 'no-follows' ? [] : complete.theater_ids,
      },
    })
    await openPage(page, '/compte/activite')
    await expect(
      page.getByRole('heading', { name: heading, exact: true }),
    ).toBeVisible()
    if (mode === 'no-follows')
      await expect(
        page.getByRole('link', { name: 'Explorer les cinémas', exact: true }),
      ).toHaveAttribute('href', '/cinemas')
    await expect(
      page.getByText('Historique partiel', { exact: true }),
    ).toHaveCount(0)
    await expect(
      page.getByRole('button', { name: 'Se déconnecter', exact: true }),
    ).toHaveCount(0)
    await expect(
      page.getByRole('link', { name: 'Explorer les séances', exact: true }),
    ).toHaveCount(0)
  })
}

test('initial feed error retries; continuation error preserves rows then 409 replaces walk', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { ...complete, feed: 'error' },
  })
  await openPage(page, '/compte/activite')
  await expect(page.getByRole('alert')).toContainText(
    'Impossible de charger l’activité',
  )
  await request.post('/__playwright/scenario', { data: complete })
  await page.getByRole('button', { name: 'Réessayer', exact: true }).click()
  await expect(page.locator('[data-event-id]')).toHaveCount(2)
  let conflict = false
  await page.route('**/api/v1/account/activity?*', (route) => {
    if (!new URL(route.request().url()).searchParams.has('cursor'))
      return route.continue()
    return route.fulfill({
      status: conflict ? 409 : 503,
      json: {
        error: {
          code: conflict ? 'theater_follows_changed' : 'accounts_unavailable',
        },
      },
    })
  })
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText(
    'Impossible de charger la suite',
  )
  await expect(page.locator('[data-event-id]')).toHaveCount(2)
  conflict = true
  await request.post('/__playwright/scenario', {
    data: { ...complete, revision: '2', theater_ids: [secondTheater.id] },
  })
  await page.getByRole('button', { name: 'Réessayer', exact: true }).click()
  await expect(page.locator('[data-event-id]')).toHaveCount(1)
  await expect(page.locator('[data-event-id="102"]')).toBeVisible()
  await expect(page.locator('[data-event-id="103"]')).toHaveCount(0)
})

test('focus revalidation replaces pages after follows change without writes', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', { data: complete })
  await openPage(page, '/compte/activite')
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  await expect(page.locator('[data-event-id]')).toHaveCount(3)
  await request.post('/__playwright/scenario', {
    data: { ...complete, revision: '2', theater_ids: [secondTheater.id] },
  })
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(page.locator('[data-event-id]')).toHaveCount(1)
  await expect(page.locator('[data-event-id="102"]')).toBeVisible()
  expect(
    (await (await request.get('/__playwright/state')).json()).followPosts,
  ).toBe(0)
})

for (const transition of ['logout', 'owner', 'departure']) {
  test(`late private feed discarded after ${transition}`, async ({
    page,
    request,
  }) => {
    await request.post('/__playwright/scenario', { data: complete })
    await openPage(page, '/compte/activite')
    let release!: () => void
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    let entered!: () => void
    const intercepted = new Promise<void>((resolve) => {
      entered = resolve
    })
    await page.route('**/api/v1/account/activity?*', async (route) => {
      if (!new URL(route.request().url()).searchParams.has('cursor'))
        return route.continue()
      entered()
      await held
      await route.fulfill({
        json: {
          username: 'fixture_alice',
          follows_revision: '1',
          items: [accountActivityItem(999)],
          next_cursor: null,
        },
      })
    })
    await page
      .getByRole('button', { name: 'Afficher plus', exact: true })
      .click()
    await intercepted
    if (transition === 'logout') {
      await request.post('/__playwright/scenario', {
        data: { enabled: true, state: 'anonymous' },
      })
      await page.evaluate(() => window.dispatchEvent(new Event('focus')))
      await expect(page.getByRole('alert')).toContainText('session expirée')
    } else if (transition === 'owner') {
      await request.post('/__playwright/scenario', {
        data: { ...complete, username: 'fixture_bob', theater_ids: [] },
      })
      await page.evaluate(() => window.dispatchEvent(new Event('focus')))
      await expect(page.getByRole('alert')).toContainText('session expirée')
    } else {
      await page
        .getByRole('link', { name: 'Mon compte', exact: true })
        .first()
        .click()
      await expect(page).toHaveURL(/\/compte$/)
    }
    release()
    await expect(page.locator('[data-event-id="999"]')).toHaveCount(0)
    await expect(page.locator('[data-event-id="103"]')).toHaveCount(0)
  })
}

test('private activity direct and SPA admission use existing auth destination', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'anonymous' },
  })
  await openPage(page, '/compte/activite')
  await expect(page).toHaveURL(/\/connexion$/)
  expect(
    (await (await request.get('/__playwright/state')).json()).activityGets,
  ).toBe(0)
})
