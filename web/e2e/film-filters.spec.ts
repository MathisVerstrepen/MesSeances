import { test, expect, openPage } from './fixtures'
import { movie, generatedAt } from './data.mjs'

const genres = [
  'Action',
  'Animation',
  'Aventure',
  'Comédie',
  'Concert',
  'Crime',
  'Divers',
  'Documentaire',
  'Drame',
  'Événement sportif',
  'Familial',
  'Fantastique',
  'Guerre',
  'Histoire',
  'Horreur',
  'Musique',
  'Mystère',
  'Romance',
  'Science-Fiction',
  'Thriller',
  'Western',
]

async function openCatalog(
  page: import('@playwright/test').Page,
  path = '/films',
) {
  await page.route(/\/api\/v1\/movies(?:\?|$)/, (route) =>
    route.fulfill({
      json: {
        items: [movie],
        available_genres: genres,
        page: 1,
        page_size: 24,
        total: 1,
        generated_at: generatedAt,
        catalog_revision: 'filter-fixture',
      },
    }),
  )
  await openPage(page, path)
  await expect(page.locator('#films-catalog')).toHaveAttribute(
    'aria-busy',
    'false',
  )
}

test('film filters overlay the catalogue, contain focus and keep actions visible', async ({
  page,
}, testInfo) => {
  await openCatalog(page)
  const trigger = page.getByRole('button', {
    name: 'Afficher les filtres avancés',
  })
  await trigger.scrollIntoViewIfNeeded()
  const workspace = page.locator('.search-workspace')
  const searchBox = page.getByRole('searchbox', { name: 'Rechercher un film' })
  const searchBounds = await searchBox.boundingBox()
  const triggerBounds = await trigger.boundingBox()
  expect(triggerBounds!.y).toBe(searchBounds!.y)
  expect(triggerBounds!.height).toBe(searchBounds!.height)
  await expect(page.getByRole('combobox', { name: 'Trier par' })).toHaveCount(0)
  expect(
    await workspace.evaluate(
      (element) => element.scrollWidth <= element.clientWidth,
    ),
  ).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('film-search.png') })
  const before = await workspace.boundingBox()
  const scroll = await page.evaluate(() => window.scrollY)
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: 'Filtres avancés' })
  const close = dialog.getByRole('button', { name: 'Fermer les filtres' })
  await expect(close).toBeFocused()
  expect(await workspace.boundingBox()).toEqual(before)
  expect(await page.evaluate(() => window.scrollY)).toBe(scroll)
  const panel = await dialog.locator('form').boundingBox()
  const viewport = page.viewportSize()!
  expect(panel!.y + panel!.height).toBe(viewport.height)
  if (testInfo.project.name === 'desktop') {
    expect(panel!.y).toBe(0)
    expect(panel!.width).toBe(512)
    expect(panel!.x + panel!.width).toBe(viewport.width)
  } else {
    expect(panel!.x).toBe(0)
    expect(panel!.width).toBe(viewport.width)
    expect(panel!.y).toBeGreaterThan(0)
  }
  await expect(
    dialog.getByRole('button', { name: 'Appliquer', exact: true }),
  ).toBeInViewport()
  await close.press('Shift+Tab')
  expect(
    await dialog.evaluate((element) =>
      element.contains(document.activeElement),
    ),
  ).toBe(true)
  await close.focus()
  await page.screenshot({ path: testInfo.outputPath('film-filters.png') })
  const footer = dialog.locator('footer')
  const footerBefore = await footer.boundingBox()
  await dialog.getByRole('searchbox').fill('Action')
  expect(await footer.boundingBox()).toEqual(footerBefore)
  const duration = dialog.getByRole('group', { name: 'Durée', exact: true })
  const segmentRows = await duration
    .locator('label')
    .evaluateAll((labels) =>
      labels.map((label) => label.getBoundingClientRect().y),
    )
  expect(new Set(segmentRows).size).toBe(1)
  const medium = duration.getByRole('radio', { name: 'De 1h30 à 2h' })
  await medium.focus()
  await medium.press('Space')
  await expect(medium).toBeChecked()
  await expect(duration.getByText('1h30–2h', { exact: true })).toHaveCSS(
    'background-color',
    'rgb(39, 39, 42)',
  )
  await dialog.getByRole('searchbox').fill('')
  await duration.scrollIntoViewIfNeeded()
  expect(await footer.boundingBox()).toEqual(footerBefore)
  await expect(
    dialog.getByRole('button', { name: 'Effacer les filtres' }),
  ).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('film-duration.png') })
  await page.mouse.wheel(0, 600)
  expect(await page.evaluate(() => window.scrollY)).toBe(scroll)
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()
  expect(await workspace.boundingBox()).toEqual(before)
  await trigger.click()
  await page.mouse.click(1, 1)
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await trigger.click()
  await close.click()
  await expect(dialog).toHaveCount(0)
  expect(
    await page.evaluate(() => document.documentElement.style.overflow),
  ).toBe('')
})

test('genre chips support search, multiple selections, apply, reload and reset', async ({
  page,
}) => {
  await openCatalog(page, '/films?q=Film&sort=title_asc')
  const trigger = page.getByRole('button', {
    name: /Afficher les filtres avancés/,
  })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: 'Filtres avancés' })
  const genreLayout = () =>
    dialog
      .getByRole('group', { name: 'Genres', exact: true })
      .evaluate((group) => {
        const bounds = group.getBoundingClientRect()
        return {
          height: bounds.height,
          chips: [...group.querySelectorAll('label')].map((label) => {
            const rect = label.getBoundingClientRect()
            return {
              x: rect.x - bounds.x,
              y: rect.y - bounds.y,
              width: rect.width,
              height: rect.height,
            }
          }),
        }
      })
  const initialGenreLayout = await genreLayout()
  await dialog.getByText('Action', { exact: true }).click()
  expect(await genreLayout()).toEqual(initialGenreLayout)
  await dialog.getByText('Action', { exact: true }).click()
  expect(await genreLayout()).toEqual(initialGenreLayout)
  await dialog.getByText('Action', { exact: true }).click()
  await dialog
    .getByRole('searchbox', { name: 'Rechercher un genre' })
    .fill('comedie')
  await expect(
    dialog.getByRole('checkbox', { name: 'Action', exact: true }),
  ).toHaveCount(0)
  await dialog.getByText('Comédie', { exact: true }).click()
  const clearGenres = dialog.getByRole('button', {
    name: 'Tout désélectionner',
  })
  await expect(clearGenres).toBeVisible()
  const titleBounds = (await dialog
    .getByText('Genres', { exact: true })
    .boundingBox())!
  const clearBounds = (await clearGenres.boundingBox())!
  expect(
    Math.abs(
      titleBounds.y +
        titleBounds.height / 2 -
        clearBounds.y -
        clearBounds.height / 2,
    ),
  ).toBeLessThan(1)
  await expect(dialog.getByText('2 genres sélectionnés')).toHaveCount(0)
  await dialog.getByRole('searchbox').fill('inexistant')
  await expect(dialog.getByRole('status')).toContainText('Aucun genre')
  await dialog.getByRole('searchbox').fill('')
  await expect(
    dialog.getByRole('checkbox', { name: 'Action', exact: true }),
  ).toBeChecked()
  await expect(
    dialog.getByRole('radio', { name: 'Cinémas sélectionnés', exact: true }),
  ).toBeChecked()
  await dialog.getByText('Tous les cinémas', { exact: true }).click()
  await expect(
    dialog.getByRole('radio', { name: 'Tous les cinémas', exact: true }),
  ).toBeChecked()
  await dialog.getByText('< 1h30', { exact: true }).click()
  await dialog
    .getByRole('combobox', { name: 'Trier par' })
    .selectOption('title_desc')
  await expect(page).toHaveURL('/films?q=Film&sort=title_asc')
  await expect(
    dialog.getByRole('checkbox', { name: 'Action', exact: true }),
  ).toBeChecked()
  await dialog.getByRole('button', { name: 'Appliquer', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  await expect(page).toHaveURL(
    (url) =>
      url.searchParams.get('genres') === 'Action,Comédie' &&
      url.searchParams.get('duration') === 'short' &&
      url.searchParams.get('all_theaters') === '1' &&
      url.searchParams.get('q') === 'Film' &&
      url.searchParams.get('sort') === 'title_desc',
  )
  await expect(trigger).toHaveAccessibleName(
    'Afficher les filtres avancés (4 actifs)',
  )
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute(
    'data-playwright-ready',
    'true',
  )
  await expect(dialog).toHaveCount(0)
  await trigger.click()
  await expect(
    dialog.getByRole('checkbox', { name: 'Comédie', exact: true }),
  ).toBeChecked()
  await expect(
    dialog.getByRole('radio', { name: 'Tous les cinémas', exact: true }),
  ).toBeChecked()
  await dialog.getByText('Cinémas sélectionnés', { exact: true }).click()
  await expect(
    dialog.getByRole('radio', { name: 'Tous les cinémas', exact: true }),
  ).not.toBeChecked()
  await dialog.getByRole('button', { name: 'Tout désélectionner' }).click()
  await expect(
    dialog.getByRole('checkbox', { name: 'Comédie', exact: true }),
  ).not.toBeChecked()
  await dialog.getByRole('button', { name: 'Effacer les filtres' }).click()
  await expect(page).toHaveURL('/films?q=Film&sort=title_desc')
  await expect(dialog.getByRole('combobox', { name: 'Trier par' })).toHaveValue(
    'title_desc',
  )
  await expect(dialog.getByLabel('Toutes les durées')).toBeChecked()
  await expect(
    dialog.getByRole('radio', { name: 'Cinémas sélectionnés', exact: true }),
  ).toBeChecked()
  await dialog.getByRole('button', { name: 'Appliquer', exact: true }).click()
  await expect(dialog).toHaveCount(0)
})

test('the date calendar remains interactive inside the filter overlay', async ({
  page,
}, testInfo) => {
  await openCatalog(page)
  await page
    .getByRole('button', { name: 'Afficher les filtres avancés' })
    .click()
  const dialog = page.getByRole('dialog', { name: 'Filtres avancés' })
  const dates = dialog.getByRole('group', {
    name: 'Date de séance',
    exact: true,
  })
  const tomorrow = dates.getByRole('radio', { name: 'Demain', exact: true })
  await tomorrow.focus()
  await tomorrow.press('Space')
  await expect(tomorrow).toBeChecked()
  await tomorrow.press('ArrowRight')
  await expect(
    dates.getByRole('radio', { name: 'Ce week-end', exact: true }),
  ).toBeChecked()
  const customDate = dates.getByRole('radio', {
    name: 'Date précise',
    exact: true,
  })
  await customDate.focus()
  await customDate.press('Space')
  const calendarTrigger = dialog.getByRole('combobox', {
    name: 'Saisir une date au format jour-mois-année',
  })
  await expect(calendarTrigger).toHaveCSS('border-top-width', '2px')
  await expect(calendarTrigger).toHaveCSS('border-top-color', 'rgb(39, 39, 42)')
  await expect(calendarTrigger).toHaveCSS('min-height', '52px')
  await calendarTrigger.scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('film-custom-date.png') })
  await calendarTrigger.click()
  const calendar = dialog.locator('.catalog-calendar-menu')
  await expect(calendar).toBeVisible()
  await expect(calendar).toBeInViewport()
  await page.keyboard.press('Escape')
  await expect(calendar).toHaveCount(0)
  await expect(dialog).toBeVisible()
  await calendarTrigger.click()
  await expect(calendar).toBeVisible()
  await calendar.getByRole('button', { name: 'Mois suivant' }).click()
  await expect(calendar).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(calendar).toHaveCount(0)
  const range = dates.getByRole('radio', { name: 'Période', exact: true })
  await range.focus()
  await range.press('Space')
  const rangeInput = dates.locator('#film-custom-range')
  await expect(rangeInput).toHaveValue(/ au /)
  await expect(rangeInput).toHaveCSS('border-top-color', 'rgb(39, 39, 42)')
  await rangeInput.scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('film-date-range.png') })
  await dates.getByRole('button', { name: 'Effacer la date' }).click()
  await expect(
    dialog.getByRole('button', { name: 'Appliquer', exact: true }),
  ).toBeDisabled()
  await expect(dialog.getByRole('alert')).toBeVisible()
  await customDate.focus()
  await customDate.press('Space')
  await expect(
    dialog.getByRole('button', { name: 'Appliquer', exact: true }),
  ).toBeEnabled()
  await dialog.getByRole('button', { name: 'Appliquer', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  await expect(page).toHaveURL((url) =>
    /^\d{4}-\d{2}-\d{2}$/.test(url.searchParams.get('date') || ''),
  )
})

test('genres collapse to two rows with an inline expansion action and preserve hidden selections', async ({
  page,
}, testInfo) => {
  await openCatalog(page)
  const trigger = page.getByRole('button', {
    name: 'Afficher les filtres avancés',
  })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: 'Filtres avancés' })
  const group = dialog.getByRole('group', { name: 'Genres', exact: true })
  const more = group.getByRole('button', { name: 'Voir plus', exact: true })
  const assertTwoRows = async () => {
    await expect(more).toBeVisible()
    const positions = await group
      .locator('label:visible')
      .evaluateAll((labels) =>
        labels.map((label) => label.getBoundingClientRect().y),
      )
    const rows = [...new Set(positions)]
    expect(rows).toHaveLength(2)
    expect((await more.boundingBox())!.y).toBe(rows[1])
    expect(await group.getByRole('checkbox').count()).toBe(positions.length)
  }
  await assertTwoRows()
  const beforeResize = await group.getByRole('checkbox').count()
  await page.setViewportSize({
    width: testInfo.project.name === 'desktop' ? 900 : 320,
    height: 844,
  })
  await expect
    .poll(() => group.getByRole('checkbox').count())
    .not.toBe(beforeResize)
  await assertTwoRows()
  await page.screenshot({ path: testInfo.outputPath('genres-collapsed.png') })
  await more.focus()
  await more.press('Space')
  await expect(group.getByRole('checkbox')).toHaveCount(genres.length)
  await group.getByText('Western', { exact: true }).click()
  await group.getByRole('button', { name: 'Voir moins', exact: true }).click()
  await assertTwoRows()
  await expect(
    group.getByRole('button', { name: 'Tout désélectionner' }),
  ).toBeVisible()
  await expect(
    group.getByRole('checkbox', { name: 'Western', exact: true }),
  ).toHaveCount(0)
  await group.getByRole('searchbox').fill('western')
  await expect(
    group.getByRole('checkbox', { name: 'Western', exact: true }),
  ).toBeChecked()
  await expect(more).toHaveCount(0)
  await group.getByRole('searchbox').fill('')
  await assertTwoRows()
  await group.getByRole('button', { name: 'Tout désélectionner' }).click()
  await expect(
    group.getByRole('button', { name: 'Tout désélectionner' }),
  ).toHaveCount(0)
  await expect(group.locator('input[type="checkbox"]:checked')).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Fermer les filtres' }).click()
  await trigger.click()
  await assertTwoRows()
})
