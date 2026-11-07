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
  expect(html).toContain('IMAX, le guide complet')
  expect(html).toContain('Laser, double laser, 70 mm')
  expect(html).toContain('application/ld+json')
  await openPage(page, '/guides/imax')
  await expect(
    page.getByText('L’annuaire est momentanément indisponible.', {
      exact: false,
    }),
  ).toBeVisible()
  await expect(page.getByRole('heading', { level: 1 })).toHaveAccessibleName(
    'IMAX le guide complet',
  )
  await expect(
    page
      .getByRole('heading', { level: 1 })
      .getByRole('img', { name: 'IMAX', exact: true }),
  ).toHaveJSProperty('complete', true)
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

test('frame comparison changes the visible height without stretching the scene', async ({
  page,
}, info) => {
  await openPage(page, '/guides/imax#formats')
  const figure = page.locator('#formats figure')
  const image = figure.getByRole('img')
  await expect(figure.locator('figcaption')).toContainText('+26 %')
  const originalBounds = await image.boundingBox()
  const movies = page.getByRole('group', {
    name: 'Choisir un film',
    exact: true,
  })
  await expect(movies.getByRole('button')).toHaveCount(3)
  await expect(page.locator('#formats').getByRole('combobox')).toHaveCount(0)
  await expect(
    movies.getByRole('button', { name: /Dune : Deuxième Partie/ }),
  ).toHaveAttribute('aria-pressed', 'true')
  await expect(figure.locator('img')).toHaveAttribute('src', /dune-1600.webp$/)
  const movieBounds = await movies.boundingBox()
  expect(movieBounds!.y).toBeGreaterThanOrEqual(
    originalBounds!.y + originalBounds!.height,
  )
  await page
    .getByRole('radio', { name: 'IMAX 1,90 (1,90:1)', exact: true })
    .focus()
  await page.keyboard.press('ArrowRight')
  await expect(
    page.getByRole('radio', { name: 'IMAX 1,43 (1,43:1)', exact: true }),
  ).toBeChecked()
  await expect(figure.locator('figcaption')).toContainText('+67 %')
  await expect(image).toHaveAttribute('aria-label', /1,43:1/)
  const expandedBounds = await image.boundingBox()
  expect(expandedBounds!.width).toBe(originalBounds!.width)
  expect(expandedBounds!.height).toBe(originalBounds!.height)
  await expect(figure.locator('.frame-mask').first()).toHaveCSS('height', '0px')
  await movies
    .getByRole('button', {
      name: /Projet Dernière Chance Phil Lord et Christopher Miller/,
    })
    .click()
  await expect(figure.locator('img')).toHaveAttribute(
    'src',
    /project-hail-mary-1600.webp$/,
  )
  await expect(
    movies.getByRole('button', {
      name: /Projet Dernière Chance Phil Lord et Christopher Miller/,
    }),
  ).toHaveAttribute('aria-pressed', 'true')
  await expect(
    movies.getByRole('button', { name: /Dune : Deuxième Partie/ }),
  ).toHaveAttribute('aria-pressed', 'false')
  await expect(figure.locator('img')).toHaveJSProperty('complete', true)
  expect(
    await figure
      .locator('img')
      .evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  const odyssey = movies.getByRole('button', {
    name: /L’Odyssée Christopher Nolan/,
  })
  await odyssey.focus()
  await page.keyboard.press('Enter')
  await expect(odyssey).toHaveAttribute('aria-pressed', 'true')
  await expect(figure.locator('img')).toHaveAttribute(
    'src',
    /odyssee-voilier-1600.webp$/,
  )
  await expect(figure.locator('img')).toHaveJSProperty('complete', true)
  await page
    .getByRole('radio', { name: 'Cinéma Scope (2,39:1)', exact: true })
    .locator('..')
    .click()
  await expect(figure.locator('figcaption')).toContainText('+0 %')
  await page
    .getByRole('radio', { name: 'IMAX 1,90 (1,90:1)', exact: true })
    .locator('..')
    .click()
  await expect(figure.locator('figcaption')).toContainText('+26 %')
  await expect(page.locator('#formats').getByRole('checkbox')).toHaveCount(0)
  await expect(page.locator('#formats')).not.toContainText(
    'Scans de photogrammes',
  )
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
  const strip = page.getByRole('img', { name: /Bande de pellicule IMAX 70 mm/ })
  await strip.scrollIntoViewIfNeeded()
  await expect(strip).toBeVisible()
  await expect(strip).toHaveJSProperty('complete', true)
  expect(
    await strip.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
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
  await expect(page.locator('#alternatives')).toHaveCount(0)
  await expect(
    page.getByText('LE GUIDE MESSEANCES · N° 01', { exact: true }),
  ).toHaveCount(0)
  await expect(page.getByText(/Lecture à votre rythme/)).toHaveCount(0)
  await expect(page.locator('main a[href^="#source-"]')).toHaveCount(0)
  await expect(page.locator('#source-imax a')).toHaveAttribute(
    'href',
    'https://www.imax.com/the-imax-experience',
  )
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
  await expect(page).toHaveURL(/#questions$/)
  await expect(page.locator('#questions-title')).toBeInViewport()
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
