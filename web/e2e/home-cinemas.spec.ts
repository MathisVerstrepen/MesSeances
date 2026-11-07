import { test, expect, openPage } from './fixtures'
import { secondTheater, theater } from './data.mjs'

test('empty homepage selection leads to the directory and updates on return', async ({
  page,
}, testInfo) => {
  await openPage(page, '/')
  const section = page.getByRole('region', { name: 'Mes cinémas.' })
  await expect(section).toHaveAttribute('aria-busy', 'false')
  const choose = section.getByRole('link', { name: 'Choisir mes cinémas' })
  await expect(choose).toHaveAttribute('href', '/cinemas')
  const neighbors = await section.evaluate((element) => ({
    before: element.previousElementSibling?.getAttribute('aria-labelledby'),
    after: element.nextElementSibling?.getAttribute('aria-labelledby'),
  }))
  expect(neighbors).toEqual({
    before: 'now-showing-title',
    after: 'shortcuts-title',
  })
  await section.scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('home-cinemas-empty.png') })
  await choose.click()
  await expect(page).toHaveURL(/\/cinemas$/)
  const checkbox = page
    .getByRole('region', { name: /^Tous les cinémas\b/ })
    .getByRole('checkbox', {
      name: `Sélectionner ${theater.name}`,
      exact: true,
    })
  await expect(checkbox).toBeEnabled()
  await checkbox.focus()
  await page.keyboard.press('Space')
  await expect(checkbox).toBeChecked()
  await page.goBack()
  await expect(
    section.getByRole('link', { name: `Voir les séances : ${theater.name}` }),
  ).toBeVisible()
  await expect(choose).toHaveCount(0)
})

test('saved device cinemas provide direct showtime links across reloads', async ({
  page,
}, testInfo) => {
  await page.addInitScript(
    (ids) => {
      localStorage.setItem(
        'messeances.favoriteTheaterIds.v1',
        JSON.stringify(ids),
      )
    },
    [theater.id, secondTheater.id],
  )
  await openPage(page, '/')
  const section = page.getByRole('region', { name: 'Mes cinémas.' })
  const cinemas = section.getByRole('list', {
    name: 'Mes cinémas sélectionnés',
  })
  await expect(cinemas.getByRole('link')).toHaveCount(2)
  for (const entry of [theater, secondTheater]) {
    const link = cinemas.getByRole('link', {
      name: `Voir les séances : ${entry.name}`,
    })
    await expect(link).toHaveAttribute('href', `/cinema/${entry.slug}`)
    await expect(link).toContainText(entry.city)
    await expect(link).toContainText(entry.address)
  }
  await section.scrollIntoViewIfNeeded()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy()
  await page.screenshot({
    path: testInfo.outputPath('home-cinemas-selected.png'),
  })
  const link = cinemas.getByRole('link', {
    name: `Voir les séances : ${theater.name}`,
  })
  await link.focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/cinema/${theater.slug}$`))
  await page.goBack()
  await page.reload()
  await expect(cinemas.getByRole('link')).toHaveCount(2)
})

test('account selection replaces device selection after hydration without private SSR content', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: {
      enabled: true,
      state: 'complete',
      selected_ids: [secondTheater.id],
    },
  })
  await page.addInitScript((id) => {
    localStorage.setItem(
      'messeances.favoriteTheaterIds.v1',
      JSON.stringify([id]),
    )
  }, theater.id)
  const html = await (await request.get('/')).text()
  expect(html).not.toContain('Mes cinémas sélectionnés')
  expect(html).not.toContain('theater_ids')
  await openPage(page, '/')
  const section = page.getByRole('region', { name: 'Mes cinémas.' })
  const cinemas = section.getByRole('list', {
    name: 'Mes cinémas sélectionnés',
  })
  await expect(cinemas.getByRole('link')).toHaveCount(1)
  await expect(
    cinemas.getByRole('link', {
      name: `Voir les séances : ${secondTheater.name}`,
    }),
  ).toBeVisible()
  await expect(
    cinemas.getByRole('link', { name: `Voir les séances : ${theater.name}` }),
  ).toHaveCount(0)
})
