import type { APIRequestContext, Page } from '@playwright/test'
import type { WatchedPreferences } from '../app/types/watched'
import type { AccountWatchlist } from '../app/types/watchlist'
import { test, expect, openPage } from './fixtures'
import {
  date,
  movie,
  theater,
  hiddenWatchedSlugs,
  watchedDefaults,
  releaseHistory,
  upcomingReleases,
} from './data.mjs'

const detail = `/film/${movie.slug}`
interface WatchedFixtureUpdate {
  slugs?: string[]
  preferences?: Partial<WatchedPreferences>
  watchlistPreferences?: Pick<
    AccountWatchlist,
    'sort_order' | 'view_mode' | 'filter_tag_id'
  >
  watchedRead?: '' | 'error'
  watchedWrite?: 'watched-conflict'
  username?: string
}
declare global {
  interface Window {
    __watchedMessages?: string[]
  }
}
const complete = {
  enabled: true,
  state: 'complete',
  selected_ids: [theater.id],
}
const badge = '[role="img"][aria-label="Film déjà vu"]'
const mark = (page: Page) =>
  page.getByRole('button', { name: 'Marquer comme vu', exact: true })
const unmark = (page: Page) =>
  page.getByRole('button', { name: 'Marquer comme non vu', exact: true })
const state = async (request: APIRequestContext) =>
  (await request.get('/__playwright/state')).json()
const seed = async (request: APIRequestContext, extra = {}) => {
  expect(
    (
      await request.post('/__playwright/scenario', {
        data: { ...complete, ...extra },
      })
    ).ok(),
  ).toBeTruthy()
}
const update = async (
  request: APIRequestContext,
  data: WatchedFixtureUpdate,
) => {
  expect(
    (await request.post('/__playwright/watched', { data })).ok(),
  ).toBeTruthy()
}
async function focus(page: Page) {
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
}
async function overflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
}
async function showFilters(page: Page) {
  if ((page.viewportSize()?.width ?? 1440) < 1024)
    await page.getByRole('button', { name: /^Modifier les filtres/ }).click()
  const advanced = page.getByRole('button', { name: /options avancées/i })
  if (
    (await advanced.count()) &&
    (await advanced.getAttribute('aria-expanded')) !== 'true'
  )
    await advanced.click()
}

test('canonical detail toggle is adjacent, keyboard safe, persisted, managed and private', async ({
  page,
  request,
}, info) => {
  const hydration: string[] = []
  page.on('console', (message) => {
    if (/hydration.*(mismatch|contains)/i.test(message.text()))
      hydration.push(message.text())
  })
  await seed(request, { canonicalAlias: true })
  const html = await (await request.get(detail)).text()
  expect(html).not.toMatch(
    /watched_at|auto_remove_watchlist|expected_watchlist_revision/,
  )
  await openPage(page, '/film/watched-alias')
  await expect(mark(page)).toHaveAttribute('aria-pressed', 'false')
  // Semantic region is deliberately a plain adjacent action container, not another tab stop.
  expect(await page.locator('[aria-label="Ma collection"] > div').count()).toBe(
    2,
  )
  for (const button of [
    mark(page),
    page.getByRole('button', { name: 'Ajouter à la watchlist', exact: true }),
  ]) {
    const box = await button.boundingBox()
    expect(box!.width).toBeGreaterThanOrEqual(44)
    expect(box!.height).toBeGreaterThanOrEqual(44)
  }
  await mark(page).focus()
  await page.keyboard.press('Enter')
  await expect(unmark(page)).toHaveAttribute('aria-pressed', 'true')
  await expect(unmark(page)).toBeFocused()
  await expect(page.locator(badge)).toHaveCount(1)
  expect((await state(request)).watched.items[0].slug).toBe(movie.slug)
  await page.screenshot({
    path: info.outputPath('watched-detail.png'),
    fullPage: true,
  })
  await page.reload()
  await expect(unmark(page)).toHaveAttribute('aria-pressed', 'true')
  await page.goto('/compte')
  await page
    .getByRole('navigation', { name: 'Rubriques du compte' })
    .getByRole('link', { name: 'Films vus', exact: true })
    .click()
  await expect(
    page.getByRole('heading', { name: 'Films vus', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('list', { name: 'Films vus' }).locator(badge),
  ).toHaveCount(1)
  await overflow(page)
  await page.screenshot({
    path: info.outputPath('watched-settings.png'),
    fullPage: true,
  })
  const privateResponse = await request.get('/compte/films-vus')
  expect(privateResponse.headers()['cache-control']).toContain('no-store')
  expect(privateResponse.headers()['x-robots-tag']).toContain('noindex')
  expect(privateResponse.headers()['referrer-policy']).toBe('no-referrer')
  const privateHtml = await privateResponse.text()
  expect(privateHtml).not.toMatch(/watched_at|auto_remove_watchlist/)
  await page.getByRole('button', { name: /Marquer .* comme non vu/ }).click()
  await expect(page.getByText('Aucun film marqué comme vu.')).toBeVisible()
  expect((await state(request)).watchedPosts).toBe(2)
  const storage = await page.evaluate(() =>
    JSON.stringify({
      local: { ...localStorage },
      session: { ...sessionStorage },
    }),
  )
  expect(storage).not.toMatch(/watched|film-playwright|fixture_alice|Private/)
  const data = await state(request)
  expect(
    JSON.stringify(
      data.publicQueries.map((entry: { query: unknown }) => entry.query),
    ),
  ).not.toMatch(/watched|expected_|fixture_alice|visibility/)
  expect(hydration, 'private snapshots must not race server hydration').toEqual(
    [],
  )
})

for (const [accountState, destination] of [
  ['anonymous', '/connexion'],
  ['pending_email', '/verification'],
  ['pending_username', '/finaliser'],
])
  test(`${accountState} navigates without queue or watched POST`, async ({
    page,
    request,
  }) => {
    await seed(request, { state: accountState })
    await openPage(page, detail)
    await expect(mark(page)).toBeEnabled()
    await mark(page).click()
    await expect(page).toHaveURL(new RegExp(`${destination}$`))
    expect((await state(request)).watchedPosts).toBe(0)
    expect((await state(request)).watchedGets).toBe(0)
  })
test('disabled accounts retain unpersonalized discovery and hide collection action', async ({
  page,
  request,
}) => {
  await seed(request, {
    enabled: false,
    state: 'anonymous',
    watchedSlugs: [movie.slug],
  })
  await openPage(page, detail)
  await expect(mark(page)).toHaveCount(0)
  await openPage(page, '/films')
  await expect(page.locator('[data-catalog-movie]')).toHaveCount(1)
  await expect(page.locator(badge)).toHaveCount(0)
})

test('auto removal is opt-in, non-retrospective and commits both resources without disturbing unrelated tags or preferences', async ({
  page,
  request,
}) => {
  await seed(request, { watchlist: true })
  await update(request, {
    watchlistPreferences: {
      view_mode: 'tags',
      filter_tag_id: '1',
      sort_order: 'title_asc',
    },
  })
  await openPage(page, detail)
  const before = (await state(request)).watchlist
  await mark(page).click()
  await expect(unmark(page)).toBeEnabled()
  expect((await state(request)).watchlist).toEqual(before)
  await openPage(page, '/compte/films-vus')
  await page.getByRole('checkbox', { name: /Retirer de la watchlist/ }).check()
  expect((await state(request)).watchedPreferencePosts).toBe(0)
  await page
    .getByRole('button', { name: 'Enregistrer les préférences' })
    .click()
  await expect(page.getByText('Préférences enregistrées.')).toBeVisible()
  expect((await state(request)).watchlist).toEqual(before)
  await openPage(page, detail)
  await unmark(page).click()
  await expect(mark(page)).toBeEnabled()
  await mark(page).click()
  await expect(unmark(page)).toBeEnabled()
  const after = (await state(request)).watchlist
  expect(after.items.map((item: { slug: string }) => item.slug)).not.toContain(
    movie.slug,
  )
  expect(after.items).toEqual(
    before.items.filter((item: { slug: string }) => item.slug !== movie.slug),
  )
  expect(after.tags).toEqual(before.tags)
  expect(after.sort_order).toBe(before.sort_order)
  expect(after.view_mode).toBe(before.view_mode)
  expect(after.filter_tag_id).toBe(before.filter_tag_id)
  await page
    .getByRole('button', { name: 'Ajouter à la watchlist', exact: true })
    .click()
  await expect(
    page.getByRole('button', { name: 'Retirer de la watchlist', exact: true }),
  ).toBeEnabled()
  expect(
    (await state(request)).watched.items.map(
      (item: { slug: string }) => item.slug,
    ),
  ).toContain(movie.slug)
})

const policies = [
  {
    key: 'films',
    path: '/films',
    target: '[data-catalog-movie="film-playwright"]',
    slug: movie.slug,
  },
  {
    key: 'home',
    path: '/',
    target:
      'ul[aria-label="Films à l’affiche"] a[href="/film/film-playwright"]',
    slug: movie.slug,
  },
  {
    key: 'upcoming',
    path: '/films/prochainement',
    target: '[data-catalog-movie="film-301"]',
    slug: 'film-301',
  },
  {
    key: 'cinema_city',
    path: `/cinema/${theater.slug}`,
    target: '[data-catalog-movie="film-playwright"]',
    slug: movie.slug,
  },
  {
    key: 'search',
    path: `/recherche?theaters=${theater.id}&date=${date}&start_after=12:00&finish_before=18:00`,
    target: 'main a[href="/film/film-playwright"]',
    slug: movie.slug,
  },
  {
    key: 'planning',
    path: `/planning?date=${date}`,
    target:
      'button[aria-controls="timeline-showtime-inspector"][aria-label^="Film Playwright,"]',
    slug: movie.slug,
  },
]
for (const policy of policies)
  test(`${policy.key} saved hide/show overrides and inherit obey both global defaults`, async ({
    page,
    request,
  }) => {
    for (const [global, override, shown] of [
      ['hide', 'inherit', false],
      ['hide', 'show', true],
      ['show', 'hide', false],
      ['show', 'inherit', true],
    ] as const) {
      await seed(request, {
        discovery: 'populated',
        watchedCatalog: policy.key === 'search' || policy.key === 'planning',
        watchedSlugs: [policy.slug],
        watchedPreferences: {
          default_visibility: global,
          [`${policy.key}_visibility`]: override,
        },
      })
      await openPage(page, policy.path)
      if (shown) {
        await expect(page.locator(policy.target).first()).toBeVisible()
        if (policy.key === 'planning')
          await page.locator(policy.target).first().click()
        await expect(page.locator(badge).first()).toBeVisible()
      } else {
        await expect(page.locator(policy.target)).toHaveCount(0)
      }
      expect((await state(request)).watchedPreferencePosts).toBe(0)
    }
  })

test('films and city walk hidden-only first and final public pages before local 24 slicing, totals, clamp and browser history', async ({
  page,
  request,
}) => {
  await seed(request, {
    watchedCatalog: true,
    watchedSlugs: hiddenWatchedSlugs,
  })
  for (const path of ['/films', '/ville/lille/cinemas']) {
    await openPage(page, `${path}?sort=title_asc&page=99`)
    await expect(page).toHaveURL(
      new RegExp(`${path.replaceAll('/', '\\/')}\\?sort=title_asc&page=2$`),
    )
    await expect(page.locator('[data-catalog-movie]')).toHaveCount(6)
    await expect(page.getByText('30 films', { exact: true })).toBeVisible()
    await expect(
      page.locator('[data-catalog-movie="film-watched-129"]'),
    ).toBeVisible()
    expect(await page.locator(badge).count()).toBe(0)
    await page
      .getByRole('link', { name: /Précédent/ })
      .last()
      .click()
    await expect(page.locator('[data-catalog-movie]')).toHaveCount(24)
    await page.goBack()
    await expect(page.locator('[data-catalog-movie]')).toHaveCount(6)
  }
  const queries = (await state(request)).publicQueries.filter(
    (item: { path: string }) => item.path === '/api/v1/movies',
  )
  const walks = queries.filter(
    (item: { query: Record<string, string> }) => item.query.page_size === '100',
  )
  expect(
    walks.some(
      (item: { query: Record<string, string> }) => item.query.page === '3',
    ),
  ).toBe(true)
  expect(
    walks.every(
      (item: { query: Record<string, string> }) =>
        item.query.sort === 'title_asc',
    ),
  ).toBe(true)
})
test('home refills six and cinema all-page films filter before counts', async ({
  page,
  request,
}) => {
  await seed(request, {
    watchedCatalog: true,
    watchedSlugs: hiddenWatchedSlugs,
  })
  await openPage(page, '/')
  await expect(
    page.getByRole('list', { name: 'Films à l’affiche' }).locator('li'),
  ).toHaveCount(6)
  await expect(
    page.getByRole('list', { name: 'Films à l’affiche' }),
  ).toContainText('Inédit 1')
  await openPage(page, `/cinema/${theater.slug}?view=films`)
  await expect(page.locator('[data-catalog-movie]')).toHaveCount(30)
  await expect(
    page.locator('[data-catalog-movie="film-watched-129"]'),
  ).toBeVisible()
  await overflow(page)
})
test('upcoming/history reflow four nonempty release weeks, local totals and empty sets', async ({
  page,
  request,
}) => {
  await seed(request, { watchedSlugs: ['film-301', 'film-204'] })
  await openPage(page, '/films/prochainement')
  await expect(page.locator('section[aria-labelledby^="week-"]')).toHaveCount(4)
  await expect(page.locator('[data-catalog-movie="film-305"]')).toBeVisible()
  await expect(page.getByText('4 films', { exact: true })).toBeVisible()
  await openPage(page, '/films/prochainement?vue=historique&annee=2025&page=2')
  await expect(page.locator('[data-catalog-movie="film-204"]')).toHaveCount(0)
  await expect(page.locator('section[aria-labelledby^="week-"]')).toHaveCount(3)
  await expect(page.getByRole('combobox', { name: /Année/ })).toHaveValue(
    '2025',
  )
  await update(request, {
    slugs: [...releaseHistory, ...upcomingReleases].map((item) => item.slug),
  })
  await focus(page)
  await expect(
    page.getByText('Aucune sortie pour cette période', { exact: true }),
  ).toBeVisible()
})
test('planning refills hidden-only theater batch, preserves query/window and both modes, closes hidden inspector', async ({
  page,
  request,
}) => {
  await seed(request, {
    watchedCatalog: true,
    watchedSlugs: [movie.slug],
    selected_ids: [],
  })
  await openPage(page, `/planning?date=${date}&format=2D&zoom=15`)
  const buttons = page.locator(
    'button[aria-controls="timeline-showtime-inspector"]',
  )
  await expect(buttons).toHaveCount(2)
  expect(
    (await state(request)).timelineQueries.map(
      (query: Record<string, string>) => query.theaters.split(',').length,
    ),
  ).toEqual([10, 2])
  await buttons.first().click()
  await expect(page.locator('#timeline-showtime-inspector')).toBeVisible()
  await update(request, { slugs: [movie.slug, 'film-watched-100'] })
  await focus(page)
  await expect(page.locator('#timeline-showtime-inspector')).toHaveCount(0)
  expect(page.url()).toContain('format=2D')
  expect(page.url()).toContain('zoom=15')
  await update(request, {
    slugs: [movie.slug],
    preferences: { planning_visibility: 'show' },
  })
  await focus(page)
  await expect(buttons).toHaveCount(12)
  await page.getByRole('button', { name: 'Par film', exact: true }).click()
  await expect(page).toHaveURL(/mode=movie/)
  await expect(buttons).toHaveCount(12)
})

test('search temporary true and false overrides saved policy without writes or shared URL changes; hidden selection cannot constrain compatibility or bypass watchlist', async ({
  page,
  request,
}) => {
  await seed(request, {
    watchedCatalog: true,
    watchedSlugs: [movie.slug],
    watchlist: true,
    selected_ids: [theater.id, 'fixture-watched-10'],
  })
  const path = `/recherche?theaters=${theater.id},fixture-watched-10&date=${date}&start_after=12:00&finish_before=18:00&selected=u800000`
  await openPage(page, path)
  const visible = page.getByRole('link', { name: /Inédit 1/ })
  await expect(visible.first()).toBeVisible()
  const before = page.url()
  await showFilters(page)
  const toggle = page.getByRole('checkbox', {
    name: 'Afficher les films vus',
    exact: true,
  })
  await expect(toggle).not.toBeChecked()
  await toggle.check()
  await expect(page.locator(badge).first()).toBeVisible()
  expect(page.url()).toBe(before)
  await toggle.uncheck()
  await expect(page.locator(badge)).toHaveCount(0)
  await page.getByRole('checkbox', { name: 'Ma watchlist uniquement' }).check()
  await expect(
    page.getByText(
      'Aucune séance de votre watchlist ne correspond à ces filtres.',
    ),
  ).toBeVisible()
  await update(request, { preferences: { search_visibility: 'show' } })
  await focus(page)
  // Explicit false remains in memory even when the saved default becomes show.
  await expect(toggle).not.toBeChecked()
  await openPage(page, path)
  await showFilters(page)
  await expect(toggle).toBeChecked()
  await toggle.uncheck()
  await expect(page.locator(badge)).toHaveCount(0)
  expect((await state(request)).watchedPreferencePosts).toBe(0)
})

for (const failure of [
  'watched-conflict',
  'watchlist-conflict',
  'commit-error',
])
  test(`${failure} reconciles both GETs without replay and keeps explicit editor intent`, async ({
    page,
    request,
  }) => {
    await seed(request, {
      watchlist: true,
      watchedPreferences: { auto_remove_watchlist: true },
      watchedWrite: failure,
    })
    await openPage(page, detail)
    await expect(mark(page)).toBeEnabled()
    const before = await state(request)
    await mark(page).click()
    await expect(page.getByRole('alert')).toContainText(
      failure === 'watchlist-conflict'
        ? 'watchlist a changé'
        : failure === 'watched-conflict'
          ? 'films vus'
          : 'indisponibles',
    )
    const after = await state(request)
    expect(after.watchedPosts).toBe(1)
    expect(after.watchedGets).toBeGreaterThan(before.watchedGets)
    expect(after.watchlistGets).toBeGreaterThan(before.watchlistGets)
    if (failure === 'commit-error') await expect(unmark(page)).toBeEnabled()
    else {
      await expect(mark(page)).toBeEnabled()
      await mark(page).click()
      await expect(unmark(page)).toBeEnabled()
    }
  })
test('segmented visibility preferences align, retain native keyboard behavior and only save explicitly', async ({
  page,
  request,
}, info) => {
  await seed(request)
  await openPage(page, '/compte/films-vus')
  const form = page.getByRole('form', { name: 'Préférences des films vus' })
  const names = [
    'Recherche',
    'Planning',
    'Films à l’affiche',
    'Prochaines sorties et historique',
    'Accueil',
    'Cinémas et villes',
  ]
  const groups = names.map((name) =>
    form.getByRole('group', { name, exact: true }),
  )
  const global = form.getByRole('group', {
    name: 'Par défaut dans l’application',
    exact: true,
  })
  const defaultHide = global.getByRole('radio', {
    name: 'Masquer',
    exact: true,
  })
  const defaultShow = global.getByRole('radio', {
    name: 'Afficher',
    exact: true,
  })
  await expect(defaultHide).toBeChecked()
  await expect(global.getByRole('radio')).toHaveCount(2)
  await expect(form.getByRole('radio')).toHaveCount(20)
  await expect(global.locator('label span')).toHaveText(['Masquer', 'Afficher'])
  const formBox = await form.boundingBox()
  expect(formBox).not.toBeNull()
  for (const group of groups) {
    await expect(group.locator('label span')).toHaveText([
      'Par défaut',
      'Afficher',
      'Masquer',
    ])
  }
  for (const group of [global, ...groups]) {
    const label = await group.locator(':scope > div > span').boundingBox()
    const controls = await group.locator(':scope > div > div').boundingBox()
    expect(label).not.toBeNull()
    expect(controls).not.toBeNull()
    expect(label!.x).toBeCloseTo(formBox!.x, 0)
    if (info.project.name === 'desktop') {
      expect(controls!.x + controls!.width).toBeCloseTo(
        formBox!.x + formBox!.width,
        0,
      )
      expect(label!.x + label!.width).toBeLessThanOrEqual(controls!.x)
      expect(label!.y + label!.height / 2).toBeCloseTo(
        controls!.y + controls!.height / 2,
        0,
      )
    } else {
      expect(controls!.x).toBeCloseTo(label!.x, 0)
      expect(label!.y + label!.height).toBeLessThanOrEqual(controls!.y)
    }
  }
  for (const radio of await form.getByRole('radio').all()) {
    const box = await radio.boundingBox()
    expect(box!.width).toBeGreaterThanOrEqual(44)
    expect(box!.height).toBeGreaterThanOrEqual(44)
    await expect(radio).toHaveCSS('opacity', '0')
  }
  await defaultHide.focus()
  await page.keyboard.press('ArrowRight')
  await expect(defaultShow).toBeChecked()
  await expect(defaultShow).toBeFocused()
  await expect(defaultHide).not.toBeChecked()
  await page.keyboard.press('ArrowLeft')
  await expect(defaultHide).toBeChecked()
  await page.keyboard.press('Tab')
  for (const group of groups) {
    const inherit = group.getByRole('radio', {
      name: 'Par défaut',
      exact: true,
    })
    const show = group.getByRole('radio', { name: 'Afficher', exact: true })
    const hide = group.getByRole('radio', { name: 'Masquer', exact: true })
    await expect(inherit).toBeFocused()
    await expect(inherit).toBeChecked()
    await page.keyboard.press('ArrowRight')
    await expect(show).toBeChecked()
    await expect(show).toBeFocused()
    await expect(inherit).not.toBeChecked()
    const selected = await show.locator('+ span').evaluate((element) => ({
      background: getComputedStyle(element).backgroundColor,
      outline: getComputedStyle(element).outlineStyle,
      width: getComputedStyle(element).outlineWidth,
    }))
    expect(selected.background).not.toBe(
      await inherit
        .locator('+ span')
        .evaluate((element) => getComputedStyle(element).backgroundColor),
    )
    expect(selected.outline).toBe('solid')
    expect(Number.parseFloat(selected.width)).toBeGreaterThanOrEqual(2)
    await page.keyboard.press('ArrowRight')
    await expect(hide).toBeChecked()
    await expect(show).not.toBeChecked()
    await page.keyboard.press('ArrowRight')
    await expect(inherit).toBeChecked()
    await expect(hide).not.toBeChecked()
    await page.keyboard.press('Tab')
  }
  const autoRemove = form.getByRole('checkbox', {
    name: /Retirer de la watchlist/,
  })
  const checkCheckboxCenter = async (wrapped: boolean) => {
    const checkbox = await autoRemove.boundingBox()
    const text = await autoRemove.locator('+ span').boundingBox()
    const target = await autoRemove.locator('xpath=..').boundingBox()
    expect(checkbox!.y + checkbox!.height / 2).toBeCloseTo(
      text!.y + text!.height / 2,
      0,
    )
    expect(target!.width).toBeGreaterThanOrEqual(44)
    expect(target!.height).toBeGreaterThanOrEqual(44)
    if (wrapped) {
      const lineHeight = await autoRemove
        .locator('+ span')
        .evaluate((element) =>
          Number.parseFloat(getComputedStyle(element).lineHeight),
        )
      expect(text!.height).toBeGreaterThan(lineHeight)
    }
  }
  await checkCheckboxCenter(info.project.name === 'mobile')
  const save = form.getByRole('button', { name: 'Enregistrer les préférences' })
  await expect(autoRemove).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(save).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(autoRemove).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(
    groups[5]!.getByRole('radio', { name: 'Par défaut', exact: true }),
  ).toBeFocused()
  await defaultShow.check()
  await groups[0]!.getByRole('radio', { name: 'Afficher', exact: true }).check()
  expect((await state(request)).watchedPreferencePosts).toBe(0)
  if (info.project.name === 'mobile') {
    await page.setViewportSize({ width: 320, height: 844 })
    await overflow(page)
    const narrowForm = await form.boundingBox()
    for (const group of [global, ...groups]) {
      const label = await group.locator(':scope > div > span').boundingBox()
      const controls = await group.locator(':scope > div > div').boundingBox()
      expect(label!.x).toBeCloseTo(narrowForm!.x, 0)
      expect(controls!.x).toBeCloseTo(label!.x, 0)
      expect(controls!.x + controls!.width).toBeLessThanOrEqual(
        narrowForm!.x + narrowForm!.width,
      )
    }
    await checkCheckboxCenter(true)
    await page.setViewportSize({ width: 390, height: 844 })
  }
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/v1/account/watched/preferences', async (route) => {
    await held
    await route.continue()
  })
  try {
    await save.click()
    await expect(save).toBeDisabled()
    for (const radio of await form.getByRole('radio').all())
      await expect(radio).toBeDisabled()
    await expect(autoRemove).toBeDisabled()
    await expect(defaultShow).toBeChecked()
    await expect(defaultShow.locator('+ span')).toHaveCSS('opacity', '0.55')
    expect((await state(request)).watchedPreferencePosts).toBe(0)
  } finally {
    release()
  }
  await expect(page.getByText('Préférences enregistrées.')).toBeVisible()
  expect((await state(request)).watchedPreferencePosts).toBe(1)
  expect((await state(request)).watched.preferences).toEqual({
    ...watchedDefaults,
    default_visibility: 'show',
    search_visibility: 'show',
  })
  await page.reload()
  await expect(defaultShow).toBeChecked()
  await expect(
    groups[0]!.getByRole('radio', { name: 'Afficher', exact: true }),
  ).toBeChecked()
  await overflow(page)
  await page.screenshot({
    path: info.outputPath('watched-segmented-preferences.png'),
    fullPage: true,
  })
})

test('preference conflict preserves edits and retries only on a new explicit submit', async ({
  page,
  request,
}) => {
  await seed(request)
  await openPage(page, '/compte/films-vus')
  const defaults = page.getByRole('group', {
    name: 'Par défaut dans l’application',
    exact: true,
  })
  await defaults.getByRole('radio', { name: 'Afficher', exact: true }).check()
  const groups = [
    'Recherche',
    'Planning',
    'Films à l’affiche',
    'Prochaines sorties et historique',
    'Accueil',
    'Cinémas et villes',
  ]
  for (const name of groups)
    await page
      .getByRole('group', { name, exact: true })
      .getByRole('radio', { name: 'Masquer', exact: true })
      .check()
  await update(request, { watchedWrite: 'watched-conflict' })
  await page
    .getByRole('button', { name: 'Enregistrer les préférences' })
    .click()
  await expect(page.getByRole('alert')).toContainText('films vus')
  await expect(
    defaults.getByRole('radio', { name: 'Afficher', exact: true }),
  ).toBeChecked()
  expect((await state(request)).watchedPreferencePosts).toBe(1)
  await page
    .getByRole('button', { name: 'Enregistrer les préférences' })
    .click()
  await expect(page.getByText('Préférences enregistrées.')).toBeVisible()
  expect((await state(request)).watched.preferences.default_visibility).toBe(
    'show',
  )
  expect((await state(request)).watched.preferences).toEqual({
    ...watchedDefaults,
    default_visibility: 'show',
    search_visibility: 'hide',
    planning_visibility: 'hide',
    films_visibility: 'hide',
    upcoming_visibility: 'hide',
    home_visibility: 'hide',
    cinema_city_visibility: 'hide',
  })
  await page.reload()
  for (const name of groups)
    await expect(
      page
        .getByRole('group', { name, exact: true })
        .getByRole('radio', { name: 'Masquer', exact: true }),
    ).toBeChecked()
})

test('initial private read is unknown, not unwatched; post-commit transport timeout reads both snapshots without replay', async ({
  page,
  request,
}) => {
  await seed(request, {
    watchlist: true,
    watchedPreferences: { auto_remove_watchlist: true },
  })
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  let entered!: () => void
  const intercepted = new Promise<void>((resolve) => {
    entered = resolve
  })
  await page.route('**/api/v1/account/watched', async (route) => {
    const response = await route.fetch()
    if (route.request().method() === 'GET') {
      entered()
      await held
      await route.fulfill({ response })
    } else await route.abort('timedout')
  })
  await openPage(page, detail)
  await intercepted
  const unknown = page.getByRole('button', {
    name: 'Films vus indisponibles pendant la vérification',
  })
  await expect(unknown).toBeDisabled()
  await expect(unknown).not.toHaveAttribute('aria-pressed')
  release()
  await expect(mark(page)).toBeEnabled()
  const before = await state(request)
  await mark(page).click()
  await expect(unmark(page)).toBeEnabled()
  const after = await state(request)
  expect(after.watchedPosts).toBe(1)
  expect(after.watchedGets).toBeGreaterThan(before.watchedGets)
  expect(after.watchlistGets).toBeGreaterThan(before.watchlistGets)
  expect(
    after.watchlist.items.map((item: { slug: string }) => item.slug),
  ).not.toContain(movie.slug)
})
test('inconsistent public walk produces retryable error rather than a partial catalog', async ({
  page,
  request,
}) => {
  await seed(request, {
    watchedCatalog: true,
    watchedSlugs: hiddenWatchedSlugs,
  })
  await page.route('**/api/v1/movies?**', async (route) => {
    const response = await route.fetch()
    const value = await response.json()
    if (value.page === 2) value.catalog_revision = 'drifted'
    await route.fulfill({ response, json: value })
  })
  await openPage(page, '/films')
  await expect(page.getByRole('alert')).toContainText('Réessayer')
  await expect(page.locator('[data-catalog-movie]')).toHaveCount(0)
  await page.unroute('**/api/v1/movies?**')
  await page.getByRole('button', { name: 'Réessayer', exact: true }).click()
  await expect(page.locator('[data-catalog-movie]')).toHaveCount(24)
})

test('initial private failure withholds affected catalog until explicit retry, never fail-open', async ({
  page,
  request,
}) => {
  await seed(request, { watchedRead: 'error', watchedSlugs: [movie.slug] })
  await openPage(page, '/films')
  await expect(page.getByRole('alert')).toContainText(
    'Impossible de préparer vos résultats',
  )
  await expect(page.locator('[data-catalog-movie]')).toHaveCount(0)
  await update(request, { watchedRead: '' })
  await page.getByRole('button', { name: 'Réessayer', exact: true }).click()
  await expect(
    page.getByText('Aucun film à l’affiche actuellement.'),
  ).toBeVisible()
})
test('pending watched admission and revalidation keep committed labels but disable both writes; owner switch and pagehide fence delayed data', async ({
  page,
  request,
}) => {
  await seed(request, {
    watchedSlugs: [movie.slug],
    watchedPreferences: { default_visibility: 'show' },
  })
  await openPage(page, detail)
  await expect(unmark(page)).toBeEnabled()
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  let entered!: () => void
  const intercepted = new Promise<void>((resolve) => {
    entered = resolve
  })
  let finished!: () => void
  const completeHandler = new Promise<void>((resolve) => {
    finished = resolve
  })
  await page.route('**/api/v1/account/watched', async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const response = await route.fetch()
    entered()
    await held
    try {
      await route.fulfill({ response })
    } catch (error) {
      if (!route.request().failure()) throw error
    } finally {
      finished()
    }
  })
  await focus(page)
  await intercepted
  await expect(unmark(page)).toBeDisabled()
  await expect(
    page.getByRole('button', { name: 'Ajouter à la watchlist', exact: true }),
  ).toBeDisabled()
  await update(request, { username: 'fixture_bob' })
  await page.evaluate(() => window.dispatchEvent(new Event('pagehide')))
  await expect(page.locator(badge)).toHaveCount(0)
  release()
  await completeHandler
  await page.unroute('**/api/v1/account/watched')
  await page.evaluate(() =>
    window.dispatchEvent(
      new PageTransitionEvent('pageshow', { persisted: true }),
    ),
  )
  await expect(mark(page)).toBeEnabled()
  await expect(page.locator(badge)).toHaveCount(0)
  expect((await state(request)).watchedPosts).toBe(0)
})
test('cross-tab notifications carry no identifiers, revalidate on focus, and offline clears private state', async ({
  page,
  request,
  context,
}) => {
  await seed(request)
  await page.addInitScript(() => {
    const Native = window.BroadcastChannel
    const messages: string[] = []
    window.__watchedMessages = messages
    window.BroadcastChannel = class extends Native {
      postMessage(value: string) {
        messages.push(value)
        super.postMessage(value)
      }
    }
  })
  await openPage(page, detail)
  await expect(mark(page)).toBeEnabled()
  const other = await context.newPage()
  await openPage(other, detail)
  await expect(mark(other)).toBeEnabled()
  await mark(page).click()
  await expect(unmark(page)).toBeEnabled()
  await expect(unmark(other)).toBeEnabled()
  const messages = await page.evaluate(() => window.__watchedMessages ?? [])
  expect(messages).toContain('watched-changed')
  for (const message of messages)
    expect([
      'changed',
      'watched-changed',
      'watchlist-changed',
      'theaters-changed',
      'theater-follows-changed',
    ]).toContain(message)
  expect(JSON.stringify(messages)).not.toMatch(
    /fixture_|film-|revision|visibility/,
  )
  await page.evaluate(() => window.dispatchEvent(new Event('offline')))
  await expect(page.locator(badge)).toHaveCount(0)
  await other.close()
})
test('shown watched badges survive saved list/tag/search, account and cinema activity and public fallbacks without filtering history', async ({
  page,
  request,
}) => {
  await seed(request, {
    watchlist: true,
    watchedSlugs: [movie.slug],
    watchedPreferences: { default_visibility: 'show' },
    theater_ids: [theater.id],
  })
  await openPage(page, '/compte/watchlist')
  const row = page
    .locator('li')
    .filter({ has: page.locator(`a[href="${detail}"]`) })
  await expect(row.locator(badge)).toHaveCount(1)
  await openPage(page, '/compte/activite')
  await expect(page.locator(badge)).toHaveCount(2)
  await openPage(page, `/cinema/${theater.slug}?view=activity`)
  await expect(page.locator(badge)).toHaveCount(2)
  await openPage(page, '/films')
  await expect(page.locator('[data-catalog-movie]')).not.toHaveCount(0)
  await expect(page.locator(badge)).toHaveCount(1)
  await openPage(page, '/admin/login')
  await expect(page.locator(badge)).toHaveCount(0)
})
