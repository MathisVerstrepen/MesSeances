import type { Page } from '@playwright/test'
import { test, expect, openPage } from './fixtures'
import { date, movie, showtimes, theater } from './data.mjs'

test('date controls defer calendar, keep unavailable quick dates keyboard reachable and constrain actual calendar selection', async ({
  page,
}) => {
  const tomorrow = new Date(`${date}T12:00:00Z`)
  tomorrow.setUTCDate(tomorrow.getUTCDate() + 1)
  const dayName = (value: string) =>
    new Intl.DateTimeFormat('fr-FR', {
      weekday: 'long',
      day: 'numeric',
      month: 'long',
      timeZone: 'Europe/Paris',
    }).format(new Date(`${value}T12:00:00Z`))
  await openPage(page, '/recherche')
  const tabs = page.locator('[role="tablist"]:visible').getByRole('tab')
  await expect(tabs).toHaveCount(2)
  await expect(
    page.getByRole('dialog', {
      includeHidden: true,
      name: 'Calendrier des dates disponibles',
    }),
  ).toHaveCount(0)
  const today = tabs.filter({ hasText: 'Aujourd’hui' })
  const next = tabs.filter({ hasText: 'Demain' })
  await today.focus()
  for (const [key, target] of [
    ['ArrowLeft', next],
    ['ArrowRight', today],
    ['End', next],
    ['Home', today],
  ] as const) {
    await page.keyboard.press(key)
    await expect(target).toBeFocused()
    await expect(target).toHaveAttribute('aria-selected', 'true')
    await expect(target).toHaveAttribute('tabindex', '0')
    expect(
      await tabs.evaluateAll(
        (nodes) =>
          nodes.filter((node) => node.getAttribute('tabindex') === '0').length,
      ),
    ).toBe(1)
  }
  await next.click()
  await expect(next).toBeEnabled()
  const trigger = page.getByRole('button', { name: /Choisir une autre date/ })
  await trigger.click()
  const calendar = page.getByRole('dialog', {
    name: 'Calendrier des dates disponibles',
  })
  await expect(calendar).toBeVisible()
  await expect(
    calendar.getByRole('gridcell', {
      name: `Choisir ${dayName(tomorrow.toISOString().slice(0, 10))}`,
      exact: true,
    }),
  ).toHaveAttribute('aria-disabled', 'true')
  const availableDay = calendar.getByRole('gridcell', {
    name: `Choisir ${dayName(date)}`,
    exact: true,
  })
  await availableDay.focus()
  await expect(availableDay).toBeFocused()
  await availableDay.click()
  await expect(calendar).toBeHidden()
  await expect(today).toHaveAttribute('aria-selected', 'true')
  const beforeEscape = page.url()
  const reopenWithImmediateEscape = async () => {
    // Start at the trigger, never move focus into the menu to avoid the race.
    // Replay activation and Escape in one JS turn, before scheduled menu focus.
    await trigger.focus()
    await trigger.evaluate((button) => {
      button.click()
      if (document.activeElement !== button)
        throw new Error('Calendar trigger must retain focus during reopening')
      button.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      )
    })
  }
  await reopenWithImmediateEscape()
  await expect(calendar).toBeHidden()
  await expect(trigger).toHaveAttribute('aria-expanded', 'false')
  await expect(trigger).toBeFocused()
  await expect(today).toHaveAttribute('aria-selected', 'true')
  expect(page.url()).toBe(beforeEscape)

  // The same Escape must not dismiss the containing mobile filter sheet.
  const searches = await mockSlotSearch(page)
  await page.getByRole('button', { name: 'Trouver une séance' }).click()
  await expect(page).toHaveURL((url) => url.searchParams.get('date') === date)
  await expect.poll(() => searches.length).toBe(1)
  const form = await openFilters(page)
  const readValues = () =>
    form.locator('input, select').evaluateAll((controls) =>
      controls.map((control) => {
        if (
          !(
            control instanceof HTMLInputElement ||
            control instanceof HTMLSelectElement
          )
        )
          throw new Error('Expected a filter control')
        return {
          value: control.value,
          checked:
            control instanceof HTMLInputElement ? control.checked : false,
        }
      }),
    )
  const values = await readValues()
  const beforeSheetEscape = page.url()
  await reopenWithImmediateEscape()
  await expect(calendar).toBeHidden()
  await expect(trigger).toHaveAttribute('aria-expanded', 'false')
  await expect(trigger).toBeFocused()
  await expect(form).toBeVisible()
  await expect(today).toHaveAttribute('aria-selected', 'true')
  expect(page.url()).toBe(beforeSheetEscape)
  expect(await readValues()).toEqual(values)
  await trigger.click()
  await expect(calendar).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(calendar).toBeHidden()
  await expect(form).toBeVisible()
})

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

async function mockSlotSearch(page: Page, empty = false) {
  const queries: Record<string, string>[] = []
  // Slot searches start after hydration; SSR account/cinema data uses the fixture server.
  await page.route('**/api/v1/search/slot?**', async (route) => {
    const params = new URL(route.request().url()).searchParams
    queries.push(Object.fromEntries(params))
    const showtime = showtimes().showtimes[0]!
    await route.fulfill({
      json: (empty
        ? []
        : [movie, { ...movie, slug: 'other-film', title: 'Autre film' }]
      ).map((item, index) => ({
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
      })),
    })
  })
  return queries
}

async function openFilters(page: Page) {
  if ((page.viewportSize()?.width ?? 1440) < 1024)
    await page.getByRole('button', { name: /^Modifier les filtres/ }).click()
  return page.locator('#search-filters')
}

test('empty results reach the time editor without changing criteria until submission', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', { data: account })
  const searches = await mockSlotSearch(page, true)
  await openPage(
    page,
    `/recherche?${query}&language=VOSTFR&format=2D&include_ads=0`,
  )
  const recovery = page.getByRole('button', {
    name: 'Modifier mon créneau',
    exact: true,
  })
  await expect(recovery).toBeVisible()
  await expect(
    page.getByText(
      'Aucune séance ne correspond à ce créneau et aux filtres sélectionnés.',
    ),
  ).toBeVisible()
  const before = page.url()
  const form = page.locator('#search-filters')
  const start = form.getByRole('combobox', { name: 'À partir de' })
  const end = form.getByRole('combobox', { name: 'Terminé avant' })
  const mobile = info.project.name === 'mobile'
  await recovery.focus()
  await page.keyboard.press('Enter')
  await expect(form).toBeVisible()
  if (mobile) {
    const close = form.getByRole('button', { name: 'Fermer les filtres' })
    const submit = form.getByRole('button', { name: 'Trouver une séance' })
    await expect(form).toHaveAttribute('aria-modal', 'true')
    await expect(close).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(submit).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(close).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(form).toBeHidden()
    await expect(recovery).toBeFocused()
    expect(page.url()).toBe(before)
    expect(searches).toHaveLength(1)
    await page.keyboard.press('Enter')
  } else {
    await expect(start).toBeFocused()
    await expect(start).toBeInViewport({ ratio: 1 })
  }
  await expect(start).toHaveValue('12:00')
  await expect(end).toHaveValue('18:00')
  await expect(
    form.getByRole('combobox', { name: 'Langue', exact: true }),
  ).toHaveValue('VOSTFR')
  await form.getByRole('button', { name: 'Options avancées' }).click()
  await expect(
    form.getByRole('combobox', { name: 'Format', exact: true }),
  ).toHaveValue('2D')
  await expect(
    form.getByLabel('Inclure les publicités (+15 min)'),
  ).not.toBeChecked()
  await expect(form.getByLabel('Ma watchlist uniquement')).not.toBeChecked()
  await form.getByRole('button', { name: 'Options avancées' }).click()
  expect(page.url()).toBe(before)
  expect(searches).toHaveLength(1)
  await page.screenshot({ path: info.outputPath('empty-time-editor.png') })
  await start.selectOption('12:15')
  expect(page.url()).toBe(before)
  expect(searches).toHaveLength(1)
  await form.getByRole('button', { name: 'Trouver une séance' }).click()
  await expect.poll(() => searches.length).toBe(2)
  expect(searches[1]).toEqual({ ...searches[0], start_after: '12:15' })
  await expect(page).toHaveURL(
    (url) => url.searchParams.get('start_after') === '12:15',
  )
  await expect(recovery).toBeVisible()
  await page.screenshot({
    path: info.outputPath('empty-recovery.png'),
    fullPage: true,
  })
})

test('watchlist empty recovery preserves selection and keeps other-film fallback within the same time slot', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', { data: account })
  const searches = await mockSlotSearch(page)
  await openPage(page, `/recherche?${query}`)
  await page
    .getByRole('button', { name: /^Ajouter la séance de Autre film/ })
    .click()
  const form = await openFilters(page)
  await form.getByRole('button', { name: 'Options avancées' }).click()
  const selected = form.getByLabel(
    'Afficher uniquement les séances sélectionnées',
  )
  await selected.check()
  const watchlist = form.getByLabel('Ma watchlist uniquement')
  await watchlist.check()
  if (info.project.name === 'mobile') await page.keyboard.press('Escape')
  const before = page.url()
  const recovery = page.getByRole('button', {
    name: 'Modifier mon créneau',
    exact: true,
  })
  await expect(recovery).toBeVisible()
  await expect(
    page.getByText(
      'Aucune séance de votre watchlist ne correspond à ces filtres.',
    ),
  ).toBeVisible()
  await recovery.click()
  await expect(watchlist).toBeChecked()
  await expect(selected).toBeChecked()
  await expect(form.getByRole('combobox', { name: 'À partir de' })).toHaveValue(
    '12:00',
  )
  await expect(
    form.getByRole('combobox', { name: 'Terminé avant' }),
  ).toHaveValue('18:00')
  if (info.project.name === 'mobile') {
    await page.keyboard.press('Escape')
    await expect(recovery).toBeFocused()
  }
  expect(page.url()).toBe(before)
  expect(searches).toHaveLength(1)
  await page
    .getByRole('button', { name: 'Afficher aussi les autres films' })
    .click()
  await expect(
    page.getByRole('heading', { name: 'Autres films', exact: true }),
  ).toBeVisible()
  await expect(recovery).toHaveCount(0)
  expect(page.url()).toBe(before)
  expect(searches).toHaveLength(1)
  expect(searches[0]).toMatchObject({
    date,
    theaters: theater.id,
    start_after: '12:00',
    finish_before: '18:00',
  })
  await openFilters(page)
  await expect(watchlist).not.toBeChecked()
  await expect(selected).toBeChecked()
})

test('pending and failed searches never offer empty-result recovery', async ({
  page,
}) => {
  let release!: () => void
  const response = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/v1/search/slot?**', async (route) => {
    await response
    await route.fulfill({ status: 503, json: {} })
  })
  try {
    await openPage(page, `/recherche?${query}`)
    await expect(
      page.getByText('Recherche des séances compatibles…'),
    ).toBeVisible()
    const recovery = page.getByRole('button', {
      name: 'Modifier mon créneau',
      exact: true,
    })
    await expect(recovery).toHaveCount(0)
    release()
    await expect(page.getByRole('alert')).toBeVisible()
    await expect(recovery).toHaveCount(0)
  } finally {
    release()
  }
})

test('cinema onboarding clears the search action bar and touch dismissal persists', async ({
  page,
}, info) => {
  const mobile = info.project.name === 'mobile'
  await openPage(page, '/recherche')
  const prompt = page
    .getByRole('status')
    .filter({ hasText: 'Choisissez vos cinémas' })
    .locator('..')
  const close = page.getByRole('button', { name: 'Ne plus afficher' })
  const submit = page.getByRole('button', { name: 'Trouver une séance' })
  await expect(prompt).toBeVisible()
  await expect(close).toHaveCount(0)
  await expect(submit).toBeEnabled()

  for (const width of mobile ? [390, 320, 768, 1023] : [1440]) {
    await page.setViewportSize({ width, height: mobile ? 844 : 900 })
    const box = await prompt.boundingBox()
    expect(box).not.toBeNull()
    expect(box!.x).toBeGreaterThanOrEqual(16)
    expect(box!.x + box!.width).toBeLessThanOrEqual(width - 16)
    if (mobile) {
      expect(
        await prompt.evaluate((element) => element.matches(':hover')),
      ).toBe(false)
      const bar = await submit.locator('..').boundingBox()
      expect(bar).not.toBeNull()
      expect(bar!.y - (box!.y + box!.height)).toBeGreaterThanOrEqual(16)
      expect(bar!.y + bar!.height).toBe(844)
      await expect(submit).toBeInViewport({ ratio: 1 })
      // Check every CTA corner as well as its center, not just visual visibility.
      expect(
        await submit.evaluate((element) => {
          const rect = element.getBoundingClientRect()
          return [
            [rect.left + 2, rect.top + 2],
            [rect.right - 2, rect.top + 2],
            [rect.left + 2, rect.bottom - 2],
            [rect.right - 2, rect.bottom - 2],
            [rect.left + rect.width / 2, rect.top + rect.height / 2],
          ].every(([x, y]) => {
            const hit = document.elementFromPoint(x!, y!)
            return Boolean(hit && element.contains(hit))
          })
        }),
      ).toBe(true)
      await submit.tap({ trial: true })
    } else {
      expect(900 - (box!.y + box!.height)).toBe(16)
      await prompt.hover()
      await expect(close).toHaveCount(0)
    }
    await page.screenshot({
      path: info.outputPath(`onboarding-search-${width}.png`),
    })
  }

  // Other pages retain the original inset, even on touch devices.
  await openPage(page, '/cinema/cinema-playwright')
  await expect(prompt).toBeVisible()
  const otherPage = await prompt.boundingBox()
  expect(page.viewportSize()!.height - (otherPage!.y + otherPage!.height)).toBe(
    16,
  )
  await openPage(page, '/recherche')
  if (mobile) {
    await page.setViewportSize({ width: 320, height: 844 })
    // Real touch swipe preserves dismissal after removing the floating cross.
    const box = (await prompt.boundingBox())!
    const session = await page.context().newCDPSession(page)
    const x = box.x + box.width / 2
    const y = box.y + box.height / 2
    await session.send('Input.dispatchTouchEvent', {
      type: 'touchStart',
      touchPoints: [{ x, y }],
    })
    await session.send('Input.dispatchTouchEvent', {
      type: 'touchMove',
      touchPoints: [{ x: x + 80, y }],
    })
    await session.send('Input.dispatchTouchEvent', {
      type: 'touchEnd',
      touchPoints: [],
    })
    await session.detach()
  } else {
    await prompt.getByRole('link', { name: 'Choisir' }).focus()
    await page.keyboard.press('Escape')
  }
  await expect(prompt).toHaveCount(0)
  expect(
    await page.evaluate(() =>
      localStorage.getItem('messeances.cinemaSelectionPromptDismissed.v1'),
    ),
  ).toBe('1')
  await openPage(page, '/recherche')
  await expect(prompt).toHaveCount(0)
  await openPage(page, '/cinema/cinema-playwright')
  await expect(prompt).toHaveCount(0)
})

test('onboarding follows initial, results, scroll, filter edit and history clearance with a bounded broad request', async ({
  page,
}, info) => {
  const catalog = Array.from({ length: 13 }, (_, index) => ({
    ...theater,
    id: `fixture-${13 - index}`,
    slug: `cinema-${13 - index}`,
  }))
  await page.route('**/api/v1/theaters', (route) =>
    route.fulfill({ json: catalog }),
  )
  const searches = await mockSlotSearch(page)
  await openPage(page, '/recherche')
  const prompt = page
    .getByRole('status')
    .filter({ hasText: 'Choisissez vos cinémas' })
    .locator('..')
  const submit = page.getByRole('button', { name: 'Trouver une séance' })
  const mobile = info.project.name === 'mobile'
  const expectInset = async (clearance: boolean) => {
    await expect(prompt).toBeVisible()
    await expect
      .poll(async () => {
        const box = (await prompt.boundingBox())!
        return page.viewportSize()!.height - box.y - box.height
      })
      .toBe(clearance && mobile ? 102 : 16)
  }
  await expectInset(true)
  await page
    .getByRole('link', { name: 'Crédits & sources', exact: true })
    .click()
  await expect(page).toHaveURL(/\/credits$/)
  await expectInset(false)
  await page.goBack()
  await expect(page).toHaveURL(/\/recherche$/)
  await expectInset(true)
  await submit.click()
  await expect.poll(() => searches.length).toBe(1)
  expect(searches[0]!.theaters).toBe(
    catalog
      .slice(0, 10)
      .map((item) => item.id)
      .join(','),
  )
  await expect(page).toHaveURL((url) => url.searchParams.get('theaters') === '')
  await expectInset(false)
  await page.evaluate(() =>
    window.scrollTo(0, document.documentElement.scrollHeight),
  )
  await expect
    .poll(() => page.evaluate(() => window.scrollY))
    .toBeGreaterThan(0)
  await expectInset(false)
  await page.screenshot({
    path: info.outputPath('onboarding-results-scrolled.png'),
  })
  if (mobile) {
    await openFilters(page)
    await expectInset(true)
    await page.getByRole('button', { name: 'Fermer les filtres' }).click()
    await expectInset(false)
  }
  // SPA navigation and history must not retain form clearance from a previous page.
  await page
    .getByRole('link', { name: 'Crédits & sources', exact: true })
    .click()
  await expect(page).toHaveURL(/\/credits$/)
  await expectInset(false)
  await page.goBack()
  await expect(page).toHaveURL(/\/recherche\?/)
  await expectInset(false)
  await openPage(page, '/recherche')
  await expectInset(true)
  await page.goBack()
  await expect(page).toHaveURL(/\/recherche\?/)
  await expectInset(false)
})

for (const mode of [
  'restored-empty',
  'restored-full',
  'saved-full',
  'subset',
] as const) {
  test(`broad cinema cap preserves ${mode} route and saved scope`, async ({
    page,
  }) => {
    const catalog = Array.from({ length: 13 }, (_, index) => ({
      ...theater,
      id: `fixture-${13 - index}`,
      slug: `cinema-${13 - index}`,
    }))
    const ids = catalog.map((item) => item.id)
    const selected = mode === 'subset' ? ids.slice(1) : ids
    await page.route('**/api/v1/theaters', (route) =>
      route.fulfill({ json: catalog }),
    )
    if (mode === 'saved-full') {
      await page.addInitScript((ids) => {
        localStorage.setItem(
          'messeances.favoriteTheaterIds.v1',
          JSON.stringify(ids),
        )
      }, ids)
    }
    const searches = await mockSlotSearch(page)
    const params = new URLSearchParams({
      ...Object.fromEntries(query),
      theaters: mode === 'restored-empty' ? '' : selected.join(','),
    })
    await openPage(
      page,
      mode === 'saved-full' ? '/recherche' : `/recherche?${params}`,
    )
    if (mode === 'saved-full')
      await page.getByRole('button', { name: 'Trouver une séance' }).click()
    await expect.poll(() => searches.length).toBe(1)
    expect(searches[0]!.theaters).toBe(
      (mode === 'subset' ? selected : ids.slice(0, 10)).join(','),
    )
    expect(new URL(page.url()).searchParams.get('theaters')).toBe(
      mode === 'saved-full' || mode === 'restored-empty'
        ? ''
        : selected.join(','),
    )
    if (mode === 'saved-full')
      expect(
        await page.evaluate(() =>
          JSON.parse(localStorage.getItem('messeances.favoriteTheaterIds.v1')!),
        ),
      ).toEqual(ids)
  })
}

test('empty broad catalog never issues a nationwide slot request', async ({
  page,
}) => {
  await page.route('**/api/v1/theaters', (route) => route.fulfill({ json: [] }))
  const searches = await mockSlotSearch(page)
  const params = new URLSearchParams({
    ...Object.fromEntries(query),
    theaters: '',
  })
  await openPage(page, `/recherche?${params}`)
  await expect(page.getByRole('alert')).toContainText(
    'Aucun cinéma disponible pour la recherche.',
  )
  expect(searches).toHaveLength(0)
})

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
  const selectedOnly = panel.getByLabel(
    'Afficher uniquement les séances sélectionnées',
  )
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
  await expect(selectedOnly).toHaveCount(1)
  await expect(selectedOnly).toBeHidden()
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
  await expect(selectedOnly).toBeVisible()
  await expect(selectedOnly).toBeDisabled()
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
  await expect(panel).toBeHidden()
  await expect(selectedOnly).toBeHidden()
  await disclosure.click()
  await expect(selectedOnly).toBeVisible()
  await expect(selectedOnly).toBeEnabled()
  await selectedOnly.check()
  await expect(page).toHaveURL(/selected_only=1/)
  expect(searches).toHaveLength(1)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
})

test('overlap option restores conflicting screenings and survives reopening a shared search', async ({
  page,
}, info) => {
  const searches = await mockSlotSearch(page)
  await openPage(page, `/recherche?${query}&selected=u100`)
  const other = page.getByRole('button', {
    name: /^Ajouter la séance de Autre film/,
  })
  await expect(other).toHaveCount(0)
  const form = await openFilters(page)
  await form.getByRole('button', { name: 'Options avancées' }).click()
  const overlap = form.getByRole('checkbox', {
    name: /^Autoriser le chevauchement des séances/,
  })
  await expect(overlap).not.toBeChecked()
  await overlap.check()
  await expect(page).toHaveURL(/allow_overlap=1/)
  await expect(page).toHaveURL(/selected=u100/)
  await expect(form).toBeVisible()
  if (info.project.name === 'mobile')
    await page.getByRole('button', { name: 'Fermer les filtres' }).click()
  await expect(other).toBeVisible()
  expect(searches).toHaveLength(1)
  await other.click()
  await expect(page).toHaveURL(
    (url) => url.searchParams.get('selected') === 'u100,u101',
  )
  const shared = page.url()
  await openPage(page, shared)
  await expect(
    page.getByRole('button', { name: /^Retirer la séance de/ }),
  ).toHaveCount(2)
  await openFilters(page)
  await form.getByRole('button', { name: 'Options avancées' }).click()
  await expect(overlap).toBeChecked()
  await overlap.scrollIntoViewIfNeeded()
  await page.screenshot({
    path: info.outputPath('search-overlap-option.png'),
    fullPage: true,
  })
  await overlap.uncheck()
  await expect(page).not.toHaveURL(/allow_overlap/)
  await expect(form).toBeVisible()
  await expect(
    page.getByRole('button', { name: /^Retirer la séance de/ }),
  ).toHaveCount(2)
  expect(searches).toHaveLength(2)
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
    const watched = form.getByLabel('Afficher les films vus')
    await expect(watched).toBeEnabled()
    const selectedOnly = form.getByLabel(
      'Afficher uniquement les séances sélectionnées',
    )
    const finalSetting = watched
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
            rect.width >= 44 &&
            rect.height >= 44 &&
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
    expect(lastControl!.height).toBeGreaterThanOrEqual(44)
    expect(action).not.toBeNull()
    expect(lastControl!.y + lastControl!.height).toBeLessThanOrEqual(action!.y)
    await form.getByLabel('Autoriser le chevauchement des séances').focus()
    await page.keyboard.press('Tab')
    if (mode === 'edit') {
      await expect(selectedOnly).toBeFocused()
      await page.keyboard.press('Tab')
    } else {
      await expect(selectedOnly).toBeDisabled()
    }
    await expect(watchlist).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(watched).toBeFocused()
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
