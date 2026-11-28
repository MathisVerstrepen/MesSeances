import type { Page } from '@playwright/test'
import {
  test,
  expect,
  openPage,
  openDirectoryFilters,
  closeDirectoryFilters,
} from './fixtures'

const results = (page: Page) =>
  page
    .locator('#cinema-results')
    .getByRole('link', { name: /^Voir les séances/ })
const noHorizontalScroll = (page: Page) =>
  page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)

test.beforeEach(async ({ request }) => {
  await request.post('/__playwright/scenario', {
    data: { locatedDirectory: true },
  })
})

test('directory home lists regions as a borderless two-column grid of links', async ({
  page,
}, info) => {
  await openPage(page, '/cinemas')
  await expect(page.locator('#cinema-results')).toHaveCount(0)
  const regions = page
    .getByRole('heading', { name: 'Par région', exact: true })
    .locator('xpath=following-sibling::ul[1]')
  const links = regions.getByRole('link')
  await expect(links).toHaveText([
    'Auvergne-Rhône-Alpes',
    'Hauts-de-France',
    'Île-de-France',
    'Nouvelle-Aquitaine',
  ])
  const columns = await regions.evaluate(
    (element) =>
      getComputedStyle(element).gridTemplateColumns.split(' ').length,
  )
  expect(columns).toBe(info.project.name === 'mobile' ? 1 : 2)
  for (const item of await regions.getByRole('listitem').all())
    expect(
      await item.evaluate(
        (element) => getComputedStyle(element).borderTopWidth,
      ),
    ).toBe('0px')
  await page.screenshot({
    path: info.outputPath('directory-home.png'),
    fullPage: true,
  })
  expect(await noHorizontalScroll(page)).toBe(true)

  await links.filter({ hasText: 'Hauts-de-France' }).scrollIntoViewIfNeeded()
  expect(await page.evaluate(() => scrollY)).toBeGreaterThan(0)
  await links.filter({ hasText: 'Hauts-de-France' }).click()
  await expect(page).toHaveURL(/\/cinemas\?region=hauts-de-france$/)
  await expect.poll(() => page.evaluate(() => scrollY)).toBe(0)
  await expect(
    page.getByRole('heading', { name: 'Hauts-de-France', exact: true }),
  ).toBeVisible()
  await expect(results(page)).toHaveCount(2)
  await expect(page.getByText('2 cinémas', { exact: true })).toBeVisible()
  await page
    .getByRole('navigation', { name: 'Fil d’Ariane de l’annuaire' })
    .getByRole('link', { name: /Tous les cinémas/ })
    .click()
  await expect(page).toHaveURL(/\/cinemas$/)
})

test('search suggests postal codes, departments and arrondissements and opens a territory', async ({
  page,
}, info) => {
  await openPage(page, '/cinemas')
  const search = page.getByRole('combobox', { name: /^Ville, code postal/ })
  await search.fill('59')
  const listbox = page.getByRole('listbox', { name: 'Suggestions' })
  await expect(search).toHaveAttribute('aria-expanded', 'true')
  await expect(
    listbox.getByRole('group', { name: 'Départements' }).getByRole('option'),
  ).toHaveText([/^Nord \(59\)/])
  await page.screenshot({ path: info.outputPath('directory-suggestions.png') })
  await search.press('ArrowDown')
  await expect(search).toHaveAttribute(
    'aria-activedescendant',
    'cinema-search-option-0',
  )
  await search.press('Enter')
  await expect(page).toHaveURL(/departement=59/)
  await expect(
    page.getByRole('heading', { name: 'Nord (59)', exact: true }),
  ).toBeVisible()
  await expect(results(page)).toHaveCount(2)
  await (await openDirectoryFilters(page))
    .getByRole('link', { name: /^Roubaix/ })
    .click()
  await expect(page).toHaveURL(/ville=roubaix/)
  await expect(results(page)).toHaveCount(1)
  await expect(
    page.getByRole('link', { name: /Films à l’affiche à Roubaix/ }),
  ).toHaveAttribute('href', '/ville/roubaix/cinemas')

  await search.fill('75011')
  await page.getByRole('option', { name: /^Paris 11e/ }).click()
  await expect(page).toHaveURL(/arrondissement=11/)
  await expect(results(page)).toHaveAccessibleName(/Cinéma Bastille/)
  await search.fill('garonne')
  await search.press('Escape')
  await expect(search).toHaveAttribute('aria-expanded', 'false')
  await search.press('ArrowDown')
  await page.getByRole('option', { name: /^Cinéma Garonne/ }).click()
  await expect(page).toHaveURL(/\/cinema\/cinema-bordeaux/)
})

test('nearby mode sorts by distance, and widens an empty radius', async ({
  page,
  context,
}, info) => {
  await context.grantPermissions(['geolocation'])
  await context.setGeolocation({ latitude: 50.637, longitude: 3.063 })
  await openPage(page, '/cinemas')
  await page.getByRole('button', { name: 'Autour de moi', exact: true }).click()
  await expect(page).toHaveURL(/location=nearby/)
  await expect(
    page.getByRole('heading', { name: 'Autour de vous', exact: true }),
  ).toBeVisible()
  await expect(results(page)).toHaveCount(2)
  await expect(results(page).first()).toHaveAccessibleName(/Cinéma Playwright/)
  await expect(page.getByText('Le plus proche', { exact: true })).toHaveCount(1)
  await expect(page.getByText(/^Position utilisée : latitude/)).toBeVisible()
  const radius = (await openDirectoryFilters(page)).getByRole('group', {
    name: 'Rayon',
    exact: true,
  })
  await radius.getByRole('button', { name: /^5 km/ }).click()
  await expect(page).toHaveURL(/rayon=5/)
  await page.screenshot({ path: info.outputPath('directory-filters.png') })
  await closeDirectoryFilters(page)
  await expect(results(page)).toHaveCount(1)
  await context.setGeolocation({ latitude: 48.8049, longitude: 2.1204 })
  await page.reload()
  await expect(page.getByText(/Aucune salle à moins de 5 km/)).toBeVisible()
  await page.getByRole('button', { name: /^Élargir à/ }).click()
  await expect(page).toHaveURL(/rayon=25/)
  await expect(results(page)).toHaveAccessibleName(/Cinéma Bastille/)
  // The map itself loads external tiles, which this offline harness forbids.
  await expect(
    page.getByRole('button', { name: 'Carte', exact: true }),
  ).toHaveAttribute('aria-pressed', 'false')
  await page.screenshot({ path: info.outputPath('directory-nearby.png') })
  expect(await noHorizontalScroll(page)).toBe(true)
})

test('picker adds nearby cinemas, shows them as photo cards and manages removal', async ({
  page,
}, info) => {
  await openPage(page, '/cinemas')
  const favorites = page.getByRole('list', { name: 'Mes cinémas sélectionnés' })
  await expect(favorites).toHaveCount(0)
  await page.getByRole('button', { name: /^Choisir mes cinémas/ }).click()
  const dialog = page.getByRole('dialog', {
    name: 'Où allez-vous au cinéma ?',
  })
  await expect(dialog).toBeVisible()
  await dialog
    .getByRole('searchbox', { name: 'Ville ou code postal' })
    .fill('lil')
  await dialog.getByRole('button', { name: /^Lille/ }).click()
  const pick = page.getByRole('dialog', { name: 'Cochez vos salles' })
  const candidates = pick.getByRole('list', { name: 'Cinémas proposés' })
  await expect(candidates.getByRole('checkbox')).toHaveCount(2)
  const confirm = pick.getByRole('button', {
    name: 'Cochez au moins une salle',
  })
  await expect(confirm).toBeDisabled()
  await candidates.getByRole('checkbox').first().check()
  await candidates.getByRole('checkbox').nth(1).check()
  await page.screenshot({ path: info.outputPath('directory-picker.png') })
  await pick.getByRole('button', { name: 'Ajouter 2 cinémas' }).click()
  await expect(pick).toBeHidden()
  await expect(page.getByText('2 cinémas ajoutés à vos cinémas.')).toBeVisible()
  await expect(favorites.getByRole('link')).toHaveCount(2)
  await expect(
    favorites.getByRole('link', {
      name: 'Voir les séances : Cinéma Playwright',
    }),
  ).toHaveAttribute('href', '/cinema/cinema-playwright')
  await page.screenshot({
    path: info.outputPath('directory-favorites.png'),
    fullPage: true,
  })

  await page.reload()
  await expect(favorites.getByRole('link')).toHaveCount(2)
  await page.getByRole('button', { name: /^Modifier/ }).click()
  const manage = page.getByRole('dialog', { name: 'Mes cinémas (2)' })
  await manage
    .getByRole('button', {
      name: 'Retirer Cinéma Seconde Salle de mes cinémas',
    })
    .click()
  await expect(
    page.getByRole('dialog', { name: 'Mes cinémas (1)' }),
  ).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(favorites.getByRole('link')).toHaveCount(1)
})

test('result rows toggle a cinema in my cinemas from any territory', async ({
  page,
}) => {
  await openPage(page, '/cinemas?ville=bordeaux')
  const add = page.getByRole('button', {
    name: 'Ajouter Cinéma Garonne à mes cinémas',
  })
  await expect(add).toHaveAttribute('aria-pressed', 'false')
  await add.click()
  const remove = page.getByRole('button', {
    name: 'Retirer Cinéma Garonne de mes cinémas',
  })
  await expect(remove).toHaveAttribute('aria-pressed', 'true')
  await openPage(page, '/cinemas')
  await expect(
    page
      .getByRole('list', { name: 'Mes cinémas sélectionnés' })
      .getByRole('link'),
  ).toHaveCount(1)
})

test('picker lists the cinemas of a city that has no coordinates yet', async ({
  page,
}) => {
  await openPage(page, '/cinemas')
  await page.getByRole('button', { name: /^Choisir mes cinémas/ }).click()
  const dialog = page.getByRole('dialog', { name: 'Où allez-vous au cinéma ?' })
  await dialog
    .getByRole('searchbox', { name: 'Ville ou code postal' })
    .fill('Lyon')
  await dialog.getByRole('button', { name: /^Lyon/ }).click()
  const pick = page.getByRole('dialog', { name: 'Cochez vos salles' })
  await expect(pick.getByText('À', { exact: false }).first()).toContainText(
    'Lyon',
  )
  await expect(
    pick.getByRole('group', { name: 'Rayon de recherche' }),
  ).toHaveCount(0)
  const candidates = pick.getByRole('list', { name: 'Cinémas proposés' })
  await expect(candidates.getByRole('checkbox')).toHaveCount(1)
  await candidates.getByRole('checkbox').check()
  await pick.getByRole('button', { name: 'Ajouter 1 cinéma' }).click()
  await expect(
    page
      .getByRole('list', { name: 'Mes cinémas sélectionnés' })
      .getByRole('link', { name: 'Voir les séances : Cinéma Sans Repère' }),
  ).toBeVisible()
})
