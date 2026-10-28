import { test, expect, openPage } from './fixtures'
import type { Page } from '@playwright/test'
import {
  movie,
  screeningCatalog,
  secondTheater,
  theater,
  watchlistMovies,
} from './data.mjs'

const complete = {
  enabled: true,
  state: 'complete',
  watchlist: true,
  selected_ids: [theater.id],
  theater_ids: [secondTheater.id],
}
const chip = '[data-watchlist-screenings]'
const average = new Intl.NumberFormat('fr-FR', {
  maximumFractionDigits: 1,
}).format(
  (screeningCatalog(new URLSearchParams()).items[0]?.remaining_showtime_count ??
    0) / screeningCatalog(new URLSearchParams()).screening_window.day_count,
)
function row(page: Page, slug: string) {
  return page
    .locator('li')
    .filter({ has: page.locator(`a[href="/film/${slug}"]`) })
}
async function display(page: Page, mode: 'Liste' | 'Par tag') {
  const mobile = (page.viewportSize()?.width ?? 1440) < 1024
  if (mobile)
    await page
      .getByRole('button', { name: 'Configuration', exact: true })
      .click()
  await page.getByRole('button', { name: mode, exact: true }).click()
  if (mobile)
    await page.getByRole('button', { name: 'Fermer la configuration' }).click()
}
async function noOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
}

test('selected-cinema chips preserve date/tag order, list/grouped rows, search dialogs and removal', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', { data: complete })
  await openPage(page, '/compte/watchlist')
  await expect(page.locator(chip)).toHaveCount(2)
  const positive = row(page, movie.slug)
  await expect(positive.getByText('En salle', { exact: true })).toBeVisible()
  await expect(
    positive.getByText(`${average} séances/j`, { exact: true }),
  ).toBeVisible()
  await expect(positive.locator(`${chip} span`).last()).toHaveAttribute(
    'aria-label',
    /Moyenne quotidienne.*restantes.*mardi.*inclus/,
  )
  const zero = row(page, 'film-imported')
  await expect(zero.getByText('En salle', { exact: true })).toHaveCount(0)
  await expect(zero.getByText('0 séances/j', { exact: true })).toHaveCount(0)
  await expect(zero.locator(chip)).toHaveCount(0)
  const grace = row(page, 'film-grace')
  await expect(grace.getByText('En salle', { exact: true })).toHaveCount(0)
  await expect(grace.getByText('0 séances/j', { exact: true })).toHaveCount(0)
  await expect(grace.locator(chip)).toHaveCount(0)
  const distant = row(page, 'film-distant')
  await expect(distant.getByText('En salle', { exact: true })).toHaveCount(0)
  await expect(distant.getByText('0 séances/j', { exact: true })).toHaveCount(0)
  await expect(distant.locator(chip)).toHaveCount(0)
  const afterTuesday = row(page, 'film-after-tuesday')
  await expect(
    afterTuesday.getByText('En salle', { exact: true }),
  ).toBeVisible()
  await expect(
    afterTuesday.getByText('0 séances/j', { exact: true }),
  ).toBeVisible()
  await expect(page.getByText('En salle', { exact: true })).toHaveCount(2)
  expect(
    await positive.evaluate((element) => {
      const date = element.querySelector('time')!
      const chips = element.querySelector('[data-watchlist-screenings]')!
      const tag = element.querySelector('[aria-label="Tags associés"]')!
      return (
        !!(
          date.compareDocumentPosition(chips) & Node.DOCUMENT_POSITION_FOLLOWING
        ) &&
        !!(
          chips.compareDocumentPosition(tag) & Node.DOCUMENT_POSITION_FOLLOWING
        )
      )
    }),
  ).toBe(true)
  await noOverflow(page)
  await page.screenshot({
    path: info.outputPath('watchlist-list.png'),
    fullPage: true,
  })

  await display(page, 'Par tag')
  await expect(page.locator(chip)).toHaveCount(3)
  await expect(row(page, movie.slug)).toHaveCount(2)
  await expect(
    page.getByText(`${average} séances/j`, { exact: true }),
  ).toHaveCount(2)
  await expect(grace.getByText('En salle', { exact: true })).toHaveCount(0)
  await expect(distant.getByText('En salle', { exact: true })).toHaveCount(0)
  await expect(zero.locator(chip)).toHaveCount(0)
  await expect(grace.locator(chip)).toHaveCount(0)
  await expect(distant.locator(chip)).toHaveCount(0)
  await expect(
    afterTuesday.getByText('En salle', { exact: true }),
  ).toBeVisible()
  await expect(
    afterTuesday.getByText('0 séances/j', { exact: true }),
  ).toBeVisible()
  await expect(page.getByText('En salle', { exact: true })).toHaveCount(3)
  await noOverflow(page)
  await page.screenshot({
    path: info.outputPath('watchlist-tags.png'),
    fullPage: true,
  })
  const beforeSearch = (await (await request.get('/__playwright/state')).json())
    .screeningQueries.length
  expect(beforeSearch).toBe(1)

  await page.getByRole('button', { name: 'Ajouter', exact: true }).click()
  const dialog = page.getByRole('dialog', {
    name: 'Ajouter un film',
    exact: true,
  })
  await dialog
    .getByRole('searchbox', { name: 'Rechercher un film' })
    .fill('Film')
  await dialog.getByRole('button', { name: 'Rechercher', exact: true }).click()
  await expect(
    dialog.getByRole('link', { name: movie.title, exact: true }),
  ).toBeVisible()
  await expect(dialog.locator(chip)).toHaveCount(0)
  await dialog.getByRole('tab', { name: 'Autres films' }).click()
  await expect(
    dialog.getByRole('link', { name: 'Autre film recherché' }),
  ).toBeVisible()
  await expect(dialog.locator(chip)).toHaveCount(0)
  await page.keyboard.press('Escape')

  await row(page, 'film-imported')
    .getByRole('button', { name: 'Retirer de la watchlist', exact: true })
    .click()
  const removal = page.getByRole('dialog', {
    name: 'Retirer de la watchlist',
    exact: true,
  })
  await expect(
    removal.getByText(watchlistMovies[1]!.title, { exact: true }),
  ).toBeVisible()
  await removal.getByRole('button', { name: 'Retirer', exact: true }).click()
  await expect(removal).toHaveCount(0)
  await expect(row(page, 'film-imported')).toHaveCount(0)
  await expect(page.locator(chip)).toHaveCount(3)
  await noOverflow(page)
})

test('committed selected cinemas refresh independently of follows; initialized empty scope means all cinemas', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', { data: complete })
  await openPage(page, '/compte/watchlist')
  await expect(
    row(page, movie.slug).getByText('En salle', { exact: true }),
  ).toBeVisible()
  await request.post('/__playwright/screenings', {
    data: { selected_ids: [secondTheater.id] },
  })
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect
    .poll(async () => {
      const state = await (await request.get('/__playwright/state')).json()
      return state.screeningQueries.map(
        (query: { theaters?: string }) => query.theaters,
      )
    })
    .toEqual([theater.id, secondTheater.id])
  await expect(page.locator(chip)).toHaveCount(0)
  await expect(page.getByText('En salle', { exact: true })).toHaveCount(0)
  await expect(page.getByText('0 séances/j', { exact: true })).toHaveCount(0)
  await request.post('/__playwright/screenings', { data: { selected_ids: [] } })
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(
    row(page, movie.slug).getByText('En salle', { exact: true }),
  ).toBeVisible()
  await expect(
    row(page, 'film-after-tuesday').getByText('En salle', { exact: true }),
  ).toBeVisible()
  await expect(
    row(page, 'film-after-tuesday').getByText('0 séances/j', {
      exact: true,
    }),
  ).toBeVisible()
  await expect(
    row(page, 'film-distant').getByText('En salle', { exact: true }),
  ).toHaveCount(0)
  await expect(
    row(page, 'film-grace').getByText('En salle', { exact: true }),
  ).toHaveCount(0)
  await expect(page.locator(chip)).toHaveCount(2)
  await expect(row(page, 'film-imported').locator(chip)).toHaveCount(0)
  await expect(row(page, 'film-distant').locator(chip)).toHaveCount(0)
  await expect(row(page, 'film-grace').locator(chip)).toHaveCount(0)
  const state = await (await request.get('/__playwright/state')).json()
  expect(
    state.screeningQueries.map(
      (query: { theaters?: string }) => query.theaters,
    ),
  ).toEqual([theater.id, secondTheater.id, undefined])
  expect(state.follows.theater_ids).toEqual([secondTheater.id])
})

for (const mode of ['error', 'partial', 'selection-error']) {
  test(`${mode} stays unknown, keeps watchlist actions usable, explicit retry recovers`, async ({
    page,
    request,
  }, info) => {
    await request.post('/__playwright/scenario', {
      data: {
        ...complete,
        screenings: mode,
        selectionError: mode === 'selection-error',
      },
    })
    await openPage(page, '/compte/watchlist')
    await expect(
      page.getByText('Séances indisponibles.', { exact: true }),
    ).toBeVisible()
    await expect(page.locator(chip)).toHaveCount(0)
    await expect(page.getByText('0 séances/j', { exact: true })).toHaveCount(0)
    await expect(
      row(page, movie.slug).getByRole('button', {
        name: 'Retirer de la watchlist',
        exact: true,
      }),
    ).toBeEnabled()
    await page.screenshot({
      path: info.outputPath('watchlist-unavailable.png'),
      fullPage: true,
    })
    await request.post('/__playwright/screenings', {
      data: { mode: 'populated', selectionError: false },
    })
    await page
      .getByRole('button', { name: 'Réessayer les séances', exact: true })
      .click()
    await expect(page.locator(chip)).toHaveCount(2)
    await expect(
      page.getByText('Séances indisponibles.', { exact: true }),
    ).toHaveCount(0)
    await expect(
      row(page, movie.slug).getByText('En salle', { exact: true }),
    ).toBeVisible()
  })
}

test('focus retains screening chips, row geometry and scroll while replacement summaries are pending', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', { data: complete })
  await openPage(page, '/compte/watchlist')
  await expect(page.locator(chip)).toHaveCount(2)
  await page.evaluate(() => document.fonts.ready)
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight))
  const position = await page.evaluate(() => window.scrollY)
  expect(position).toBeGreaterThan(0)
  const original = await page.locator(chip).first().elementHandle()
  const height = await row(page, movie.slug).evaluate(
    (element) => element.getBoundingClientRect().height,
  )
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  let entered!: () => void
  const intercepted = new Promise<void>((resolve) => {
    entered = resolve
  })
  await page.route('**/api/v1/movies?*', async (route) => {
    if (
      new URL(route.request().url()).searchParams.get('screening_summary') !==
      'true'
    )
      return route.continue()
    entered()
    await held
    await route.continue()
  })
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await intercepted
  try {
    await expect(page.locator(chip)).toHaveCount(2)
    expect(await original!.evaluate((element) => element.isConnected)).toBe(
      true,
    )
    expect(
      await row(page, movie.slug).evaluate(
        (element) => element.getBoundingClientRect().height,
      ),
    ).toBe(height)
    expect(await page.evaluate(() => window.scrollY)).toBe(position)
  } finally {
    release()
  }
  await expect(
    page.getByRole('status').filter({ hasText: 'Chargement des séances' }),
  ).toHaveCount(0)
  await expect(page.locator(chip)).toHaveCount(2)
  expect(await page.evaluate(() => window.scrollY)).toBe(position)
})

test('focus resumes an interrupted watchlist movie search without closing the dialog or losing the query', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', { data: complete })
  await openPage(page, '/compte/watchlist')
  await page.getByRole('button', { name: 'Ajouter', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Ajouter un film' })
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  let entered!: () => void
  const intercepted = new Promise<void>((resolve) => {
    entered = resolve
  })
  let searches = 0
  await page.route('**/api/v1/account/watchlist/search', async (route) => {
    if (++searches !== 1) return route.continue()
    const original = await route.fetch()
    entered()
    await held
    await route.fulfill({ response: original })
  })
  await dialog
    .getByRole('searchbox', { name: 'Rechercher un film' })
    .fill('Film')
  await dialog.getByRole('button', { name: 'Rechercher', exact: true }).click()
  await intercepted
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  try {
    await expect(
      dialog.getByRole('link', { name: movie.title, exact: true }),
    ).toBeVisible()
    await expect(dialog.getByRole('searchbox')).toHaveValue('Film')
    await expect(
      dialog.getByRole('button', { name: 'Rechercher', exact: true }),
    ).toBeEnabled()
    expect(searches).toBe(2)
  } finally {
    release()
  }
})

test('pending summaries omit factual chips and expose one screen-reader loading status', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { ...complete, screenings: 'delay' },
  })
  await openPage(page, '/compte/watchlist')
  await expect(
    page.getByRole('status').filter({ hasText: 'Chargement des séances' }),
  ).toHaveCount(1)
  await expect(page.locator(chip)).toHaveCount(0)
  await expect(
    row(page, movie.slug).getByRole('button', {
      name: 'Retirer de la watchlist',
      exact: true,
    }),
  ).toBeEnabled()
  await expect(page.locator(chip)).toHaveCount(2)
})
