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
async function formats(page: Page) {
  return (await openDirectoryFilters(page)).getByRole('group', {
    name: 'Format',
    exact: true,
  })
}

test('directory and cinema detail show historical technology without currently bookable formats', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { historicalTechnologies: true },
  })
  await openPage(page, '/cinemas')
  const search = page.getByRole('combobox', { name: /^Ville, code postal/ })
  await search.fill('ICE')
  await page.getByRole('option', { name: /^Salles ICE/ }).click()
  await expect(page).toHaveURL(/technologies=ICE/)
  await expect(
    page.getByRole('heading', { name: 'Salles ICE', exact: true }),
  ).toBeVisible()
  await results(page).click()
  const technologies = page.getByRole('list', {
    name: 'Technologies du cinéma',
  })
  await expect(technologies.getByRole('listitem')).toHaveCount(2)
  await expect(technologies.getByTitle('IMAX', { exact: true })).toBeVisible()
  await expect(technologies.getByTitle('ICE', { exact: true })).toBeVisible()
})

test('format facets combine with chains, restore URL history and reset to the directory home', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', {
    data: { directoryTechnologies: true },
  })
  await openPage(page, '/cinemas?tag=kept')
  await expect(
    page.getByRole('heading', { name: 'Par région', exact: true }),
  ).toBeVisible()
  const search = page.getByRole('combobox', { name: /^Ville, code postal/ })
  await search.focus()
  await page.keyboard.type('imax')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/technologies=IMAX/)
  await expect(page).toHaveURL(/tag=kept/)
  await expect(results(page)).toHaveCount(1)
  await expect(results(page)).toHaveAccessibleName(/Cinéma Playwright/)
  let filters = await openDirectoryFilters(page)
  const format = () => filters.getByRole('group', { name: 'Format' })
  const chain = () => filters.getByRole('group', { name: 'Enseigne' })
  await expect(format().getByRole('button', { name: /^2D/ })).toHaveCount(0)
  await expect(format().getByRole('button', { name: /^IMAX/ })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await expect(format().locator('img')).toHaveCount(2)
  for (const logo of await format().locator('img').all()) {
    await expect(logo).toHaveAttribute('alt', '')
    await expect(logo).toHaveJSProperty('complete', true)
    expect(
      await logo.evaluate((image: HTMLImageElement) => image.naturalWidth),
    ).toBeGreaterThan(0)
  }
  await closeDirectoryFilters(page)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute(
    'data-playwright-ready',
    'true',
  )
  filters = await openDirectoryFilters(page)
  await format().getByRole('button', { name: /^ICE/ }).click()
  await expect(results(page)).toHaveCount(2)
  await expect(chain().locator('img')).toHaveCount(2)
  await page.goBack()
  await expect(page).toHaveURL(/technologies=IMAX(?:&|$)/)
  await expect(results(page)).toHaveCount(1)
  await page.goForward()
  await expect(results(page)).toHaveCount(2)
  await chain().getByRole('button', { name: /^CGR/ }).click()
  await expect(results(page)).toHaveCount(1)
  await expect(results(page)).toHaveAccessibleName(/Cinéma Seconde Salle/)
  await page.screenshot({ path: info.outputPath('cinema-technologies.png') })
  if (info.project.name === 'mobile') {
    await filters.getByRole('button', { name: 'Effacer', exact: true }).click()
  } else {
    await page
      .getByRole('link', { name: 'Effacer les filtres', exact: true })
      .click()
  }
  await expect(page).toHaveURL(/\/cinemas\?tag=kept$/)
  await expect(
    page.getByRole('heading', { name: 'Par région', exact: true }),
  ).toBeVisible()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
})

test('unavailable technology deep link remains visible and clearable', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { directoryTechnologies: true },
  })
  await openPage(page, '/cinemas?technologies=DOLBY')
  await expect(
    page.getByText('Aucun cinéma ne correspond à votre recherche.'),
  ).toBeVisible()
  const dolby = (await formats(page)).getByRole('button', { name: /^Dolby/ })
  await expect(dolby).toHaveAttribute('aria-pressed', 'true')
  await expect(dolby).toContainText('0')
  await dolby.click()
  await expect(page).not.toHaveURL(/technologies=/)
})
