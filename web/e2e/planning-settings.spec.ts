import { test, expect, openPage } from './fixtures'
import { date, theater } from './data.mjs'

const selectedIds = [theater.id, 'fixture-watched-1', 'fixture-watched-2']

test.beforeEach(async ({ request }) => {
  await request.post('/__playwright/scenario', {
    data: {
      enabled: true,
      state: 'complete',
      watchedCatalog: true,
      selected_ids: selectedIds,
    },
  })
})

test('planning sheet groups settings, applies a temporary cinema subset and keeps it after reload', async ({
  page,
  request,
}, info) => {
  await openPage(page, `/planning?date=${date}&campaign=mobile`)
  const filters = page.getByRole('region', { name: 'Filtres du planning' })
  await expect(filters.getByRole('group')).toHaveCount(0)
  await expect(filters.getByRole('button', { name: /Partager/ })).toBeVisible()
  const trigger = page.getByRole('button', {
    name: 'Modifier les paramètres du planning',
  })
  await trigger.click()
  const sheet = page.getByRole('dialog', { name: 'Paramètres du planning' })
  await expect(sheet).toBeVisible()
  await expect(
    sheet.getByRole('link', { name: 'Gérer mes cinémas' }),
  ).toHaveAttribute('href', '/cinemas')
  const bounds = await sheet.boundingBox()
  const viewport = page.viewportSize()!
  expect(Math.round(bounds!.y + bounds!.height)).toBe(viewport.height)
  expect(bounds!.width).toBeLessThanOrEqual(viewport.width)
  await expect(
    sheet.getByRole('button', { name: 'Fermer les paramètres' }),
  ).toBeFocused()
  await page.screenshot({
    path: info.outputPath('planning-settings.png'),
    fullPage: true,
  })
  await sheet.getByRole('button', { name: '3 cinémas inclus' }).click()
  const cinemas = sheet.getByRole('checkbox')
  await expect(cinemas).toHaveCount(3)
  await sheet.locator('#planning-theater-list label').nth(2).click()
  await expect(cinemas.nth(2)).not.toBeChecked()
  await sheet.getByRole('button', { name: 'Par film', exact: true }).click()
  await sheet.getByRole('button', { name: '2D', exact: true }).click()
  await sheet
    .getByRole('combobox', { name: 'Langue', exact: true })
    .selectOption('VF')
  await sheet.getByRole('button', { name: '15 min', exact: true }).click()
  expect(new URL(page.url()).searchParams.has('mode')).toBe(false)
  await sheet
    .getByRole('button', { name: 'Afficher le planning', exact: true })
    .click()
  await expect(sheet).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await expect(page).toHaveURL(
    (url) =>
      url.searchParams.get('shared_theaters') ===
        selectedIds.slice(0, 2).join(',') &&
      url.searchParams.get('mode') === 'movie' &&
      url.searchParams.get('format') === '2D' &&
      url.searchParams.get('language') === 'VF' &&
      url.searchParams.get('zoom') === '15' &&
      url.searchParams.get('campaign') === 'mobile',
  )
  await expect
    .poll(
      async () =>
        (
          await (await request.get('/__playwright/state')).json()
        ).timelineQueries.at(-1)?.theaters,
    )
    .toBe(selectedIds.slice(0, 2).join(','))
  await openPage(
    page,
    new URL(page.url()).pathname + new URL(page.url()).search,
  )
  await trigger.click()
  await expect(
    sheet.getByRole('button', { name: 'Par film', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true')
  await sheet.getByRole('button', { name: '2 cinémas inclus' }).click()
  await expect(cinemas).toHaveCount(3)
  await expect(cinemas.nth(2)).not.toBeChecked()
  await sheet.locator('#planning-theater-list label').nth(2).click()
  await sheet
    .getByRole('button', { name: 'Afficher le planning', exact: true })
    .click()
  await expect(page).toHaveURL(
    (url) => !url.searchParams.has('shared_theaters'),
  )
  // Page selections never write account favorites.
  const state = await (await request.get('/__playwright/state')).json()
  expect(state.selected.theater_ids).toEqual(selectedIds)
})

test('sheet keeps keyboard focus, dismisses drafts and rejects an empty cinema selection', async ({
  page,
}) => {
  await openPage(page, `/planning?date=${date}`)
  const trigger = page.getByRole('button', {
    name: 'Modifier les paramètres du planning',
  })
  const sheet = page.getByRole('dialog', { name: 'Paramètres du planning' })
  const close = sheet.getByRole('button', { name: 'Fermer les paramètres' })
  await trigger.click()
  await page.keyboard.press('Shift+Tab')
  await expect(
    sheet.getByRole('button', { name: 'Afficher le planning', exact: true }),
  ).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(close).toBeFocused()
  await sheet.getByRole('button', { name: 'Par film', exact: true }).click()
  await page.keyboard.press('Escape')
  await expect(sheet).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await trigger.click()
  await expect(
    sheet.getByRole('button', { name: 'Par cinéma', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true')
  await sheet.getByRole('button', { name: '3 cinémas inclus' }).click()
  for (const checkbox of await sheet.getByRole('checkbox').all())
    await checkbox.press('Space')
  await sheet
    .getByRole('button', { name: 'Afficher le planning', exact: true })
    .click()
  await expect(sheet.getByRole('alert')).toHaveText(
    'Sélectionnez au moins un cinéma.',
  )
  await close.click()
  await expect(trigger).toBeFocused()
  await trigger.click()
  await page.mouse.click(2, 2)
  await expect(sheet).toHaveCount(0)
  await expect(trigger).toBeFocused()
  expect(await page.evaluate(() => document.body.style.overflow)).toBe('')
})

test('narrow screens keep date, settings and sharing reachable without page overflow', async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await openPage(page, `/planning?date=${date}`)
  await page
    .getByRole('button', { name: 'Modifier les paramètres du planning' })
    .click()
  const sheet = page.getByRole('dialog', { name: 'Paramètres du planning' })
  await sheet.getByRole('button', { name: '3 cinémas inclus' }).click()
  await sheet
    .getByRole('button', { name: '1 h', exact: true })
    .scrollIntoViewIfNeeded()
  await expect(
    sheet.getByRole('button', { name: 'Afficher le planning', exact: true }),
  ).toBeInViewport()
  expect(
    await sheet.evaluate(
      (element) => element.scrollWidth <= element.clientWidth,
    ),
  ).toBe(true)
  await page.screenshot({
    path: info.outputPath('planning-settings-narrow.png'),
    fullPage: true,
  })
  await page.keyboard.press('Escape')
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)
})

test('without favorites the cinema dropdown keeps the full catalog available after narrowing', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: {
      enabled: true,
      state: 'complete',
      watchedCatalog: true,
      selected_ids: [],
    },
  })
  await openPage(page, `/planning?date=${date}`)
  const trigger = page.getByRole('button', {
    name: 'Modifier les paramètres du planning',
  })
  const sheet = page.getByRole('dialog', { name: 'Paramètres du planning' })
  await trigger.click()
  await sheet.getByRole('button', { name: '12 cinémas inclus' }).click()
  await sheet.locator('#planning-theater-list label').last().click()
  await sheet
    .getByRole('button', { name: 'Afficher le planning', exact: true })
    .click()
  await expect(page).toHaveURL(
    (url) => url.searchParams.get('shared_theaters')?.split(',').length === 11,
  )
  await trigger.click()
  await sheet.getByRole('button', { name: '11 cinémas inclus' }).click()
  await expect(sheet.getByRole('checkbox')).toHaveCount(12)
  await expect(sheet.getByRole('checkbox').last()).not.toBeChecked()
  await sheet.locator('#planning-theater-list label').last().click()
  await sheet
    .getByRole('button', { name: 'Afficher le planning', exact: true })
    .click()
  await expect(page).toHaveURL(
    (url) => !url.searchParams.has('shared_theaters'),
  )
})
