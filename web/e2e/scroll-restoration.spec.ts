import type { Page } from '@playwright/test'
import { test, expect, openPage } from './fixtures'
import { movie } from './data.mjs'

const listPath = '/films?q=Film&page=2&sort=title_asc'
const catalog = (page: Page) =>
  page.getByRole('list', { name: 'Films à l’affiche' })
const scrollY = (page: Page) => page.evaluate(() => window.scrollY)
async function restored(page: Page, top: number) {
  await expect
    .poll(async () => Math.abs((await scrollY(page)) - top))
    .toBeLessThanOrEqual(5)
}
async function bottom(page: Page) {
  await catalog(page).getByRole('link').last().scrollIntoViewIfNeeded()
  return scrollY(page)
}

test('delayed catalog restores Back position and query; fresh detail and pagination start top', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { scrollCatalog: 'long' },
  })
  await openPage(page, listPath)
  await expect(catalog(page).getByRole('link')).toHaveCount(24)
  const saved = await bottom(page)
  expect(saved).toBeGreaterThan(1000)
  await catalog(page).getByRole('link').last().click()
  await expect(page).toHaveURL(`/film/${movie.slug}`)
  await restored(page, 0)
  await page.goBack()
  await expect(page).toHaveURL(listPath)
  await expect(catalog(page).getByRole('link')).toHaveCount(24)
  await restored(page, saved)

  await page.getByRole('link', { name: 'Suivant' }).click()
  await expect(page).toHaveURL(/page=3/)
  await expect(catalog(page).getByRole('link')).toHaveCount(24)
  await restored(page, 0)
  await page.evaluate(() => window.scrollTo({ top: 150 }))
  await page.getByRole('combobox', { name: 'Trier par' }).evaluate((select) => {
    if (!(select instanceof HTMLSelectElement))
      throw new Error('Expected sort select')
    select.value = 'title_desc'
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await expect(page).toHaveURL(/sort=title_desc/)
  await expect(page.locator('#films-catalog')).toHaveAttribute(
    'aria-busy',
    'false',
  )
  await restored(page, 150)
})

test('delayed catalog restores cross-page Forward position', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { scrollCatalog: 'long' },
  })
  await openPage(page, `/film/${movie.slug}`)
  await page
    .getByRole('navigation', { name: 'Navigation principale' })
    .getByRole('link', { name: 'Films', exact: true })
    .click()
  await expect(catalog(page).getByRole('link')).toHaveCount(24)
  await page.getByRole('searchbox', { name: 'Rechercher un film' }).fill('Film')
  await page
    .getByRole('searchbox', { name: 'Rechercher un film' })
    .press('Enter')
  await expect(page).toHaveURL('/films?q=Film')
  await expect(page.locator('#films-catalog')).toHaveAttribute(
    'aria-busy',
    'false',
  )
  const saved = await bottom(page)
  await page.goBack()
  await expect(page).toHaveURL(`/film/${movie.slug}`)
  await page.goForward()
  await expect(page).toHaveURL('/films?q=Film')
  await expect(catalog(page).getByRole('link')).toHaveCount(24)
  await restored(page, saved)
})

for (const mode of ['short', 'error']) {
  test(`${mode} catalog finishes restoration at reachable offset without stale scrolling`, async ({
    page,
    request,
  }) => {
    await request.post('/__playwright/scenario', {
      data: { scrollCatalog: 'long' },
    })
    await openPage(page, listPath)
    await expect(catalog(page).getByRole('link')).toHaveCount(24)
    await bottom(page)
    await catalog(page).getByRole('link').last().click()
    await expect(page).toHaveURL(`/film/${movie.slug}`)
    await request.post('/__playwright/scenario', {
      data: { scrollCatalog: mode },
    })
    await page.goBack()
    await expect(page.locator('#films-catalog')).toHaveAttribute(
      'aria-busy',
      'false',
    )
    const max = await page.evaluate(() =>
      Math.max(0, document.documentElement.scrollHeight - window.innerHeight),
    )
    await restored(page, max)
    await page
      .getByRole('navigation', { name: 'Navigation principale' })
      .getByRole('link', { name: 'Films', exact: true })
      .click()
    await page.getByRole('link', { name: /Gérer mes cinémas/ }).click()
    await expect(page).toHaveURL('/cinemas')
    await restored(page, 0)
  })
}

test('superseding navigation cancels delayed catalog restoration', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { scrollCatalog: 'long' },
  })
  await openPage(page, listPath)
  await expect(catalog(page).getByRole('link')).toHaveCount(24)
  await bottom(page)
  await catalog(page).getByRole('link').last().click()
  await expect(page).toHaveURL(`/film/${movie.slug}`)
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/api/v1/movies?**', async (route) => {
    await held
    await route.continue()
  })
  await page.goBack()
  await expect(page.locator('#films-catalog')).toHaveAttribute(
    'aria-busy',
    'true',
  )
  await page.getByRole('link', { name: /Gérer mes cinémas/ }).click()
  await expect(page).toHaveURL('/cinemas')
  await restored(page, 0)
  const response = page.waitForResponse(
    (response) => new URL(response.url()).pathname === '/api/v1/movies',
  )
  release()
  await (await response).finished()
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  )
  await restored(page, 0)
  await expect(page).toHaveURL('/cinemas')
})
