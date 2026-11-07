import { test, expect, openPage } from './fixtures'

test('guide film mentions use catalogue pages and the scene selector keeps its own action', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { imaxGuideMovies: true },
  })
  await openPage(page, '/guides/imax#projection')
  await expect(
    page
      .locator('#projection')
      .getByRole('link', { name: 'L’Odyssée', exact: true }),
  ).toHaveAttribute('href', '/film/guide-odyssey')
  const comparison = page.locator('#formats .imax-comparison')
  await expect(
    comparison.getByRole('link', {
      name: 'Dune : Deuxième Partie ↗',
      exact: true,
    }),
  ).toHaveAttribute('href', '/film/guide-dune-2')
  await comparison
    .getByRole('button', { name: /Projet Dernière Chance/ })
    .click()
  await expect(
    comparison.getByRole('link', {
      name: 'Projet Dernière Chance ↗',
      exact: true,
    }),
  ).toHaveAttribute('href', '/film/guide-hail-mary')
  await page
    .getByText('Peut-on voir de l’IMAX 70 mm en France ?', { exact: true })
    .click()
  await expect(
    page
      .locator('#questions')
      .getByRole('link', { name: 'L’Odyssée', exact: true }),
  ).toHaveAttribute('href', '/film/guide-odyssey')
  await page
    .getByText('Comment le réseau français s’est-il développé ?', {
      exact: true,
    })
    .click()
  await expect(
    page.locator('#salles').getByRole('link', { name: 'Avatar', exact: true }),
  ).toHaveAttribute('href', '/film/guide-avatar')
  await expect(
    page
      .locator('#sources')
      .getByRole('link', { name: 'Dune : Deuxième Partie', exact: true }),
  ).toHaveAttribute('href', '/film/guide-dune-2')
})

test('guide serves readable content and metadata without depending on a cinema response', async ({
  page,
  request,
}, info) => {
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
  const heroFigure = page.locator('.imax-guide > header figure')
  if (info.project.name === 'mobile') await expect(heroFigure).toBeHidden()
  else await expect(heroFigure).toBeVisible()
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
  if (info.project.name === 'mobile')
    expect(
      await movies.evaluate(
        (element) => element.scrollWidth > element.clientWidth,
      ),
    ).toBe(true)
  for (const poster of await posters.all()) {
    await poster.scrollIntoViewIfNeeded()
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
  if (info.project.name === 'mobile') {
    const stripBounds = (await movies.boundingBox())!
    const selectedBounds = (await odyssey.boundingBox())!
    expect(selectedBounds.x).toBeGreaterThanOrEqual(stripBounds.x - 1)
    expect(selectedBounds.x + selectedBounds.width).toBeLessThanOrEqual(
      stripBounds.x + stripBounds.width + 1,
    )
  }
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
  await page.locator('#formats').screenshot({
    path: info.outputPath('imax-frame-comparison.png'),
    animations: 'disabled',
  })
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
  const topics = [
    'Quelles caméras utilise-t-on pour filmer en IMAX ?',
    'Que fait la remasterisation IMAX DMR ?',
    'Pourquoi parle-t-on de « 18K » pour la pellicule IMAX ?',
  ]
  const disclosures = page.locator('#projection details')
  await expect(disclosures).toHaveCount(3)
  for (const title of topics) {
    const disclosure = disclosures.filter({
      has: page.locator('summary', { hasText: title }),
    })
    await expect(disclosure).not.toHaveAttribute('open')
    await disclosure.locator('summary').click()
    await expect(disclosure).toHaveAttribute('open')
  }
  await expect(page.locator('#projection details[open]')).toHaveCount(3)
  const cameras = disclosures.filter({
    has: page.locator('summary', { hasText: topics[0] }),
  })
  const photo = cameras.getByRole('img')
  await photo.scrollIntoViewIfNeeded()
  await expect(photo).toHaveAttribute(
    'src',
    '/images/guides/camera-imax-1200.webp',
  )
  await expect(photo).toHaveJSProperty('complete', true)
  expect(
    await photo.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  await cameras.screenshot({
    path: info.outputPath('imax-cameras.png'),
    style:
      '#app-header, .guide-mobile-contents { visibility: hidden !important; }',
  })
  await cameras.locator('summary').focus()
  await page.keyboard.press('Enter')
  await expect(cameras).not.toHaveAttribute('open')
  await expect(page.locator('#projection details[open]')).toHaveCount(2)
})

test('guide highlights Odysseum and sends the full cinema list to the IMAX directory', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', {
    data: { historicalTechnologies: true },
  })
  await openPage(page, '/guides/imax#salles')
  const venues = page.locator('#salles')
  await expect(
    venues.getByRole('list', { name: 'Sélection de cinémas IMAX' }),
  ).toHaveCount(0)
  await expect(venues.getByRole('searchbox')).toHaveCount(0)
  const spotlight = venues.locator('article')
  await expect(spotlight.getByRole('heading', { level: 3 })).toHaveCount(1)
  await expect(spotlight.getByRole('heading', { level: 3 })).toContainText(
    'Le seul cinéma de France à proposer l’IMAX 70 mm en 1,43:1.',
  )
  await expect(
    venues.getByRole('link', { name: 'Montpellier', exact: true }).first(),
  ).toHaveAttribute('href', '/ville/montpellier/cinemas')
  await expect(
    spotlight.getByRole('link', { name: 'Montpellier', exact: true }),
  ).toHaveAttribute('href', '/ville/montpellier/cinemas')
  const otherFormats = venues.getByRole('region', {
    name: 'Les autres expériences IMAX en France',
    exact: true,
  })
  await expect(otherFormats.getByRole('heading', { level: 4 })).toHaveCount(2)
  await expect(otherFormats).toContainText('L’IMAX Laser en multiplexe')
  await expect(otherFormats).toContainText('L’IMAX sur un dôme')
  await expect(otherFormats.getByRole('list')).toHaveCount(0)
  await expect(
    venues.getByRole('link', {
      name: 'Voir les séances au Pathé Odysseum',
      exact: true,
    }),
  ).toHaveAttribute('href', '/cinemas?technologies=IMAX&q=Odysseum')
  const panorama = venues.getByRole('img', {
    name: /Façade panoramique du Pathé Odysseum/,
  })
  await panorama.scrollIntoViewIfNeeded()
  await expect(panorama).toHaveJSProperty('complete', true)
  expect(
    await panorama.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  await venues.screenshot({
    path: info.outputPath('imax-odysseum.png'),
    style:
      'header, nav, .guide-mobile-contents { visibility: hidden !important; }',
  })
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
  await openPage(page, '/guides/imax#questions')
  const initialContents = page.getByRole('navigation', {
    name:
      info.project.name === 'mobile'
        ? 'Sommaire mobile du guide'
        : 'Sommaire du guide',
    exact: true,
  })
  if (info.project.name === 'mobile')
    await expect(initialContents.locator('summary')).toContainText(
      'Questions fréquentes',
    )
  else
    await expect(
      initialContents.getByRole('link', { name: /Questions fréquentes/ }),
    ).toHaveAttribute('aria-current', 'location')
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
