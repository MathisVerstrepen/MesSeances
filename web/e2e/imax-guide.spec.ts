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
  const figure = page.locator('#formats .imax-comparison')
  const image = figure.getByRole('img')
  const frame = page.locator('#imax-scene-preview img')
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
  await expect(frame).toHaveAttribute('src', /dune-1600.webp$/)
  const movieBounds = await movies.boundingBox()
  if (info.project.name === 'desktop') {
    expect(movieBounds!.x).toBeGreaterThanOrEqual(
      originalBounds!.x + originalBounds!.width,
    )
  } else {
    expect(movieBounds!.y).toBeGreaterThanOrEqual(
      originalBounds!.y + originalBounds!.height,
    )
  }
  const posters = movies.locator('img')
  await expect(posters).toHaveCount(3)
  await movies.scrollIntoViewIfNeeded()
  for (const poster of await posters.all()) {
    await expect(poster).toHaveJSProperty('complete', true)
    expect(
      await poster.evaluate((image: HTMLImageElement) => image.naturalWidth),
    ).toBeGreaterThan(0)
  }
  await page
    .getByRole('radio', { name: 'IMAX 1,90 (1,90:1)', exact: true })
    .focus()
  await page.keyboard.press('ArrowUp')
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
  await expect(frame).toHaveAttribute('src', /project-hail-mary-1600.webp$/)
  await expect(
    movies.getByRole('button', {
      name: /Projet Dernière Chance Phil Lord et Christopher Miller/,
    }),
  ).toHaveAttribute('aria-pressed', 'true')
  await expect(
    movies.getByRole('button', { name: /Dune : Deuxième Partie/ }),
  ).toHaveAttribute('aria-pressed', 'false')
  await expect(frame).toHaveJSProperty('complete', true)
  expect(
    await frame.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  const odyssey = movies.getByRole('button', {
    name: /L’Odyssée Christopher Nolan/,
  })
  await odyssey.focus()
  await page.keyboard.press('Enter')
  await expect(odyssey).toHaveAttribute('aria-pressed', 'true')
  await expect(frame).toHaveAttribute('src', /odyssee-voilier-1600.webp$/)
  await expect(frame).toHaveJSProperty('complete', true)
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
}, info) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await openPage(page, '/guides/imax#projection')
  const result = page.locator('#imax-projection-result')
  const technical = page.locator('#imax-projection-technical')
  await expect(technical).toHaveCount(0)
  await expect(result.getByRole('heading')).toHaveText(
    'IMAX Laser · simple laser',
  )
  await page
    .getByRole('radio', { name: 'Double laser', exact: true })
    .locator('..')
    .click()
  await expect(result.getByRole('heading')).toHaveText('IMAX · double laser')
  await expect(result).toContainText('Jusqu’à 1,43:1')
  await page
    .getByRole('radio', { name: 'Pellicule 70 mm', exact: true })
    .locator('..')
    .click()
  await expect(result).toContainText('Pellicule 15/70')
  await page
    .getByRole('button', { name: 'Détails techniques', exact: true })
    .click()
  await expect(page.locator('#imax-projection-technical')).toContainText(
    '15 perforations',
  )
  await page
    .getByRole('radio', { name: 'Xénon 2K', exact: true })
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
  await expect(page.locator('#imax-projection-technical')).toContainText(
    'ne constitue pas une projection 4K native',
  )
  for (const [label, id] of [
    ['Laser 4K', 'laser'],
    ['Double laser', 'gt'],
    ['Pellicule 70 mm', 'film'],
    ['Xénon 2K', 'xenon'],
  ]) {
    await page
      .getByRole('radio', { name: label, exact: true })
      .locator('..')
      .click()
    await expect(technical.getByRole('heading')).toHaveCount(4)
    const photo = technical.getByRole('img')
    await photo.scrollIntoViewIfNeeded()
    await expect(photo).toHaveAttribute(
      'src',
      `/images/guides/projecteur-imax-${id}.webp`,
    )
    await expect(photo).toHaveJSProperty('complete', true)
    expect(
      await photo.evaluate((image: HTMLImageElement) => image.naturalWidth),
    ).toBeGreaterThan(0)
    if (id === 'laser' || id === 'gt') {
      await technical.screenshot({
        path: info.outputPath(`imax-technical-${id}.png`),
      })
    }
  }
  await page
    .getByRole('button', { name: 'Détails techniques', exact: true })
    .click()
  await expect(technical).toHaveCount(0)
  await expect(result).toContainText('2K')
})

test('guide shows only sourced venue capabilities and sends the full cinema list to the IMAX directory', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { historicalTechnologies: true },
  })
  await openPage(page, '/guides/imax#salles')
  const venues = page.locator('#salles')
  const selection = venues.getByRole('list', {
    name: 'Sélection de cinémas IMAX',
  })
  await expect(selection.getByRole('listitem')).toHaveCount(4)
  await expect(venues.getByRole('searchbox')).toHaveCount(0)
  const cannes = selection.getByRole('listitem').filter({
    has: page.getByRole('link', { name: 'Cineum Cannes', exact: true }),
  })
  await expect(cannes).toContainText('IMAX Laser 4K')
  await expect(cannes).not.toContainText('Cadre maximal')
  const dome = selection
    .getByRole('listitem')
    .filter({ has: page.getByRole('link', { name: 'La Géode', exact: true }) })
  await expect(dome).toContainText('Dôme hémisphérique')
  await expect(dome.getByRole('link')).toHaveAttribute(
    'href',
    '/cinemas?technologies=IMAX&q=G%C3%A9ode',
  )
  await expect(page.locator('#source-cineum a')).toHaveAttribute(
    'href',
    'https://www.cineum.fr/nos-salles/',
  )
  await expect(
    venues.getByRole('link', { name: 'Voir la carte', exact: true }),
  ).toHaveAttribute('href', '/cinemas?technologies=IMAX&view=map')
  expect(
    await page
      .locator('article > section')
      .evaluateAll((sections) => sections.map((section) => section.id)),
  ).toEqual([
    'essentiel',
    'formats',
    'projection',
    'experience',
    'choisir',
    'salles',
    'questions',
    'sources',
  ])
  await venues
    .getByRole('link', { name: 'Voir tous les cinémas IMAX', exact: true })
    .click()
  await expect(page).toHaveURL(/\/cinemas\?technologies=IMAX$/)
  await expect(
    page
      .locator('#cinema-section-all')
      .getByRole('link', { name: /Voir les séances/ }),
  ).toHaveCount(1)
})

test('contents follows the reading position and mobile navigation closes accessibly without hiding headings', async ({
  page,
}, info) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await openPage(page, '/guides/imax')
  await page
    .getByRole('link', { name: 'Comparer les formats', exact: true })
    .click()
  await expect(page.locator('#formats-title')).toBeInViewport()
  const mobile = info.project.name === 'mobile'
  const contents = page.getByRole('navigation', {
    name: mobile ? 'Sommaire mobile du guide' : 'Sommaire du guide',
    exact: true,
  })
  if (mobile) {
    await contents.locator('summary').click()
    await expect(
      contents.getByRole('link', { name: /Comparer l’image/ }),
    ).toHaveAttribute('aria-current', 'location')
  }
  await contents.getByRole('link', { name: /Choisir sa séance/ }).click()
  await expect(page.locator('#choisir-title')).toBeInViewport()
  await expect(page).toHaveURL(/#choisir$/)
  if (mobile) {
    await expect(contents.locator('details')).not.toHaveAttribute('open')
    await expect(contents.locator('summary')).toContainText('Choisir sa séance')
    const navBounds = (await contents.boundingBox())!
    const headerBounds = (await page.locator('#app-header').boundingBox())!
    expect(Math.abs(navBounds.y - headerBounds.height)).toBeLessThan(2)
    await contents.locator('summary').focus()
    await page.keyboard.press('Enter')
    await expect(contents.locator('details')).toHaveAttribute('open', '')
    await page.keyboard.press('Escape')
    await expect(contents.locator('summary')).toBeFocused()
    await expect(contents.locator('details')).not.toHaveAttribute('open')
  } else {
    await expect(
      contents.getByRole('link', { name: /Choisir sa séance/ }),
    ).toHaveAttribute('aria-current', 'location')
  }
  await page
    .locator('#salles')
    .evaluate((section) =>
      section.scrollIntoView({ behavior: 'instant', block: 'start' }),
    )
  if (mobile)
    await expect(contents.locator('summary')).toContainText(
      'Les salles en France',
    )
  else
    await expect(
      contents.getByRole('link', { name: /Les salles en France/ }),
    ).toHaveAttribute('aria-current', 'location')
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
    footer.getByRole('link', { name: 'IMAX : le guide complet', exact: true }),
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
  if (info.project.name === 'mobile')
    await page
      .getByRole('navigation', {
        name: 'Sommaire mobile du guide',
        exact: true,
      })
      .locator('summary')
      .click()
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
