import { test, expect, openPage } from './fixtures'

test('guide serves readable content and metadata without depending on a cinema response', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { imaxGuideOffline: true },
  })
  const response = await request.get('/guides/imax')
  expect(response.status()).toBe(200)
  const html = await response.text()
  expect(html).toContain('L’IMAX en France,')
  expect(html).toContain('Laser, double laser, 70 mm')
  expect(html).toContain('application/ld+json')
  await openPage(page, '/guides/imax')
  await expect(
    page.getByText('L’annuaire est momentanément indisponible.', {
      exact: false,
    }),
  ).toBeVisible()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(
    /L’IMAX en France,\s*tout simplement\./,
  )
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
    'href',
    /\/guides\/imax$/,
  )
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute(
    'content',
    'index,follow',
  )
  const sitemap = await request.get('/sitemaps/guides.xml')
  expect(sitemap.status()).toBe(200)
  expect(await sitemap.text()).toContain('/guides/imax</loc>')
  expect(await (await request.get('/sitemap.xml')).text()).toContain(
    '/sitemaps/guides.xml',
  )
})

test('frame comparison changes the visible height without stretching the scene and handles a film without expanded image', async ({
  page,
}, info) => {
  await openPage(page, '/guides/imax#formats')
  const figure = page.locator('#formats figure')
  const image = figure.getByRole('img')
  await expect(figure.locator('figcaption')).toContainText('+26 %')
  const originalBounds = await image.boundingBox()
  await page
    .getByRole('radio', { name: 'IMAX 1,90 (1,90:1)', exact: true })
    .focus()
  await page.keyboard.press('ArrowRight')
  await expect(
    page.getByRole('radio', { name: 'IMAX 1,43 (1,43:1)', exact: true }),
  ).toBeChecked()
  await expect(figure.locator('figcaption')).toContainText('+67 %')
  await expect(image).toHaveAttribute('aria-label', /1,43:1/)
  expect(await image.boundingBox()).toEqual(originalBounds)
  await expect(figure.locator('.frame-mask').first()).toHaveCSS('height', '0px')
  await page.getByLabel('Choisir une scène').selectOption('odyssee-voilier')
  await expect(figure.locator('img')).toHaveAttribute(
    'src',
    /odyssee-voilier-1600.webp$/,
  )
  await expect(figure.locator('img')).toHaveJSProperty('complete', true)
  await page.getByLabel('Tester un film sans image étendue').check()
  await expect(figure.locator('figcaption')).toContainText('+0 %')
  await expect(figure.locator('figcaption')).toContainText(
    'Un film en Scope reste en Scope.',
  )
  await page
    .getByRole('radio', { name: 'IMAX 1,90 (1,90:1)', exact: true })
    .locator('..')
    .click()
  await expect(figure.locator('figcaption')).toContainText('+0 %')
  await page.getByLabel('Tester un film sans image étendue').uncheck()
  await expect(figure.locator('figcaption')).toContainText('+26 %')
  await page.getByLabel('Afficher les repères du Scope').check()
  await page
    .locator('#formats')
    .screenshot({ path: info.outputPath('imax-frame-comparison.png') })
})

test('projection explorer distinguishes 4K laser, 2K xenon and physical 70mm', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await openPage(page, '/guides/imax#projection')
  const result = page.locator('#imax-projection-result')
  await expect(result.getByRole('heading')).toHaveText(
    'IMAX Laser · simple laser',
  )
  await page
    .getByRole('radio', { name: 'Double laser GT', exact: true })
    .locator('..')
    .click()
  await expect(result.getByRole('heading')).toHaveText(
    'IMAX GT Laser · double laser',
  )
  await expect(result).toContainText('jusqu’à 1,43:1')
  await page
    .getByRole('radio', { name: 'Pellicule 70 mm', exact: true })
    .locator('..')
    .click()
  await expect(result).toContainText('IMAX 70 mm · 15/70')
  await expect(result).toContainText('15 perforations')
  await page
    .getByRole('radio', { name: 'Numérique xénon', exact: true })
    .locator('..')
    .click()
  await expect(result).toContainText('2K')
  await expect(result).toContainText(
    'ne constitue pas une projection 4K native',
  )
})

test('guide lists registered IMAX cinemas, supports search, and keeps the filter on fallback links', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { historicalTechnologies: true },
  })
  await openPage(page, '/guides/imax#salles')
  const directory = page.locator('#salles')
  await expect(
    directory.getByRole('link', { name: /Cinéma Playwright/ }),
  ).toHaveAttribute('href', '/cinema/cinema-playwright')
  await expect(directory.getByRole('status')).toContainText(
    '1 cinéma affiché sur 1 référencés avec IMAX',
  )
  await page
    .getByRole('searchbox', { name: 'Chercher une ville ou un cinéma' })
    .fill('inconnu')
  await expect(
    directory.getByText('Aucun cinéma ne correspond à votre recherche.'),
  ).toBeVisible()
  await directory.getByRole('button', { name: 'Effacer la recherche' }).click()
  await expect(
    directory.getByRole('link', { name: /Cinéma Playwright/ }),
  ).toBeVisible()
  await expect(
    directory.getByRole('link', { name: 'La Géode', exact: true }),
  ).toHaveAttribute('href', '/cinemas?technologies=IMAX&q=G%C3%A9ode')
  await directory
    .getByRole('link', { name: 'Ouvrir la liste avec le filtre IMAX ↗' })
    .click()
  await expect(page).toHaveURL(/\/cinemas\?technologies=IMAX$/)
  await expect(
    page
      .locator('#cinema-section-all')
      .getByRole('link', { name: /Voir les séances/ }),
  ).toHaveCount(1)
})

test('footer exposes Guides and the guide stays usable on small screens', async ({
  page,
}, info) => {
  await openPage(page, '/guides/imax')
  const footer = page.getByRole('contentinfo')
  await expect(
    page.getByText('Choisissez vos cinémas', { exact: true }),
  ).toBeHidden()
  await expect(
    footer.getByRole('heading', { name: 'Guides', exact: true }),
  ).toBeVisible()
  await expect(
    footer.getByRole('link', { name: 'L’IMAX en France', exact: true }),
  ).toHaveAttribute('href', '/guides/imax')
  await page.screenshot({
    path: info.outputPath('imax-guide.png'),
    fullPage: true,
  })
  if (info.project.name === 'mobile')
    await page.setViewportSize({ width: 320, height: 568 })
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)
  await page.getByRole('link', { name: /Questions fréquentes/ }).click()
  const question = page
    .locator('#questions')
    .getByText('Le 70 mm classique est-il de l’IMAX ?', { exact: true })
  await question.click()
  await expect(
    page
      .locator('#questions')
      .getByText('Non. Le 70 mm classique utilise généralement', {
        exact: false,
      }),
  ).toBeVisible()
})
