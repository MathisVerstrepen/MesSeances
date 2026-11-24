import { fileURLToPath } from 'node:url'
import { test, expect, openPage } from './fixtures'
import { secondTheater, theater } from './data.mjs'

test('empty homepage selection leads to the directory and updates on return', async ({
  page,
}, testInfo) => {
  await openPage(page, '/')
  const section = page.getByRole('region', {
    name: 'Mes cinémas.',
    exact: true,
  })
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

test('saved device cinemas show available photos and preserve links when a photo fails', async ({
  page,
  isMobile,
}, testInfo) => {
  const imageUrl = '/api/v1/theaters/ugc/fixture-cinema/image/1'
  let servePhoto = true
  await page.route('**/api/v1/theaters', (route) =>
    route.fulfill({
      json: [
        { ...theater, image: { url: imageUrl, width: 800, height: 195 } },
        secondTheater,
      ],
    }),
  )
  await page.route(`**${imageUrl}`, (route) =>
    servePhoto
      ? route.fulfill({
          contentType: 'image/webp',
          path: fileURLToPath(
            new URL(
              '../public/images/guides/pathe-odysseum-800.webp',
              import.meta.url,
            ),
          ),
        })
      : route.fulfill({ status: 404 }),
  )
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
  const section = page.getByRole('region', {
    name: 'Mes cinémas.',
    exact: true,
  })
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
  const link = cinemas.getByRole('link', {
    name: `Voir les séances : ${theater.name}`,
  })
  const withoutPhoto = cinemas.getByRole('link', {
    name: `Voir les séances : ${secondTheater.name}`,
  })
  const photo = link.locator('img[loading="lazy"]')
  const placeholder = link.locator('svg.lucide-clapperboard')
  const title = link.getByRole('heading')
  await expect(photo).toHaveCSS('opacity', '1')
  await expect(title).toHaveCSS('color', 'rgb(39, 39, 42)')
  await expect(placeholder).toHaveCount(0)
  await expect(withoutPhoto.locator('img[loading="lazy"]')).toHaveCount(0)
  await expect(withoutPhoto.locator('svg.lucide-clapperboard')).toBeVisible()
  if (isMobile) {
    const viewport = page.viewportSize() ?? { width: 390, height: 844 }
    for (const width of [320, 375]) {
      await page.setViewportSize({ ...viewport, width })
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBeTruthy()
    }
    await page.setViewportSize(viewport)
  }
  await section.scrollIntoViewIfNeeded()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy()
  await section.screenshot({
    path: testInfo.outputPath('home-cinemas-selected.png'),
  })
  await link.focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/cinema/${theater.slug}$`))
  await page.goBack()
  servePhoto = false
  await page.reload()
  await expect(cinemas.getByRole('link')).toHaveCount(2)
  await expect(photo).toHaveJSProperty('complete', true)
  await expect(photo).toHaveCSS('opacity', '0')
  await expect(placeholder).toHaveCSS('stroke', 'rgb(39, 39, 42)')
  await expect(placeholder).toHaveCSS('opacity', '0.35')
  await expect(title).toHaveCSS('color', 'rgb(39, 39, 42)')
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
  const section = page.getByRole('region', {
    name: 'Mes cinémas.',
    exact: true,
  })
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
