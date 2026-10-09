import { test, expect, openPage } from './fixtures'

const path = '/films/prochainement'
const history = `${path}?vue=historique`

test('SSR default history resolves latest stored year, descending weeks, SEO and responsive layout', async ({
  page,
  request,
}, testInfo) => {
  const ssr = await request.get(history)
  expect(ssr.status()).toBe(200)
  const html = await ssr.text()
  expect(html).toContain('Déjà sortis')
  expect(html).toContain('Le premier novembre')
  expect(html).toContain('value="2025" selected')
  expect(html).toContain('noindex,follow')
  expect(html).toContain('Films déjà sortis au cinéma - MesSeances')
  expect(html).toContain(
    'Les films déjà sortis au cinéma en France, semaine par semaine.',
  )
  expect(html).not.toContain('Le premier jour de 2025')
  await openPage(page, history)
  await expect(page).toHaveURL(`${history}&annee=2025`)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(
    'Déjà sortis.',
  )
  await expect(page.getByLabel('Année', { exact: true })).toHaveValue('2025')
  await expect(page.getByLabel('Mois', { exact: true })).toHaveValue('')
  await expect(page.locator('main section h2')).toHaveText([
    '29 octobre 2025',
    '22 octobre 2025',
    '15 octobre 2025',
    '8 octobre 2025',
  ])
  await expect(
    page
      .getByRole('navigation', { name: 'Période des sorties' })
      .getByRole('link', { name: 'Déjà sortis', exact: true }),
  ).toHaveAttribute('aria-current', 'page')
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
    'href',
    /\/films\/prochainement$/,
  )
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute(
    'content',
    'noindex,follow',
  )
  await expect(page.getByRole('link', { name: 'Suivant' })).toHaveAttribute(
    'href',
    `${history}&annee=2025&page=2`,
  )
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy()
  await page.screenshot({
    path: testInfo.outputPath(`history-${testInfo.project.name}.png`),
    fullPage: true,
  })
})

test('French deep links render SSR filters, complete boundary weeks and last-page correction', async ({
  page,
  request,
}) => {
  const deep = `${history}&annee=2025&mois=10&page=2`
  const ssr = await request.get(deep)
  expect(ssr.status()).toBe(200)
  const html = await ssr.text()
  expect(html).toContain('Le début d’octobre')
  expect(html).not.toContain('Le premier novembre')
  await openPage(page, deep)
  await expect(page.getByLabel('Mois', { exact: true })).toHaveValue('10')
  await expect(page.locator('main section h2')).toHaveText(['1 octobre 2025'])
  await expect(page.getByRole('link', { name: 'Précédent' })).toHaveAttribute(
    'href',
    `${history}&annee=2025&mois=10`,
  )
  await page.getByRole('link', { name: 'Précédent' }).click()
  await expect(page.locator('main section h2')).toHaveText([
    '29 octobre 2025',
    '22 octobre 2025',
    '15 octobre 2025',
    '8 octobre 2025',
  ])
  await expect(
    page.getByRole('link', { name: 'La dernière nuit d’octobre' }),
  ).toBeVisible()
  await expect(
    page.getByRole('link', { name: 'Le premier novembre' }),
  ).toHaveCount(0)
  await openPage(page, `${history}&annee=2025&mois=10&page=999`)
  await expect(page).toHaveURL(deep)
  const state = await (await request.get('/__playwright/state')).json()
  expect(state.upcomingQueries.slice(-2)).toEqual([
    { view: 'history', year: '2025', month: '10', page: '999' },
    { view: 'history', year: '2025', month: '10', page: '2' },
  ])
  await openPage(page, `${history}&annee=2024&mois=1`)
  await expect(page.locator('main section h2')).toHaveText(['27 décembre 2023'])
  await expect(page.locator('main time')).toHaveAttribute(
    'datetime',
    '2024-01-01',
  )
})

test('switches, year/month changes and back/forward reset page and restore full URL state', async ({
  page,
}) => {
  await openPage(page, path)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(
    'Prochainement.',
  )
  await expect(page.getByLabel('Année', { exact: true })).toHaveCount(0)
  await expect(page.locator('main section h2')).toHaveText([
    '14 octobre 2026',
    '28 octobre 2026',
    '11 novembre 2026',
    '2 décembre 2026',
  ])
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute(
    'content',
    'index,follow',
  )
  await page
    .getByRole('navigation', { name: 'Période des sorties' })
    .getByRole('link', { name: 'Déjà sortis', exact: true })
    .click()
  await expect(page).toHaveURL(`${history}&annee=2025`)
  await page.getByRole('link', { name: 'Suivant' }).click()
  await expect(page).toHaveURL(`${history}&annee=2025&page=2`)
  await expect(page.locator('main section h2')).toHaveText([
    '1 octobre 2025',
    '24 septembre 2025',
    '17 septembre 2025',
    '1 janvier 2025',
  ])
  await page.getByLabel('Mois', { exact: true }).selectOption('10')
  await expect(page).toHaveURL(`${history}&annee=2025&mois=10`)
  await expect(page.locator('main section h2')).toHaveCount(4)
  await page.getByRole('link', { name: 'Suivant' }).click()
  await expect(page).toHaveURL(`${history}&annee=2025&mois=10&page=2`)
  await page.getByLabel('Année', { exact: true }).selectOption('2024')
  await expect(page).toHaveURL(`${history}&annee=2024`)
  await expect(page.getByLabel('Mois', { exact: true })).toHaveValue('')
  await page.goBack()
  await expect(page).toHaveURL(`${history}&annee=2025&mois=10&page=2`)
  await expect(page.getByLabel('Année', { exact: true })).toHaveValue('2025')
  await expect(page.getByLabel('Mois', { exact: true })).toHaveValue('10')
  await expect(page.locator('main section h2')).toHaveText(['1 octobre 2025'])
  await page.goForward()
  await expect(page).toHaveURL(`${history}&annee=2024`)
  await expect(page.getByLabel('Année', { exact: true })).toHaveValue('2024')
  const month = page.getByLabel('Mois', { exact: true })
  await month.focus()
  await expect(month).toBeFocused()
  await month.press('ArrowDown')
  await month.press('Enter')
  await expect(page).toHaveURL(`${history}&annee=2024&mois=1`)
  await month.selectOption('')
  await expect(page).toHaveURL(`${history}&annee=2024`)
  await page
    .getByRole('navigation', { name: 'Période des sorties' })
    .getByRole('link', { name: 'À venir', exact: true })
    .click()
  await expect(page).toHaveURL(path)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(
    'Prochainement.',
  )
  await expect(page.getByLabel('Année', { exact: true })).toHaveCount(0)
})

test('empty explicit periods remain selected; no eligible history leaves usable exit and disabled filters', async ({
  page,
  request,
}) => {
  await openPage(page, `${history}&annee=2023&mois=2&page=99`)
  await expect(page).toHaveURL(`${history}&annee=2023&mois=2`)
  await expect(
    page.getByText('Aucune sortie pour cette période', { exact: true }),
  ).toBeVisible()
  await expect(page.getByLabel('Année', { exact: true })).toHaveValue('2023')
  await expect(page.getByLabel('Année', { exact: true })).toBeEnabled()
  await expect(page.getByLabel('Mois', { exact: true })).toHaveValue('2')
  await expect(page.getByLabel('Mois', { exact: true })).toBeDisabled()
  await page.getByLabel('Année', { exact: true }).selectOption('2025')
  await expect(page).toHaveURL(`${history}&annee=2025`)
  await openPage(page, `${history}&annee=2025&mois=2`)
  await expect(page.getByLabel('Mois', { exact: true })).toHaveValue('2')
  await expect(page.getByLabel('Mois', { exact: true })).toBeEnabled()
  await page.getByLabel('Mois', { exact: true }).selectOption('')
  await expect(page).toHaveURL(`${history}&annee=2025`)
  await request.post('/__playwright/upcoming', { data: { mode: 'empty' } })
  const ssr = await request.get(history)
  expect(ssr.status()).toBe(200)
  expect(await ssr.text()).toContain('Aucune sortie pour cette période')
  await openPage(page, history)
  await expect(page).toHaveURL(history)
  await expect(page.getByLabel('Année', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('Année', { exact: true })).toBeDisabled()
  await expect(page.getByLabel('Mois', { exact: true })).toBeDisabled()
  await page
    .getByRole('navigation', { name: 'Période des sorties' })
    .getByRole('link', { name: 'À venir', exact: true })
    .click()
  await expect(
    page.getByText('Aucune sortie annoncée', { exact: true }),
  ).toBeVisible()
})

test('malformed route keys normalize without English aliases or upcoming filters', async ({
  page,
  request,
}) => {
  await openPage(
    page,
    `${path}?vue=history&annee=2025&mois=10&page=01&year=2024&unknown=x`,
  )
  await expect(page).toHaveURL(path)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(
    'Prochainement.',
  )
  await expect(page.getByLabel('Année', { exact: true })).toHaveCount(0)
  await openPage(page, `${history}&annee=10000&mois=13&page=0&view=history`)
  await expect(page).toHaveURL(`${history}&annee=2025`)
  await expect(page.getByLabel('Mois', { exact: true })).toHaveValue('')
  const state = await (await request.get('/__playwright/state')).json()
  expect(state.upcomingQueries).toEqual([
    { page: '1' },
    { view: 'history', page: '1' },
  ])
})

test('SSR failures return 502; client retry preserves failed period and loading hides stale cards', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/upcoming', { data: { mode: 'error' } })
  const failed = `${history}&annee=2024&mois=12`
  const ssr = await request.get(failed)
  expect(ssr.status()).toBe(502)
  expect(await ssr.text()).toContain(
    'L’historique des sorties n’est pas encore disponible. Réessayez plus tard.',
  )
  await openPage(page, failed)
  await expect(page.getByRole('alert')).toContainText(
    'L’historique des sorties n’est pas encore disponible.',
  )
  await request.post('/__playwright/upcoming', { data: { mode: 'populated' } })
  await page.getByRole('button', { name: 'Réessayer' }).click()
  await expect(page).toHaveURL(failed)
  await expect(
    page.getByRole('link', { name: 'Le dernier jour de 2024' }),
  ).toBeVisible()
  await expect(page.getByLabel('Mois', { exact: true })).toHaveValue('12')
  await request.post('/__playwright/upcoming', { data: { mode: 'delay' } })
  await page
    .getByRole('navigation', { name: 'Période des sorties' })
    .getByRole('link', { name: 'À venir', exact: true })
    .click()
  await expect(
    page.getByText('Chargement des sorties…', { exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('link', { name: 'Le dernier jour de 2024' }),
  ).toHaveCount(0)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(
    'Prochainement.',
  )
  await expect(
    page.getByRole('link', { name: 'Le prochain mercredi' }),
  ).toBeVisible()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy()
})
