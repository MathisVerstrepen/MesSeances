import {
  date,
  publicActivityItems,
  secondTheater,
  theater,
  watchlistMovies,
} from './data.mjs'
import { expect, openPage, test } from './fixtures'

test('home previews the next saved-film sessions and recent favorite-cinema activity below the shortcuts', async ({
  page,
  request,
}, testInfo) => {
  await request.post('/__playwright/scenario', {
    data: {
      enabled: true,
      state: 'complete',
      watchlist: true,
      selected_ids: [theater.id, secondTheater.id],
      theater_ids: [],
    },
  })
  await page.clock.setFixedTime(new Date(`${date}T12:00:00+01:00`))
  const [saved, , , , anotherSaved] = watchlistMovies
  const requests: string[] = []
  page.on('request', (request) => {
    const { pathname } = new URL(request.url())
    if (
      /\/api\/v1\/(timeline|account\/watchlist\/next-screenings|theaters\/(activity-preview|[^/]+\/activity))$/.test(
        pathname,
      )
    ) {
      requests.push(pathname)
    }
  })
  const session = (movie: typeof saved, hour: string) => ({
    movie_slug: movie.slug,
    title: movie.title,
    poster_url: movie.poster_url,
    start_time: `${date}T${hour}:00+01:00`,
    service_date: date,
    theater: secondTheater,
  })
  const favorites = [theater.id, secondTheater.id].sort()
  await page.route(
    '**/api/v1/account/watchlist/next-screenings',
    async (route) => {
      expect(route.request().method()).toBe('POST')
      expect(route.request().postDataJSON().sort()).toEqual(favorites)
      await route.fulfill({
        json: {
          username: 'fixture_alice',
          items: [session(saved, '15:00'), session(anotherSaved, '16:00')],
        },
      })
    },
  )
  await page.route('**/api/v1/theaters/activity-preview', async (route) => {
    expect(route.request().method()).toBe('POST')
    expect(route.request().postDataJSON().sort()).toEqual(favorites)
    await route.fulfill({
      json: [
        {
          ...publicActivityItems[1],
          event_id: '204',
          detected_at: '2026-10-02T12:00:00Z',
          movie: {
            ...publicActivityItems[1].movie,
            title: 'Une séance à Roubaix',
          },
          theater: secondTheater,
        },
        ...publicActivityItems
          .slice(0, 2)
          .map((item) => ({ ...item, theater })),
      ],
    })
  })

  const response = await openPage(page, '/')
  const watchlist = page.getByRole('region', { name: 'À voir dans ma liste.' })
  const activity = page.getByRole('region', {
    name: 'Nouveau dans mes cinémas.',
  })
  const shortcuts = page.getByRole('region', { name: 'Accès rapides' })
  await expect(watchlist.getByRole('heading', { level: 3 })).toHaveText([
    saved.title,
    anotherSaved.title,
  ])
  await expect(watchlist.locator('time').first()).toHaveAttribute(
    'datetime',
    `${date}T15:00:00+01:00`,
  )
  await expect(
    watchlist.getByRole('link', { name: saved.title }),
  ).toHaveAttribute(
    'href',
    `/film/${saved.slug}?shared_theaters=${secondTheater.id}&date=${date}#schedule-heading`,
  )
  await expect(
    watchlist.getByRole('link', { name: 'Ma watchlist' }),
  ).toHaveAttribute('href', '/compte/watchlist')
  expect(await response?.text()).not.toContain(saved.title)
  await expect(activity.getByRole('heading', { level: 3 })).toHaveText([
    'Une séance à Roubaix',
    'Les lumières reviennent',
    'Une nouvelle histoire',
  ])
  await expect(
    activity.getByText('Retour à l’affiche', { exact: false }),
  ).toBeVisible()
  await expect(
    activity.getByRole('link', { name: secondTheater.name }),
  ).toHaveAttribute('href', `/cinema/${secondTheater.slug}?view=activity`)
  expect(requests.sort()).toEqual([
    '/api/v1/account/watchlist/next-screenings',
    '/api/v1/theaters/activity-preview',
  ])
  const shortcutsBox = await shortcuts.boundingBox()
  const watchlistBox = await watchlist.boundingBox()
  const activityBox = await activity.boundingBox()
  expect(watchlistBox!.y).toBeGreaterThanOrEqual(
    shortcutsBox!.y + shortcutsBox!.height,
  )
  expect(activityBox!.y).toBeGreaterThanOrEqual(
    watchlistBox!.y + watchlistBox!.height,
  )
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(await page.evaluate(() => innerWidth))
  await page.screenshot({
    path: testInfo.outputPath('home-personal-sections.png'),
    fullPage: true,
  })
})

test('guests can see programming in their device favorites and retry a failed preview', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'anonymous' },
  })
  await page.addInitScript((id) => {
    localStorage.setItem(
      'messeances.favoriteTheaterIds.v1',
      JSON.stringify([id]),
    )
  }, theater.id)
  let attempts = 0
  await page.route('**/api/v1/theaters/activity-preview', async (route) => {
    expect(route.request().postDataJSON()).toEqual([theater.id])
    attempts++
    if (attempts === 1) {
      await route.fulfill({
        status: 429,
        headers: { 'Retry-After': '1' },
        json: { error: { code: 'rate_limited' } },
      })
    } else {
      await route.continue()
    }
  })
  await openPage(page, '/')
  const watchlist = page.getByRole('region', { name: 'À voir dans ma liste.' })
  const activity = page.getByRole('region', {
    name: 'Nouveau dans mes cinémas.',
  })
  await expect(
    watchlist.getByRole('link', { name: 'Se connecter' }),
  ).toHaveAttribute('href', '/connexion')
  await expect(activity.getByRole('alert')).toHaveText(
    'Impossible de charger les nouveautés de vos cinémas.',
  )
  expect(attempts).toBe(1)
  await activity.getByRole('button', { name: 'Réessayer' }).click()
  await expect(
    activity.getByRole('heading', { name: 'Les lumières reviennent' }),
  ).toBeVisible()
  await expect(activity.getByRole('alert')).toHaveCount(0)
  expect(attempts).toBe(2)
})

test('an empty watchlist screening preview uses one request across many program days', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: {
      enabled: true,
      state: 'complete',
      watchlist: true,
      selected_ids: [theater.id, secondTheater.id],
    },
  })
  const dates = Array.from({ length: 24 }, (_, index) =>
    new Date(Date.parse(`${date}T12:00:00Z`) + index * 86_400_000)
      .toISOString()
      .slice(0, 10),
  )
  await page.route('**/api/v1/theaters', (route) =>
    route.fulfill({
      json: [theater, secondTheater].map((venue) => ({
        ...venue,
        available_dates: dates,
      })),
    }),
  )
  const requests: string[] = []
  page.on('request', (request) => {
    const { pathname } = new URL(request.url())
    if (
      pathname === '/api/v1/timeline' ||
      pathname === '/api/v1/account/watchlist/next-screenings'
    ) {
      requests.push(pathname)
    }
  })
  await openPage(page, '/')
  const section = page.getByRole('region', { name: 'À voir dans ma liste.' })
  await expect(
    section.getByText(
      'Aucune séance à venir pour les films de votre watchlist dans vos cinémas.',
    ),
  ).toBeVisible()
  await expect(section.getByRole('alert')).toHaveCount(0)
  expect(requests).toEqual(['/api/v1/account/watchlist/next-screenings'])
})
