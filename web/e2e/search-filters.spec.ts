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

for (const mode of ['initial', 'edit'] as const) {
  test(`short viewport keeps ${mode} filter action reachable through settings scroll`, async ({
    page,
    request,
  }, info) => {
    const mobile = info.project.name === 'mobile'
    await page.setViewportSize({ width: mobile ? 390 : 1440, height: 460 })
    await request.post('/__playwright/scenario', { data: account })
    const searches = await mockSlotSearch(page)
    await openPage(
      page,
      mode === 'initial' ? '/recherche' : `/recherche?${query}`,
    )
    const form =
      mode === 'initial'
        ? page.locator('#search-filters')
        : await openFilters(page)
    const settings = form.locator('#search-filter-settings')
    const submit = form.getByRole('button', { name: 'Trouver une séance' })
    const disclosure = form.getByRole('button', { name: 'Options avancées' })

    // Check layering before result selection scrolls the document. Scrolled calendar
    // positioning is a pre-existing datepicker issue, separate from the action footer.
    const calendar = form.getByRole('button', {
      name: /^Choisir une autre date/,
    })
    await calendar.click()
    const menu = page.locator('.dp--menu.editorial-calendar-menu')
    await expect(menu).toBeInViewport({ ratio: 1 })
    expect(
      await menu.evaluate((element) => {
        const rect = element.getBoundingClientRect()
        const hit = document.elementFromPoint(
          rect.x + rect.width / 2,
          rect.y + rect.height / 2,
        )
        return Boolean(hit && element.contains(hit))
      }),
    ).toBe(true)
    await page.keyboard.press('Escape')
    await expect(menu).toBeHidden()
    if (mode === 'edit') {
      if (mobile) {
        await expect(form).toHaveAttribute('aria-modal', 'true')
        await page.getByRole('button', { name: 'Fermer les filtres' }).click()
        await expect(
          page.getByRole('button', { name: /^Modifier les filtres/ }),
        ).toBeFocused()
      }
      await page
        .getByRole('button', { name: /^Ajouter la séance de Film Playwright/ })
        .click()
      await expect(page).toHaveURL(/selected=/)
      await openFilters(page)
    }
    await disclosure.click()
    await form
      .getByRole('combobox', { name: 'Format', exact: true })
      .selectOption('2D')
    await form.getByLabel('Inclure les publicités (+15 min)').uncheck()
    const watchlist = form.getByLabel('Ma watchlist uniquement')
    await watchlist.check()
    const finalSetting =
      mode === 'edit'
        ? form.getByLabel('Afficher uniquement les séances sélectionnées')
        : watchlist
    await form
      .getByRole('combobox', { name: 'À partir de' })
      .selectOption('12:00')
    await form
      .getByRole('combobox', { name: 'Terminé avant' })
      .selectOption('18:00')

    const scrollSettings = async (fraction: number) => {
      await settings.evaluate((element, value) => {
        if (getComputedStyle(element).overflowY === 'auto') {
          element.scrollTop =
            value * (element.scrollHeight - element.clientHeight)
        } else {
          const footer = document.querySelector(
            '#search-filters button[type="submit"]',
          )!.parentElement!
          const end =
            element.getBoundingClientRect().bottom +
            window.scrollY -
            window.innerHeight +
            footer.getBoundingClientRect().height +
            16
          window.scrollTo(0, value * end)
        }
      }, fraction)
    }

    const expectActionInViewport = async () => {
      await expect(submit).toHaveCount(1)
      await expect(submit).toBeEnabled()
      await expect(submit).toBeInViewport({ ratio: 1 })
      expect(
        await submit.evaluate((element) => {
          const rect = element.getBoundingClientRect()
          const hit = document.elementFromPoint(
            rect.x + rect.width / 2,
            rect.y + rect.height / 2,
          )
          return (
            rect.top >= 0 &&
            rect.bottom <= window.innerHeight &&
            rect.left >= 0 &&
            rect.right <= window.innerWidth &&
            Boolean(hit && element.contains(hit))
          )
        }),
      ).toBe(true)
      await submit.click({ trial: true })
    }

    for (const [position, fraction] of [
      ['top', 0],
      ['middle', 0.5],
      ['end', 1],
    ] as const) {
      await scrollSettings(fraction)
      await expectActionInViewport()
      await page.screenshot({
        path: info.outputPath(`short-${mode}-${position}.png`),
      })
    }
    // Last setting must fit above the action, not merely remain mounted underneath it.
    await expect(finalSetting).toBeInViewport({ ratio: 1 })
    const lastControl = await finalSetting.locator('..').boundingBox()
    const action = await submit.locator('..').boundingBox()
    expect(lastControl).not.toBeNull()
    expect(action).not.toBeNull()
    expect(lastControl!.y + lastControl!.height).toBeLessThanOrEqual(action!.y)
    await finalSetting.focus()
    await page.keyboard.press('Tab')
    await expect(submit).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(finalSetting).toBeFocused()

    if (mode === 'edit' && mobile) {
      await submit.focus()
      await page.keyboard.press('Tab')
      await expect(
        page.getByRole('button', { name: 'Fermer les filtres' }),
      ).toBeFocused()
      await page.keyboard.press('Shift+Tab')
      await expect(submit).toBeFocused()
    }

    await page.setViewportSize({ width: mobile ? 390 : 1440, height: 320 })
    await scrollSettings(1)
    await expectActionInViewport()
    await expect(finalSetting).toBeInViewport({ ratio: 1 })
    expect(
      await finalSetting.locator('..').evaluate((element) => {
        const footer = document.querySelector(
          '#search-filters button[type="submit"]',
        )!.parentElement!
        return (
          element.getBoundingClientRect().bottom <=
          footer.getBoundingClientRect().top
        )
      }),
    ).toBe(true)
    await page.screenshot({
      path: info.outputPath(`short-${mode}-320-end.png`),
    })

    await scrollSettings(0.5)
    await expectActionInViewport()
    await submit.click()
    await expect(page).toHaveURL(/format=2D/)
    await expect(page).toHaveURL(/include_ads=0/)
    await expect.poll(() => searches.length).toBe(mode === 'initial' ? 1 : 2)
    expect(searches.at(-1)).toMatchObject({
      start_after: '12:00',
      finish_before: '18:00',
      format: '2D',
      include_ads: 'false',
    })
    await expect(
      page.getByRole('heading', { name: 'Ma watchlist', exact: true }),
    ).toBeVisible()
    if (mobile) {
      await expect(form).toBeHidden()
      await expect(submit).toBeHidden()
      await expect(
        page.getByRole('region', { name: 'Résultats de recherche' }),
      ).toBeFocused()
    }
    await openFilters(page)
    await expect(disclosure).toHaveAttribute('aria-expanded', 'true')
    await expect(
      form.getByRole('combobox', { name: 'Format', exact: true }),
    ).toHaveValue('2D')
    await expect(
      form.getByLabel('Inclure les publicités (+15 min)'),
    ).not.toBeChecked()
    await expect(watchlist).toBeChecked()
  })
}
