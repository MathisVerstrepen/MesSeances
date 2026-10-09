import type { Page } from '@playwright/test'
import { test, expect, openPage } from './fixtures'
import { date, movie, showtimes, theater } from './data.mjs'

const account = {
  enabled: true,
  state: 'complete',
  watchlist: true,
  selected_ids: [theater.id],
}
const query = new URLSearchParams({
  theaters: theater.id,
  date,
  start_after: '12:00',
  finish_before: '18:00',
})

async function mockSlotSearch(page: Page) {
  const queries: Record<string, string>[] = []
  // Slot searches start after hydration; SSR account/cinema data uses the fixture server.
  await page.route('**/api/v1/search/slot?**', async (route) => {
    const params = new URL(route.request().url()).searchParams
    queries.push(Object.fromEntries(params))
    const showtime = showtimes().showtimes[0]!
    await route.fulfill({
      json: [movie, { ...movie, slug: 'other-film', title: 'Autre film' }].map(
        (item, index) => ({
          showtime: {
            ...showtime,
            id: `ugc-showing-${100 + index}`,
            movie: item,
          },
          theater,
          poster_url: null,
          backdrop_url: null,
          effective_start_time: showtime.start_time,
          effective_end_time: showtime.end_time,
          buffer_ads_minutes: 15,
          slack_before_minutes: 120,
          slack_after_minutes: 150,
        }),
      ),
    })
  })
  return queries
}

async function openFilters(page: Page) {
  if ((page.viewportSize()?.width ?? 1440) < 1024)
    await page.getByRole('button', { name: /^Modifier les filtres/ }).click()
  return page.locator('#search-filters')
}

test('advanced filters start collapsed, support keyboard and preserve hidden values on search', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', { data: account })
  const searches = await mockSlotSearch(page)
  await openPage(page, '/recherche')
  const form = page.locator('#search-filters')
  const disclosure = form.getByRole('button', { name: 'Options avancées' })
  const panel = form.locator('#search-advanced-options')
  const format = form.getByLabel(/^Format/)
  const ads = form.getByLabel('Inclure les publicités (+15 min)')
  const watchlist = form.getByLabel('Ma watchlist uniquement')
  const submit = form.getByRole('button', { name: 'Trouver une séance' })

  await expect(disclosure).toHaveAttribute('aria-expanded', 'false')
  await expect(disclosure).toHaveAttribute(
    'aria-controls',
    'search-advanced-options',
  )
  await expect(panel).toBeHidden()
  await expect(format).toHaveCount(1)
  await expect(format).toBeHidden()
  await expect(ads).toBeHidden()
  await expect(watchlist).toBeHidden()
  await expect(
    form.getByRole('combobox', { name: 'Langue', exact: true }),
  ).toBeVisible()
  await expect(
    form.getByRole('combobox', { name: 'À partir de' }),
  ).toBeVisible()
  await expect(
    form.getByRole('combobox', { name: 'Terminé avant' }),
  ).toBeVisible()
  await expect(
    form.getByRole('group', { name: 'Date de la séance' }),
  ).toBeVisible()
  await expect(form.getByRole('group', { name: 'Cinémas' })).toBeVisible()
  await expect(submit).toBeEnabled()
  await disclosure.focus()
  await page.keyboard.press('Tab')
  await expect(submit).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(disclosure).toBeFocused()
  await page.screenshot({
    path: info.outputPath('search-collapsed.png'),
    fullPage: true,
  })

  await page.keyboard.press('Enter')
  await expect(disclosure).toHaveAttribute('aria-expanded', 'true')
  await expect(format).toHaveValue('ALL')
  await expect(ads).toBeChecked()
  await expect(watchlist).not.toBeChecked()
  await expect(watchlist).toBeEnabled()
  await page.keyboard.press('Tab')
  await expect(format).toBeFocused()
  await format.selectOption('2D')
  await ads.uncheck()
  await watchlist.check()
  await form
    .getByRole('combobox', { name: 'À partir de' })
    .selectOption('12:00')
  await form
    .getByRole('combobox', { name: 'Terminé avant' })
    .selectOption('18:00')
  await page.screenshot({
    path: info.outputPath('search-expanded.png'),
    fullPage: true,
  })
  await disclosure.focus()
  await page.keyboard.press('Space')
  await expect(disclosure).toHaveAttribute('aria-expanded', 'false')
  await expect(panel).toBeHidden()
  expect(searches).toHaveLength(0)
  await submit.click()
  await expect(
    page.getByRole('heading', { name: 'Ma watchlist', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'Autres films', exact: true }),
  ).toHaveCount(0)
  await expect(page).toHaveURL(/format=2D/)
  await expect(page).toHaveURL(/include_ads=0/)
  expect(searches).toHaveLength(1)
  expect(searches[0]).toMatchObject({
    theaters: theater.id,
    date,
    start_after: '12:00',
    finish_before: '18:00',
    format: '2D',
    language: 'ALL',
    include_ads: 'false',
    buffer_ads: '15',
  })

  await openFilters(page)
  await expect(disclosure).toHaveAttribute('aria-expanded', 'false')
  await disclosure.click()
  await expect(format).toHaveValue('2D')
  await expect(ads).not.toBeChecked()
  await expect(watchlist).toBeChecked()
  const beforeToggle = page.url()
  await watchlist.uncheck()
  await expect(
    page.getByRole('heading', { name: 'Autres films', exact: true }),
  ).toBeVisible()
  expect(page.url()).toBe(beforeToggle)
  expect(searches).toHaveLength(1)
  await disclosure.click()
  if (info.project.name === 'mobile')
    await page.getByRole('button', { name: 'Fermer les filtres' }).click()
  await page
    .getByRole('button', { name: /^Ajouter la séance de Film Playwright/ })
    .click()
  await openFilters(page)
  const selectedOnly = form.getByLabel(
    'Afficher uniquement les séances sélectionnées',
  )
  await expect(selectedOnly).toBeVisible()
  await expect(panel).toBeHidden()
  await selectedOnly.check()
  await expect(page).toHaveURL(/selected_only=1/)
  expect(searches).toHaveLength(1)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
})

test('shared search stays collapsed and retains URL format/ads with no anonymous watchlist control', async ({
  page,
}) => {
  const searches = await mockSlotSearch(page)
  await openPage(
    page,
    `/recherche?${query}&format=2D&include_ads=0&buffer_ads=30`,
  )
  await expect.poll(() => searches.length).toBe(1)
  const form = await openFilters(page)
  const disclosure = form.getByRole('button', { name: 'Options avancées' })
  await expect(disclosure).toHaveAttribute('aria-expanded', 'false')
  await expect(form.locator('#search-advanced-options')).toBeHidden()
  await disclosure.click()
  await expect(
    form.getByRole('combobox', { name: 'Format', exact: true }),
  ).toHaveValue('2D')
  await expect(
    form.getByLabel('Inclure les publicités (+15 min)'),
  ).not.toBeChecked()
  await expect(form.getByLabel('Ma watchlist uniquement')).toHaveCount(0)
  expect(searches[0]).toMatchObject({
    format: '2D',
    include_ads: 'false',
    buffer_ads: '30',
  })
  await disclosure.click()
  expect(searches).toHaveLength(1)
  await expect(page).toHaveURL(/buffer_ads=30/)
})

test('advanced watchlist control remains disabled until owner snapshot is ready', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', { data: account })
  let release!: () => void
  const ready = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/v1/account/watchlist', async (route) => {
    await ready
    await route.continue()
  })
  try {
    await openPage(page, '/recherche')
    const form = page.locator('#search-filters')
    await form.getByRole('button', { name: 'Options avancées' }).click()
    const watchlist = form.getByLabel('Ma watchlist uniquement')
    await expect(watchlist).toBeVisible()
    await expect(watchlist).toBeDisabled()
    release()
    await expect(watchlist).toBeEnabled()
    await expect(watchlist).not.toBeChecked()
  } finally {
    release()
  }
})
