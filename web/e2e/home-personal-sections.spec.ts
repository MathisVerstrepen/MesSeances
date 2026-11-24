import {
  date,
  publicActivityItems,
  secondTheater,
  showtimes,
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
  const [saved, , expired, , anotherSaved] = watchlistMovies
  const [showtime] = showtimes().showtimes
  const session = (movie: typeof saved, hour: string) => ({
    ...showtime,
    movie,
    start_time: `${date}T${hour}:00+01:00`,
  })
  await page.route('**/api/v1/timeline?*', async (route) => {
    const params = new URL(route.request().url()).searchParams
    expect(params.get('date')).toBe(date)
    expect(params.get('theaters')?.split(',').sort()).toEqual(
      [theater.id, secondTheater.id].sort(),
    )
    await route.fulfill({
      json: {
        date,
        theaters: [
          {
            ...theater,
            showtimes: [session(saved, '17:00'), session(expired, '10:00')],
          },
          {
            ...secondTheater,
            showtimes: [
              session(saved, '15:00'),
              session(anotherSaved, '16:00'),
            ],
          },
        ],
      },
    })
  })
  await page.route(
    `**/api/v1/theaters/${secondTheater.slug}/activity?*`,
    async (route) => {
      await route.fulfill({
        json: {
          items: [
            {
              ...publicActivityItems[1],
              event_id: '204',
              detected_at: '2026-10-02T12:00:00Z',
              movie: {
                ...publicActivityItems[1].movie,
                title: 'Une séance à Roubaix',
              },
            },
          ],
        },
      })
    },
  )

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
  await page.route(
    `**/api/v1/theaters/${theater.slug}/activity?*`,
    async (route) => {
      attempts++
      if (attempts === 1) {
        await route.fulfill({
          status: 503,
          json: { error: { code: 'history_unavailable' } },
        })
      } else {
        await route.continue()
      }
    },
  )
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
  await activity.getByRole('button', { name: 'Réessayer' }).click()
  await expect(
    activity.getByRole('heading', { name: 'Les lumières reviennent' }),
  ).toBeVisible()
  await expect(activity.getByRole('alert')).toHaveCount(0)
})
