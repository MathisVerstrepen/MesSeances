import { test, expect, openPage } from './fixtures'
import { date, publicActivityItems, theater } from './data.mjs'

test('public timeline preserves Paris chronology, same-day events, labels, fallback posters and scoped links', async ({
  page,
}, testInfo) => {
  await openPage(page, `/cinema/${theater.slug}?view=activity`)
  const timeline = page.getByRole('list', {
    name: 'Historique de la programmation',
  })
  const rows = timeline.locator('[data-event-id]')
  await expect(rows).toHaveCount(2)
  await expect(timeline.locator('[data-activity-day]')).toHaveCount(1)
  const newest = timeline.locator('[data-activity-day="2026-10-02"]')
  await expect(newest.getByRole('heading', { level: 3 })).toHaveText(
    testInfo.project.name === 'desktop'
      ? /2 octobre 2026/
      : /vendredi 2 octobre 2026/,
  )
  await expect(newest.locator('h3 time')).toHaveAttribute(
    'datetime',
    '2026-10-02',
  )
  await expect(newest.locator('> span').nth(1)).toHaveCSS(
    'background-color',
    'rgb(250, 204, 21)',
  )
  for (const [index, item] of publicActivityItems.slice(0, 2).entries()) {
    const row = rows.nth(index)
    await expect(row).toHaveAttribute('data-event-id', item.event_id)
    await expect(
      row.getByRole('heading', { level: 4, name: item.movie.title }),
    ).toBeVisible()
    await expect(row.locator('[data-poster-fallback="activity"]')).toHaveCount(
      1,
    )
    const poster = row
      .getByRole('link', { name: item.movie.title, exact: true })
      .first()
    await expect(poster).toHaveAttribute(
      'href',
      `/film/film-playwright?shared_theaters=${theater.id}`,
    )
    expect((await poster.boundingBox())!.width).toBe(
      testInfo.project.name === 'desktop' ? 112 : 72,
    )
    await expect(
      row.getByText(
        index === 0 ? 'Retour à l’affiche' : 'Ajout à la programmation',
        { exact: true },
      ),
    ).toBeVisible()
    await expect(row.locator('time').first()).toHaveAttribute(
      'datetime',
      item.first_screening_date,
    )
  }
  await expect(rows.first()).toContainText(
    'Programmation précédente · jusqu’au 1 août 2026',
  )
  await expect(
    rows.first().getByRole('link', { name: 'Voir les séances' }),
  ).toHaveAttribute(
    'href',
    `/film/film-playwright?shared_theaters=${theater.id}&date=${date}#schedule-heading`,
  )
  await expect(
    rows.nth(1).getByRole('link', { name: 'Voir les séances' }),
  ).toHaveCount(0)
  await expect(timeline.locator('a[href^="/cinema/"]')).toHaveCount(0)

  const disclosure = page
    .locator('summary')
    .filter({ hasText: 'Historique suivi depuis le' })
  await disclosure.focus()
  await disclosure.press('Enter')
  await expect(
    page.getByText('Les programmations antérieures ne sont pas reconstituées.'),
  ).toBeVisible()
  await disclosure.press('Enter')
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  await expect(rows).toHaveCount(3)
  expect(
    await rows.evaluateAll((elements) =>
      elements.map((row) => row.getAttribute('data-event-id')),
    ),
  ).toEqual(['203', '202', '201'])
  expect(
    await timeline
      .locator('[data-activity-day]')
      .evaluateAll((elements) =>
        elements.map((day) => day.getAttribute('data-activity-day')),
      ),
  ).toEqual(['2026-10-02', '2026-10-01'])
  await expect(
    timeline.locator('[data-activity-day="2026-10-01"] > span').nth(1),
  ).toHaveCSS('background-color', 'rgb(252, 250, 248)')
  await expect(page.getByRole('button', { name: 'Afficher plus' })).toHaveCount(
    0,
  )
  await expect(rows.last().getByRole('heading', { level: 4 })).toHaveText(
    publicActivityItems[2].movie.title,
  )
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)

  const geometry = await newest.evaluate((element) => {
    const heading = element.querySelector('h3')!.getBoundingClientRect()
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
    expect(geometry.dateRight).toBeLessThan(geometry.rowLeft)
  else expect(geometry.dateBottom).toBeLessThan(geometry.rowTop)
  const titleLink = rows
    .first()
    .getByRole('heading', { level: 4 })
    .getByRole('link')
  await titleLink.focus()
  await page.keyboard.press('Tab')
  await page.keyboard.press('Shift+Tab')
  await expect(titleLink).toBeFocused()
  await expect(titleLink).toHaveCSS('outline-style', 'solid')
  const dismiss = page.getByRole('button', { name: 'Ne plus afficher' })
  await dismiss.focus()
  await dismiss.press('Enter')
  await page.evaluate(() => window.scrollTo(0, 0))
  await page.screenshot({
    path: testInfo.outputPath('cinema-activity.png'),
    fullPage: true,
  })
})

test('public activity loading uses timeline geometry before client response', async ({
  page,
}) => {
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/v1/theaters/*/activity?*', async (route) => {
    await held
    await route.continue()
  })
  await openPage(page, `/cinema/${theater.slug}`)
  await page
    .getByRole('navigation', { name: 'Vue de la programmation' })
    .getByRole('link', { name: 'Activité', exact: true })
    .click()
  await expect(
    page.getByRole('status').filter({ hasText: 'Chargement de l’activité' }),
  ).toBeAttached()
  await expect(page.locator('[data-event-id]')).toHaveCount(0)
  release()
  await expect(page.locator('[data-event-id]')).toHaveCount(2)
})

for (const [mode, heading] of [
  ['initializing', 'Historique en cours d’initialisation'],
  ['empty', 'Aucune nouvelle programmation détectée.'],
  ['error', 'Impossible de charger l’activité'],
] as const) {
  test(`public activity ${mode} retains local state`, async ({
    page,
    request,
  }) => {
    await request.post('/__playwright/scenario', { data: { publicFeed: mode } })
    await openPage(page, `/cinema/${theater.slug}?view=activity`)
    await expect(
      page.getByRole('heading', { level: 3, name: heading, exact: true }),
    ).toBeVisible()
    await expect(page.locator('[data-event-id]')).toHaveCount(0)
    if (mode === 'error') {
      await request.post('/__playwright/scenario', { data: {} })
      await page.getByRole('button', { name: 'Réessayer', exact: true }).click()
      await expect(page.locator('[data-event-id]')).toHaveCount(2)
    }
  })
}
