import { test, expect, openPage } from './fixtures'

test('mobile view controls stay beside Filters while the long filter sheet scrolls', async ({
  page,
  request,
}, info) => {
  test.skip(info.project.name !== 'mobile', 'Mobile viewport coverage')
  await request.post('/__playwright/scenario', {
    data: {
      directoryTechnologies: true,
      directoryTechnologyGallery: true,
      directoryLongList: true,
    },
  })
  for (const viewport of [
    { width: 320, height: 568 },
    { width: 390, height: 844 },
    { width: 390, height: 568 },
    { width: 844, height: 390 },
  ]) {
    await page.setViewportSize(viewport)
    await openPage(page, '/cinemas')
    const switcher = page.getByRole('group', {
      name: 'Mode d’affichage des cinémas',
      exact: true,
    })
    const filterButton = page.getByRole('button', { name: /^Filtres/ })
    const switchBounds = (await switcher.boundingBox())!
    const filterBounds = (await filterButton.boundingBox())!
    expect(switchBounds.y).toBe(filterBounds.y)
    expect(filterBounds.x - switchBounds.x - switchBounds.width).toBeCloseTo(
      8,
      0,
    )
    await expect(
      switcher.getByRole('button', { name: 'Liste', exact: true }),
    ).toHaveAttribute('aria-pressed', 'true')
    await expect(
      switcher.getByRole('button', { name: 'Carte', exact: true }),
    ).toBeVisible()
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true)
    await page.locator('.selection-toolbar').screenshot({
      path: info.outputPath(`mobile-toolbar-${viewport.width}.png`),
    })
    await page.getByRole('button', { name: /^Filtres/ }).click()
    const dialog = page.getByRole('dialog', {
      name: 'Filtres des cinémas',
      exact: true,
    })
    await expect(
      dialog.getByRole('group', {
        name: 'Mode d’affichage des cinémas',
        exact: true,
      }),
    ).toHaveCount(0)
    const header = dialog.locator('header')
    const footer = dialog.locator('footer')
    const headerBounds = (await header.boundingBox())!
    const footerBounds = (await footer.boundingBox())!
    expect(headerBounds.y).toBeGreaterThanOrEqual(0)
    expect(footerBounds.y + footerBounds.height).toBeLessThanOrEqual(
      viewport.height,
    )
    const content = dialog.locator('.cinema-settings-content')
    await dialog
      .getByRole('checkbox', { name: 'Infinity Vision', exact: true })
      .focus()
    expect(
      await content.evaluate((element) => element.scrollTop),
    ).toBeGreaterThan(0)
    expect(await header.boundingBox()).toEqual(headerBounds)
    expect(await footer.boundingBox()).toEqual(footerBounds)
    expect(await dialog.evaluate((element) => element.scrollTop)).toBe(0)
    await dialog
      .getByRole('checkbox', { name: 'Infinity Vision', exact: true })
      .press('Space')
    await page.screenshot({
      path: info.outputPath(
        `mobile-long-filters-${viewport.width}-${viewport.height}.png`,
      ),
    })
    await dialog.getByRole('button', { name: 'Fermer les filtres' }).click()
    await expect(dialog).toBeHidden()
  }
})

test('filter overlay traps focus, restores scrolling and adapts across the sidebar breakpoint', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', {
    data: { directoryTechnologies: true },
  })
  await openPage(page, '/cinemas')
  const originalOverflow = await page.evaluate(
    () => document.documentElement.style.overflow,
  )
  const dialog = page.getByRole('dialog', {
    name: 'Filtres des cinémas',
    exact: true,
  })
  await page.getByRole('button', { name: /^Filtres/ }).click()
  const close = dialog.getByRole('button', { name: 'Fermer les filtres' })
  await expect(close).toBeFocused()
  expect(
    await page.evaluate(() => document.documentElement.style.overflow),
  ).toBe('hidden')
  await page.keyboard.press('Shift+Tab')
  await expect(
    dialog.getByRole('button', { name: /^Voir \d+ cinémas?$/ }),
  ).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(close).toBeFocused()
  const checkbox = dialog.getByRole('checkbox', { name: 'IMAX', exact: true })
  await checkbox.locator('..').click()
  const startedDesktop = info.project.name === 'desktop'
  await page.setViewportSize(
    startedDesktop ? { width: 390, height: 844 } : { width: 1440, height: 900 },
  )
  await expect(dialog).toBeVisible()
  await expect(checkbox).toBeChecked()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(page.getByRole('button', { name: /^Filtres/ })).toBeFocused()
  expect(
    await page.evaluate(() => document.documentElement.style.overflow),
  ).toBe(originalOverflow)
  await page.getByRole('button', { name: /^Filtres/ }).click()
  await page.mouse.click(startedDesktop ? 195 : 20, startedDesktop ? 2 : 200)
  await expect(dialog).toBeHidden()
})

test('filter overlay shows technology logos, 3D text and accessible names', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', {
    data: { directoryTechnologies: true, directoryTechnologyGallery: true },
  })
  await openPage(page, '/cinemas')
  const trigger = page.getByRole('button', { name: /^Filtres/ })
  const toolbar = page.locator('.selection-toolbar')
  const desktopToolbarBounds =
    info.project.name === 'desktop' ? await toolbar.boundingBox() : null
  const filters = page.getByRole('dialog', {
    name: 'Filtres des cinémas',
    exact: true,
  })
  await expect(filters).toBeHidden()
  await trigger.click()
  if (desktopToolbarBounds) {
    expect(await toolbar.boundingBox()).toEqual(desktopToolbarBounds)
  }
  const technologies = filters.getByRole('group', {
    name: 'Filtrer par technologie',
    exact: true,
  })
  await expect(technologies.getByRole('checkbox')).toHaveCount(8)
  const logos = technologies.locator('img')
  await expect(logos).toHaveCount(7)
  for (const logo of await logos.all()) {
    await expect(logo).toHaveAttribute('alt', '')
    await expect(logo).toHaveJSProperty('complete', true)
    expect(
      await logo.evaluate((image: HTMLImageElement) => image.naturalWidth),
    ).toBeGreaterThan(0)
  }
  for (const label of [
    '3D',
    'IMAX',
    'Dolby',
    'ScreenX',
    'Laser ULTRA by Kinepolis',
    '4DX',
    'ICE',
    'Infinity Vision',
  ])
    await expect(
      technologies.getByRole('checkbox', { name: label, exact: true }),
    ).toHaveCount(1)
  await technologies
    .getByRole('checkbox', { name: 'IMAX', exact: true })
    .locator('..')
    .click()
  await filters
    .getByRole('checkbox', { name: 'UGC', exact: true })
    .locator('..')
    .click()
  // Only 3D has visible text; image-only options retain checkbox names.
  for (const checkbox of await technologies.getByRole('checkbox').all()) {
    const value = await checkbox.getAttribute('value')
    await expect(checkbox.locator('..')).toHaveText(value === '3D' ? '3D' : '')
  }
  const bounds = (await filters.locator(':scope > div').boundingBox())!
  if (info.project.name === 'desktop') {
    expect(bounds.x).toBe(1440 - 512)
    expect(bounds.y).toBe(0)
    expect(bounds.height).toBe(900)
  } else {
    expect(bounds.x).toBe(0)
    expect(bounds.y).toBe(16)
    expect(bounds.width).toBe(390)
  }
  await technologies.scrollIntoViewIfNeeded()
  await page.screenshot({
    path: info.outputPath('technology-logos.png'),
    fullPage: info.project.name === 'desktop',
  })
  await filters.getByRole('button', { name: 'Fermer les filtres' }).click()
  await expect(filters).toBeHidden()
  await expect(trigger).toHaveAccessibleName('Filtres 2')
})

test('directory technology filtering combines with search and chains, restores URL history and resets', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', {
    data: { directoryTechnologies: true },
  })
  await openPage(page, '/cinemas?tag=kept')
  const panel = page.getByRole('group', {
    name: 'Filtrer par technologie',
    exact: true,
  })
  const all = page.locator('#cinema-section-all')
  const filterButton = page.getByRole('button', { name: /^Filtres(?: \d+)?$/ })
  const openSettings = async () => {
    await filterButton.click()
  }
  const closeSettings = async () => {
    await page.getByRole('button', { name: 'Fermer les filtres' }).click()
  }
  await openSettings()
  await expect(panel.getByRole('checkbox')).toHaveCount(3)
  await expect(
    panel.getByRole('checkbox', { name: '2D', exact: true }),
  ).toHaveCount(0)
  const imax = panel.getByRole('checkbox', { name: 'IMAX', exact: true })
  await imax.focus()
  await page.keyboard.press('Space')
  await expect(imax).toBeChecked()
  await expect(page).toHaveURL(/technologies=IMAX/)
  await closeSettings()
  await expect(
    all.getByRole('link', { name: /^Voir les séances/ }),
  ).toHaveCount(1)
  await expect(
    all.getByRole('link', { name: /^Voir les séances/ }),
  ).toContainText('Cinéma Playwright')
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute(
    'data-playwright-ready',
    'true',
  )
  await openSettings()
  await expect(imax).toBeChecked()
  await panel
    .getByRole('checkbox', { name: 'ICE', exact: true })
    .locator('..')
    .click()
  await closeSettings()
  await expect(
    all.getByRole('link', { name: /^Voir les séances/ }),
  ).toHaveCount(2)
  await page.goBack()
  await expect(page).toHaveURL(/technologies=IMAX(?:&|$)/)
  await expect(
    all.getByRole('link', { name: /^Voir les séances/ }),
  ).toHaveCount(1)
  await page.goForward()
  await expect(
    all.getByRole('link', { name: /^Voir les séances/ }),
  ).toHaveCount(2)
  await openSettings()
  await page
    .getByRole('checkbox', { name: 'CGR', exact: true })
    .locator('..')
    .click()
  await closeSettings()
  await expect(
    all.getByRole('link', { name: /^Voir les séances/ }),
  ).toHaveCount(1)
  await expect(
    all.getByRole('link', { name: /^Voir les séances/ }),
  ).toContainText('Cinéma Seconde Salle')
  await page.getByRole('searchbox').fill('Lille')
  await expect(
    page.getByText('Aucun cinéma ne correspond à votre recherche.'),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Effacer les filtres' }).click()
  await expect(page).toHaveURL(/\/cinemas\?tag=kept$/)
  await expect(
    all.getByRole('link', { name: /^Voir les séances/ }),
  ).toHaveCount(3)
  await openSettings()
  await expect(imax).not.toBeChecked()
  await imax.locator('..').click()
  await panel.getByRole('button', { name: 'Toutes les technologies' }).click()
  await expect(imax).not.toBeChecked()
  await expect(page).not.toHaveURL(/technologies=/)
  await page.screenshot({ path: info.outputPath('cinema-technologies.png') })
  await closeSettings()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
})

test('unavailable technology deep link remains clearable', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', {
    data: { directoryTechnologies: true },
  })
  await openPage(page, '/cinemas?technologies=DOLBY')
  await expect(
    page.getByText('Aucun cinéma ne correspond à votre recherche.'),
  ).toBeVisible()
  await page.getByRole('button', { name: /^Filtres/ }).click()
  const panel = page.getByRole('group', {
    name: 'Filtrer par technologie',
    exact: true,
  })
  await expect(
    panel.getByRole('checkbox', { name: 'Dolby', exact: true }),
  ).toBeChecked()
  await panel.getByRole('button', { name: 'Toutes les technologies' }).click()
  await expect(page).not.toHaveURL(/technologies=/)
  await expect(
    panel.getByRole('checkbox', { name: 'Dolby', exact: true }),
  ).toHaveCount(0)
})
