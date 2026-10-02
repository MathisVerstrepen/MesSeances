import { test, expect, openPage } from './fixtures'
import { date, movie, theater } from './data.mjs'

const path = `/cinema/${theater.slug}?date=${date}`

test('cinema identity and programme render on the server', async ({
  request,
}) => {
  const response = await request.get(path)
  expect(response.status()).toBe(200)
  const html = await response.text()
  expect(html).toContain(theater.name)
  expect(html).toContain(theater.address)
  expect(html).toContain(movie.title)
})

test('cinema is usable without horizontal overflow', async ({
  page,
}, testInfo) => {
  await openPage(page, path)
  await expect(page.getByRole('heading', { level: 1 })).toContainText(
    theater.name,
  )
  await expect(
    page.getByRole('heading', { name: 'Séances', exact: true }),
  ).toBeVisible()
  await expect(page.getByText(theater.address, { exact: true })).toBeVisible()
  await expect(
    page.getByText(movie.title, { exact: true }).first(),
  ).toBeVisible()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy()
  await page.screenshot({
    path: testInfo.outputPath('cinema.png'),
    fullPage: true,
  })
})

test('films tab, search and browser history preserve the programme', async ({
  page,
}) => {
  await openPage(page, path)
  await page
    .getByRole('navigation', { name: 'Vue de la programmation' })
    .getByRole('link', { name: 'Films', exact: true })
    .click()
  await expect(page).toHaveURL(/view=films/)
  await expect(
    page.getByRole('heading', { name: 'Films', exact: true }),
  ).toBeVisible()
  const search = page.getByRole('searchbox', { name: 'Rechercher un film' })
  await search.fill('absent')
  await search.press('Enter')
  await expect(page).toHaveURL(/q=absent/)
  await expect(
    page.getByRole('heading', { name: movie.title, level: 3, exact: true }),
  ).toHaveCount(0)
  await page.goBack()
  await expect(
    page.getByRole('heading', { name: 'Séances', exact: true }),
  ).toBeVisible()
  await page.goForward()
  await expect(page).toHaveURL(/q=absent/)
  await expect(search).toHaveValue('absent')
  await search.fill('')
  await search.press('Enter')
  await expect(page).not.toHaveURL(/q=absent/)
  await expect(
    page
      .getByRole('heading', { name: movie.title, level: 3, exact: true })
      .first(),
  ).toBeVisible()
})

test('missing cinema returns an SSR 404 and useful page', async ({ page }) => {
  const response = await openPage(page, '/cinema/missing')
  expect(response?.status()).toBe(404)
  await expect(
    page.getByRole('heading', { name: 'Cinéma introuvable', exact: true }),
  ).toBeVisible()
})
