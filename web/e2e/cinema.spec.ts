import type { Locator } from '@playwright/test'
import { test, expect, openPage } from './fixtures'
import {
  date,
  movie,
  movieSchedule,
  showtimes,
  theater,
  veoTheaters,
} from './data.mjs'

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
  for (const viewport of ['mobile', 'desktop']) {
    expect(html).toMatch(
      new RegExp(`id="cinema-${viewport}-result-layout"[\\s\\S]*?</button>`),
    )
    const selector = html.match(
      new RegExp(`id="cinema-${viewport}-result-layout"[\\s\\S]*?</button>`),
    )![0]
    expect(selector).toContain('Boîtes')
    expect(selector).not.toContain('Lignes')
  }
  expect(html).toContain(
    `/statistiques?period=all&amp;theater=${encodeURIComponent(theater.id)}`,
  )
})

test('film synopsis resets on changed schedule and external menu keeps real keyboard focus and safe destinations', async ({
  page,
  isMobile,
}) => {
  const tomorrow = new Date(`${date}T12:00:00Z`)
  tomorrow.setUTCDate(tomorrow.getUTCDate() + 1)
  const nextDate = tomorrow.toISOString().slice(0, 10)
  const overview =
    'Un synopsis volontairement long pour vérifier le déploiement du texte et son retour au mode réduit. '.repeat(
      40,
    )
  await page.route('**/api/v1/movies/*/showtimes*', async (route) => {
    const query = new URL(route.request().url()).searchParams
    const payload = movieSchedule('film-playwright', query)
    payload.available_dates = [date, nextDate]
    Object.assign(payload.movie, {
      overview: query.get('date') === nextDate ? 'Résumé court.' : overview,
      tmdb_id: 123,
      imdb_id: 'tt1234567',
      metacritic_id: 'movie/fixture-film',
    })
    await route.fulfill({ json: payload })
  })
  await openPage(page, `/cinema/${theater.slug}`)
  await page
    .locator('a[href="/film/film-playwright?shared_theaters=fixture-cinema"]')
    .click()
  const synopsis = page.getByRole('button', {
    name: overview.trim(),
    exact: true,
  })
  await expect(synopsis).toHaveAttribute('aria-expanded', 'false')
  await synopsis.press('Enter')
  await expect(synopsis).toHaveAttribute('aria-expanded', 'true')
  await expect(synopsis).toHaveJSProperty(
    'scrollHeight',
    await synopsis.evaluate((el) => el.clientHeight),
  )
  const menuTrigger = page.getByRole('button', {
    name: 'Voir les liens externes de Film Playwright',
  })
  await menuTrigger.press('ArrowDown')
  const menu = page.getByRole('menu', {
    name: 'Liens externes de Film Playwright',
  })
  const links = menu.getByRole('menuitem')
  await expect(links).toHaveCount(5)
  await expect(links.first()).toBeFocused()
  for (const [key, target] of [
    ['ArrowUp', links.last()],
    ['ArrowDown', links.first()],
    ['End', links.last()],
    ['Home', links.first()],
  ] as const) {
    await page.keyboard.press(key)
    await expect(target).toBeFocused()
  }
  for (const link of await links.all()) {
    await expect(link).toHaveAttribute('href', /^https:\/\//)
    await expect(link).toHaveAttribute('target', '_blank')
    await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    await expect(link).toHaveAccessibleName(/nouvel onglet/)
  }
  await page.keyboard.press('Escape')
  await expect(menu).toBeHidden()
  await expect(menuTrigger).toBeFocused()
  // Structured data belongs to nationwide SSR, not this client-side scoped view.
  const document = await page.request.get('/film/film-playwright')
  expect(document.ok()).toBe(true)
  const schemas = [
    ...(await document.text()).matchAll(
      /<script\b[^>]*type="application\/ld\+json"[^>]*>([\s\S]*?)<\/script>/g,
    ),
  ].map((match) => JSON.parse(match[1]))
  expect(
    schemas
      .flatMap((schema) => schema['@graph'] || [schema])
      .find((schema) => schema['@type'] === 'Movie').url,
  ).toBe('http://127.0.0.1:13400/film/film-playwright')
  if (isMobile) await page.locator('#mobile-date-trigger').click()
  const tabs = page.locator('[role="tablist"]:visible').getByRole('tab')
  await tabs.filter({ hasText: 'Demain' }).click()
  await expect(page).toHaveURL(new RegExp(`date=${nextDate}`))
  await expect(
    page.getByRole('button', { name: 'Résumé court.', exact: true }),
  ).toBeDisabled()
  if (isMobile) await expect(page.locator('#mobile-date-panel')).toBeVisible()
  await tabs.filter({ hasText: 'Aujourd’hui' }).click()
  await expect(synopsis).toHaveAttribute('aria-expanded', 'false')
  await expect
    .poll(() => synopsis.evaluate((el) => el.scrollHeight > el.clientHeight))
    .toBe(true)
})

test('estimated end descriptions have unique associations, focus/Escape behavior and never activate booking', async ({
  page,
}) => {
  await page.route('**/api/v1/movies/*/showtimes*', async (route) => {
    const payload = movieSchedule(
      'film-playwright',
      new URL(route.request().url()).searchParams,
    )
    for (const theater of payload.theaters)
      for (const session of theater.showtimes) {
        session.end_time = session.start_time
        session.estimated_end_time = `${date}T15:45:00+02:00`
        session.estimated_end_ads_minutes = 15
        session.booking_url = 'https://www.ugc.fr/reservation'
      }
    await route.fulfill({ json: payload })
  })
  await openPage(page, `/cinema/${theater.slug}`)
  await page.locator('a[href="/film/film-playwright"]').click()
  await expect(
    page.locator('a[href="https://www.ugc.fr/reservation"]').first(),
  ).toBeVisible()
  const triggers = page.locator('span[tabindex="0"][aria-describedby]:visible')
  await expect(triggers).toHaveCount(3)
  const trigger = triggers.first()
  const id = await trigger.getAttribute('aria-describedby')
  const tooltip = page.locator(`[id="${id}"]`)
  await expect(tooltip).toBeHidden()
  await trigger.focus()
  await expect(tooltip).toBeVisible()
  await expect(tooltip).toContainText(
    /début annoncé à .* \+ 15 min de publicités \+ 90 min de film/,
  )
  const url = page.url()
  await trigger.click()
  await expect(page).toHaveURL(url)
  expect(page.context().pages()).toHaveLength(1)
  await page.keyboard.press('Escape')
  await expect(tooltip).toBeHidden()
  await expect(trigger).toBeFocused()
  const ids = await page
    .locator('[role="tooltip"]')
    .evaluateAll((nodes) => nodes.map((node) => node.id))
  expect(new Set(ids).size).toBe(ids.length)
})

test('rendered legal pages retain privacy disclosures, credit links and canonical robots policy', async ({
  page,
}) => {
  for (const [path, title, robots] of [
    ['/confidentialite', 'Confidentialité', 'index,follow'],
    ['/credits', 'Crédits', 'noindex,follow'],
  ]) {
    await openPage(page, path)
    await expect(
      page.getByRole('heading', { name: title, exact: true, level: 1 }),
    ).toBeVisible()
    await expect(page.locator('meta[name="robots"]')).toHaveAttribute(
      'content',
      robots,
    )
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
      'href',
      `http://127.0.0.1:13400${path}`,
    )
    const content = page.getByRole('main')
    if (path === '/confidentialite')
      for (const disclosure of [
        /messeances.favoriteTheaterIds.v1/,
        /distincte de celle du compte/,
        /enregistrés sur le serveur et associés à ce compte/,
        /Ce sont des données personnelles/,
        /sélection du compte prévaut sur celle de chaque appareil/,
        /sans remplacer la sélection locale du navigateur/,
        /importés une seule fois dans le compte/,
        /aucun import n’a lieu et le compte attend un enregistrement explicite/,
        /pas d’une synchronisation instantanée entre appareils/,
        /changements non enregistrés doivent être réappliqués/,
        /suppression du compte ne supprime pas la sélection locale/,
        /supprime cette sélection locale, sans supprimer celle du compte/,
        /Jusqu’à leur remplacement ou la suppression du compte dans la base active/,
        /aucune suppression immédiate de ces copies n’est promise/,
        /cette notice n’annonce pas leur activation en production/,
        /efface cette liste et ses tags/,
      ])
        await expect(content).toContainText(disclosure)
    else {
      await expect(content).toContainText(
        /uses the TMDB API but is not endorsed or certified by TMDB/,
      )
      for (const host of [
        'themoviedb.org',
        'infinityvisiontickets.com',
        'ugc.fr',
        'kinepolis.fr',
        'pathe.fr',
        'cgrcinemas.fr',
        'openfreemap.org',
        'openmaptiles.org',
        'openstreetmap.org',
        'cineville.fr',
        'cinewest.fr',
        'grandecran.fr',
        'megarama.fr',
        'mk2.com',
      ])
        await expect(
          content.locator(`a[href*="${host}"]`).first(),
        ).toHaveAttribute('rel', 'noopener noreferrer')
    }
  }
})

async function expectHeaderLayout(
  header: Locator,
  desktop: boolean,
  expectedAddress = theater.address,
) {
  await expect(header.getByText('Programmation', { exact: true })).toHaveCount(
    0,
  )
  await expect(header.getByText(/dates? disponibles?/)).toHaveCount(0)
  const statistics = header.getByRole('link', {
    name: 'Statistiques',
    exact: true,
  })
  await expect(statistics).toHaveCount(1)
  await expect(statistics).toBeVisible()
  await expect(statistics).toHaveAttribute(
    'href',
    `/statistiques?period=all&theater=${encodeURIComponent(theater.id)}`,
  )
  const heroElement = header.locator(':scope > div').first()
  const stripElement = header.locator(':scope > div').last()
  await expect(heroElement.locator('p:visible')).toContainText(
    desktop ? expectedAddress || theater.city : theater.city,
  )
  const hero = await heroElement.boundingBox()
  const button = await statistics.boundingBox()
  const follow = header.getByRole('button', {
    name: 'Suivre ce cinéma',
    exact: true,
  })
  const followBox = (await follow.count()) ? await follow.boundingBox() : null
  if (followBox) {
    expect(followBox.width).toBeGreaterThanOrEqual(44)
    expect(followBox.height).toBeGreaterThanOrEqual(44)
    expect(button!.x - followBox.x - followBox.width).toBeCloseTo(8, 0)
  }
  expect(hero).not.toBeNull()
  expect(button).not.toBeNull()
  expect(button!.width).toBeGreaterThanOrEqual(44)
  expect(button!.height).toBeGreaterThanOrEqual(44)
  if (desktop) {
    await expect(stripElement).toBeHidden()
    await statistics.focus()
    await header.page().keyboard.press('Shift+Tab')
    await header.page().keyboard.press('Tab')
    await expect(statistics).toBeFocused()
    expect(
      await statistics.evaluate((link) => getComputedStyle(link).outlineStyle),
    ).toBe('solid')
    const headerBox = await header.boundingBox()
    expect(headerBox!.height).toBeCloseTo(hero!.height + 4, 0)
    expect(hero!.x + hero!.width - button!.x - button!.width).toBeCloseTo(32, 0)
    expect(hero!.y + hero!.height - button!.y - button!.height).toBeCloseTo(
      32,
      0,
    )
    for (const text of [
      heroElement.locator('h1'),
      heroElement.locator('p:visible'),
    ]) {
      const box = await text.boundingBox()
      expect(box!.x + box!.width).toBeLessThanOrEqual(followBox?.x ?? button!.x)
    }
  } else {
    await expect(stripElement).toBeVisible()
    await expect(stripElement.locator('dd')).toContainText(
      expectedAddress || theater.city,
    )
    const strip = await stripElement.boundingBox()
    const address = await stripElement.locator('dd').boundingBox()
    expect(strip!.width).toBeCloseTo(hero!.width, 0)
    expect(strip!.y).toBeCloseTo(hero!.y + hero!.height, 0)
    expect(address!.x + address!.width).toBeLessThanOrEqual(
      followBox?.x ?? button!.x,
    )
    expect(strip!.x + strip!.width - button!.x - button!.width).toBeCloseTo(
      16,
      0,
    )
  }
}

test('cinema boxes default and explicit lines survive date, grouping, tabs and history', async ({
  page,
}, testInfo) => {
  await openPage(page, `${path}&other=keep`)
  const layout = page.getByRole('button', { name: 'Vue Boîtes', exact: true })
  await expect(layout).toBeVisible()
  const sessions = page.getByRole('list', {
    name: `Séances de ${movie.title}`,
    exact: true,
  })
  await expect(sessions).toHaveCSS('display', 'grid')
  await layout.click()
  await expect(
    page.getByRole('menuitemradio', { name: 'Boîtes', exact: true }),
  ).toHaveAttribute('aria-checked', 'true')
  await page.getByRole('menuitemradio', { name: 'Lignes', exact: true }).click()
  await expect(page).toHaveURL(/layout=lines/)
  await expect(
    page.getByRole('button', { name: 'Vue Lignes', exact: true }),
  ).toBeVisible()
  await expect(sessions).not.toHaveCSS('display', 'grid')
  const preserved = () => {
    const query = new URL(page.url()).searchParams
    expect(query.get('layout')).toBe('lines')
    expect(query.get('other')).toBe('keep')
  }
  await page
    .getByRole('button', { name: 'Groupement Par film', exact: true })
    .click()
  await page
    .getByRole('menuitemradio', { name: 'Chronologique', exact: true })
    .click()
  await expect(page).toHaveURL(/grouping=chronological/)
  preserved()
  const nextDate = new Date(`${date}T12:00:00Z`)
  nextDate.setUTCDate(nextDate.getUTCDate() + 1)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('tab', { name: 'Demain', exact: true }).click()
  await expect(page).toHaveURL(
    new RegExp(`date=${nextDate.toISOString().slice(0, 10)}`),
  )
  preserved()
  await page.getByRole('tab', { name: 'Aujourd’hui', exact: true }).click()
  await page.setViewportSize(testInfo.project.use.viewport!)
  await expect(
    page.getByRole('button', { name: 'Vue Lignes', exact: true }),
  ).toBeVisible()
  preserved()
  const navigation = page.getByRole('navigation', {
    name: 'Vue de la programmation',
  })
  for (const tab of ['Films', 'Activité', 'Séances']) {
    await navigation.getByRole('link', { name: tab, exact: true }).click()
    await expect(
      navigation.getByRole('link', { name: tab, exact: true }),
    ).toHaveAttribute('aria-current', 'page')
    preserved()
  }
  await page.getByRole('button', { name: 'Vue Lignes', exact: true }).click()
  await page.getByRole('menuitemradio', { name: 'Boîtes', exact: true }).click()
  await expect(page).not.toHaveURL(/layout=/)
  await expect(layout).toBeVisible()
  await expect(
    page.getByRole('list', {
      name: 'Séances par ordre chronologique',
      exact: true,
    }),
  ).toHaveCSS('display', 'grid')
  expect(new URL(page.url()).searchParams.get('other')).toBe('keep')
  expect(new URL(page.url()).searchParams.get('grouping')).toBe('chronological')
  await page.goBack()
  await expect(
    page.getByRole('button', { name: 'Vue Lignes', exact: true }),
  ).toBeVisible()
  preserved()
  await page.goForward()
  await expect(layout).toBeVisible()
  await expect(page).not.toHaveURL(/layout=/)
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
  const header = page.locator('main > header')
  const hero = header.locator(':scope > div').first()
  expect((await hero.boundingBox())!.height).toBeLessThanOrEqual(
    testInfo.project.name === 'mobile' ? 220 : 320,
  )
  await expectHeaderLayout(header, testInfo.project.name === 'desktop')
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

for (const imageState of ['photo', 'missing', 'failed']) {
  test(`cinema ${imageState} hero preserves long identity`, async ({
    page,
    request,
  }, testInfo) => {
    await request.post('/__playwright/scenario', {
      data: { enabled: true, state: 'complete' },
    })
    await openPage(page, path)
    const imageLoads = imageState === 'photo'
    const longName =
      'Cinéma des Grandes Salles et des Rencontres Internationales'
    const longAddress = `12 rue des Rencontres ${'QuartierInternational'.repeat(5)}`
    const imageUrl = '/api/v1/theaters/ugc/fixture-cinema/image/1'
    await page.route(`**${imageUrl}`, (route) =>
      imageLoads
        ? route.fulfill({
            contentType: 'image/svg+xml',
            body: '<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="800"><rect width="1200" height="800" fill="#54636e"/></svg>',
          })
        : route.abort(),
    )
    await page.route(
      `**/api/v1/theaters/${theater.slug}/showtimes*`,
      (route) => {
        const payload = showtimes()
        return route.fulfill({
          json: {
            ...payload,
            theater: {
              ...payload.theater,
              name: longName,
              address: longAddress,
              image:
                imageState === 'missing'
                  ? null
                  : { url: imageUrl, width: 1200, height: 800 },
            },
          },
        })
      },
    )
    // Tomorrow triggers a client fetch; browser interception cannot mock SSR.
    await page.setViewportSize({ width: 390, height: 844 })
    await page.getByRole('tab', { name: 'Demain', exact: true }).click()
    await page.setViewportSize(testInfo.project.use.viewport!)
    const header = page.locator('main > header')
    await expect(header.getByRole('heading', { level: 1 })).toContainText(
      longName,
    )
    const photo = header.locator('img[fetchpriority="high"]')
    if (imageLoads) {
      await expect(photo).toBeVisible()
      await expect(photo).toHaveJSProperty('complete', true)
      expect(
        await photo.evaluate((image) =>
          image instanceof HTMLImageElement ? image.naturalWidth : 0,
        ),
      ).toBeGreaterThan(0)
    } else {
      await expect(photo).toHaveCount(0)
    }
    await expectHeaderLayout(
      header,
      testInfo.project.name === 'desktop',
      longAddress,
    )
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBeTruthy()
    await page.screenshot({
      path: testInfo.outputPath(`cinema-${imageState}.png`),
      fullPage: true,
    })
  })
}

test('cinema without an address keeps city identity', async ({
  page,
  request,
}, testInfo) => {
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'complete' },
  })
  await openPage(page, path)
  await page.route(`**/api/v1/theaters/${theater.slug}/showtimes*`, (route) => {
    const payload = showtimes()
    return route.fulfill({
      json: { ...payload, theater: { ...payload.theater, address: '' } },
    })
  })
  await page.setViewportSize({ width: 390, height: 844 })
  const updated = page.waitForResponse(
    `**/api/v1/theaters/${theater.slug}/showtimes*`,
  )
  await page.getByRole('tab', { name: 'Demain', exact: true }).click()
  await updated
  await expect(page.locator('main > header dd')).not.toContainText(
    theater.address,
  )
  await page.setViewportSize(testInfo.project.use.viewport!)
  await expectHeaderLayout(
    page.locator('main > header'),
    testInfo.project.name === 'desktop',
    '',
  )
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

test('ICE showtime logo loads without duplicate labels or overflow', async ({
  page,
}, info) => {
  await openPage(page, path)
  await page.route(`**/api/v1/theaters/${theater.slug}/showtimes*`, (route) => {
    const payload = showtimes()
    return route.fulfill({
      json: {
        ...payload,
        date: new URL(route.request().url()).searchParams.get('date'),
        showtimes: payload.showtimes.map((showtime) => ({
          ...showtime,
          format: 'ICE',
        })),
      },
    })
  })
  // Trigger a client fetch, reusing the synthetic SSR fixture for page identity.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('tab', { name: 'Demain', exact: true }).click()
  await page.setViewportSize(info.project.use.viewport!)
  const logo = page.locator('main img[src*="ice_logo_small"]:visible')
  await expect(logo).toHaveCount(1)
  await expect(logo).toHaveAttribute('alt', '')
  await expect(logo).toHaveAttribute('aria-hidden', 'true')
  await expect(logo.locator('..').locator('.sr-only')).toHaveText('ICE')
  await expect(logo).toHaveJSProperty('complete', true)
  expect(
    await logo.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  const box = await logo.boundingBox()
  expect(box!.height).toBeGreaterThanOrEqual(18)
  expect(box!.width / box!.height).toBeCloseTo(
    await logo.evaluate(
      (image: HTMLImageElement) => image.naturalWidth / image.naturalHeight,
    ),
    2,
  )
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
  await page.screenshot({
    path: info.outputPath('ice-showtime.png'),
    fullPage: true,
  })
})

test('ICE planning filter preserves selected inversion and canonical value', async ({
  page,
}, info) => {
  await page.route('**/api/v1/timeline?**', (route) =>
    route.fulfill({
      json: {
        date,
        timezone: 'Europe/Paris',
        window_start_time: `${date}T06:00:00+02:00`,
        window_end_time: `${date}T23:59:00+02:00`,
        theaters: [],
      },
    }),
  )
  await openPage(page, '/planning')
  const button = page.getByRole('button', { name: 'ICE', exact: true })
  const logo = button.locator('img')
  await expect(button).toHaveCount(1)
  await expect(button).toHaveAttribute('aria-pressed', 'false')
  await expect(logo).toHaveAttribute('src', /ice_logo_small/)
  await expect(logo).toHaveAttribute('alt', '')
  await expect(logo).toHaveAttribute('aria-hidden', 'true')
  await expect(logo).toHaveJSProperty('complete', true)
  expect(
    await logo.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  await expect(logo).not.toHaveClass(/invert/)
  await button.scrollIntoViewIfNeeded()
  await button.screenshot({ path: info.outputPath('ice-filter-default.png') })
  await button.click()
  await expect(button).toHaveAttribute('aria-pressed', 'true')
  await expect(page).toHaveURL(/format=ICE/)
  await expect(logo).toHaveClass(/brightness-0 invert/)
  expect(await logo.evaluate((image) => getComputedStyle(image).filter)).toBe(
    'brightness(0) invert(1)',
  )
  const box = await logo.boundingBox()
  expect(box!.height).toBeCloseTo(24, 0)
  expect(box!.width / box!.height).toBeCloseTo(
    await logo.evaluate(
      (image: HTMLImageElement) => image.naturalWidth / image.naturalHeight,
    ),
    2,
  )
  await button.screenshot({ path: info.outputPath('ice-filter-selected.png') })
  await page
    .getByRole('button', { name: 'Tous les formats', exact: true })
    .click()
  await expect(button).toHaveAttribute('aria-pressed', 'false')
  await expect(logo).not.toHaveClass(/invert/)
  await expect(page).not.toHaveURL(/format=ICE/)
})

test('ICE technology credit uses supplied display logo and official attribution', async ({
  page,
}, info) => {
  await openPage(page, '/credits')
  const credit = page.locator('section[aria-labelledby="credit-ICE"]')
  const link = credit.getByRole('link', {
    name: 'Site officiel ICE, ouverture dans un nouvel onglet',
    exact: true,
  })
  await expect(link).toHaveAttribute('href', 'https://www.icetheaters.com/')
  await expect(link).toHaveAttribute('target', '_blank')
  await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  const logo = link.locator('img')
  await expect(logo).toHaveAttribute('src', /ice_logo_large/)
  await expect(logo).toHaveAttribute('alt', '')
  await expect(logo).toHaveAttribute('aria-hidden', 'true')
  await expect(logo).toHaveJSProperty('complete', true)
  expect(
    await logo.evaluate((image: HTMLImageElement) => image.naturalWidth),
  ).toBeGreaterThan(0)
  const box = await logo.boundingBox()
  expect(box!.height).toBeCloseTo(64, 0)
  expect(box!.width / box!.height).toBeCloseTo(
    await logo.evaluate(
      (image: HTMLImageElement) => image.naturalWidth / image.naturalHeight,
    ),
    2,
  )
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true)
  await credit.screenshot({ path: info.outputPath('ice-credit.png') })
})

for (const venue of veoTheaters) {
  test(`Véo ${venue.id} SSR and hydrated reservations preserve missing metadata and safe links`, async ({
    page,
    request,
  }, info) => {
    await request.post('/__playwright/scenario', { data: { veo: true } })
    const url = `/cinema/${venue.slug}?date=${date}`
    const response = await request.get(url)
    expect(response.status()).toBe(200)
    const html = await response.text()
    expect(html).toContain(venue.name)
    expect(html).toContain('veocinema_logo')
    expect(html).toContain('Événement Véo')
    expect(html).toContain('VOSTFR')
    expect(html).not.toContain('href="https://achat.veocinemas.fr.evil.test')
    expect(html).toContain(
      venue.id === 'veo-EMS1361'
        ? 'href="https://castelginest.miracinemas.fr/#showsession?id=emsx136100000001"'
        : 'href="https://achat.veocinemas.fr/muret-veo/r/19"',
    )
    await openPage(page, url)
    const logo = page.locator('main > header img[src*="veocinema_logo"]')
    await expect(logo).toHaveCount(1)
    await expect(logo).toHaveAttribute('alt', '')
    await expect(logo).toHaveAttribute('aria-hidden', 'true')
    await expect(logo).toHaveJSProperty('complete', true)
    expect(
      await logo.evaluate((image: HTMLImageElement) => image.naturalWidth),
    ).toBeGreaterThan(0)
    // Hero constrains its image box in both axes; contain preserves artwork ratio.
    await expect(logo).toHaveCSS('object-fit', 'contain')
    expect(
      await logo.evaluate(
        (image: HTMLImageElement) => image.naturalWidth / image.naturalHeight,
      ),
    ).toBeCloseTo(2357 / 1485, 2)
    async function checkProgramme() {
      const booking = page.locator('main a[data-showtime-id]').first()
      await expect(booking).toHaveAttribute(
        'href',
        venue.id === 'veo-EMS1361'
          ? 'https://castelginest.miracinemas.fr/#showsession?id=emsx136100000001'
          : 'https://achat.veocinemas.fr/muret-veo/r/19',
      )
      await expect(booking).toHaveAttribute('target', '_blank')
      await expect(booking).toHaveAttribute('rel', 'noopener noreferrer')
      await expect(booking).toHaveAttribute(
        'aria-label',
        /ouverture dans un nouvel onglet/,
      )
      await expect(
        page.locator('main [aria-disabled="true"][data-showtime-id]'),
      ).toHaveCount(1)
      await expect(page.locator('main a[href*="evil.test"]')).toHaveCount(0)
      await expect(
        page.locator('main').getByText(/^Salle(?:\s|$)/),
      ).toHaveCount(0)
      await expect(page.locator('main').getByText(/^fin(?:\s|$)/i)).toHaveCount(
        0,
      )
      if (venue.id === 'veo-EMS1361') {
        const website = page.getByRole('link', {
          name: 'Site du cinéma Véo, ouverture dans un nouvel onglet',
          exact: true,
        })
        await expect(website).toHaveAttribute(
          'href',
          'https://castelginest.miracinemas.fr/',
        )
        await expect(website).toContainText('Site du cinéma Véo')
      }
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true)
    }
    await checkProgramme()
    await page.screenshot({
      path: info.outputPath(`${venue.id}.png`),
      fullPage: true,
    })
    // Exercise a real client fetch after SSR, without replacing server responses by routes.
    await page.setViewportSize({ width: 390, height: 844 })
    await page.getByRole('tab', { name: 'Demain', exact: true }).click()
    await page.setViewportSize(info.project.use.viewport!)
    await expect(page).not.toHaveURL(new RegExp(`date=${date}`))
    await checkProgramme()
  })
}

test('Enseignes contains all chains with scrolling, keyboard access and mobile settings flow', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', {
    data: { veo: true, allChains: true },
  })
  const mobile = info.project.name === 'mobile'
  for (const height of mobile ? [844] : [900, 480]) {
    await page.setViewportSize({ width: mobile ? 390 : 1440, height })
    await openPage(page, '/cinemas')
    const summary = page
      .locator('summary')
      .filter({ hasText: 'Toutes les enseignes' })
    if (mobile)
      await page.getByRole('button', { name: /Réglages|Paramètres/ }).click()
    else {
      if (height === 480) {
        await summary.evaluate((element) =>
          window.scrollTo(
            0,
            window.scrollY + element.getBoundingClientRect().top - 100,
          ),
        )
        await expect
          .poll(async () => (await summary.boundingBox())!.y)
          .toBeCloseTo(100, 0)
      }
      await summary.click()
    }
    const panel = page.getByRole('group', {
      name: 'Filtrer par enseigne',
      exact: true,
    })
    const options = panel.getByRole('checkbox')
    const last = options.last()
    const card = last.locator('..')
    await expect(options).toHaveCount(11)
    await expect(last).toHaveAccessibleName('Véo Cinémas')
    // Measure the real scroll container, not visibility (which allows overflow).
    const grid = panel.locator('.grid')
    await expect
      .poll(async () =>
        grid.evaluate((element) => {
          const bounds = element.getBoundingClientRect()
          const panelBounds = element
            .closest('fieldset')!
            .getBoundingClientRect()
          return (
            bounds.bottom <= panelBounds.bottom && bounds.top >= panelBounds.top
          )
        }),
      )
      .toBe(true)
    await expect(grid).toHaveCSS('grid-template-columns', /^\S+ \S+$/)
    if (!mobile) {
      const bounds = (await panel.boundingBox())!
      expect(bounds.y).toBeGreaterThanOrEqual(8)
      expect(bounds.y + bounds.height).toBeLessThanOrEqual(height - 8)
      expect(bounds.height).toBeLessThanOrEqual(440)
      const anchor = (await summary.boundingBox())!
      if (height === 480)
        expect(bounds.y).toBeGreaterThan(anchor.y + anchor.height)
      else expect(bounds.y + bounds.height).toBeLessThan(anchor.y)
      expect(
        await grid.evaluate((element) => element.scrollHeight),
      ).toBeGreaterThan(await grid.evaluate((element) => element.clientHeight))
      // Tab through every option: focusing the sr-only input must reveal its card.
      await options.first().focus()
      for (let index = 1; index < (await options.count()); index++)
        await page.keyboard.press('Tab')
      await expect(last).toBeFocused()
      expect(
        await grid.evaluate((element) => element.scrollTop),
      ).toBeGreaterThan(0)
    } else {
      await expect(panel).toHaveCSS('position', 'static')
      expect(await grid.evaluate((element) => element.scrollHeight)).toBe(
        await grid.evaluate((element) => element.clientHeight),
      )
      await card.scrollIntoViewIfNeeded()
    }
    const lastBounds = (await card.boundingBox())!
    const panelBounds = (await panel.boundingBox())!
    expect(lastBounds.y + lastBounds.height).toBeLessThanOrEqual(
      panelBounds.y + panelBounds.height,
    )
    if (!mobile) {
      const gridBounds = (await grid.boundingBox())!
      expect(lastBounds.y).toBeGreaterThanOrEqual(gridBounds.y)
      expect(lastBounds.y + lastBounds.height).toBeLessThanOrEqual(
        gridBounds.y + gridBounds.height,
      )
      await page.keyboard.press('Space')
    } else await card.click()
    await expect(last).toBeChecked()
    await expect(page).toHaveURL(/chains=veo/)
    await page.screenshot({ path: info.outputPath(`enseignes-${height}.png`) })
    await panel.getByRole('button', { name: 'Réinitialiser' }).click()
    await expect(last).not.toBeChecked()
    await expect(page).not.toHaveURL(/chains=/)
    if (!mobile) {
      await options.first().focus()
      await page.keyboard.press('Escape')
      await expect(panel).toBeHidden()
      await expect(summary).toBeFocused()
      await summary.click()
      await page.getByRole('searchbox').click()
      await expect(panel).toBeHidden()
      await summary.click()
      await page.getByRole('searchbox').focus()
      await expect(panel).toBeHidden()
    } else await page.getByRole('button', { name: /Fermer/ }).click()
  }
})

test('Véo directory filtering and operator credit retain brand identity on desktop and mobile', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', { data: { veo: true } })
  await openPage(page, '/cinemas')
  // The responsive directory exposes the same chain control inside mobile settings.
  if (info.project.name === 'mobile')
    await page.getByRole('button', { name: /Réglages|Paramètres/ }).click()
  else
    await page
      .locator('summary')
      .filter({ hasText: 'Toutes les enseignes' })
      .click()
  const chain = page.getByRole('checkbox', { name: 'Véo Cinémas', exact: true })
  await chain.locator('..').click()
  await expect(chain).toBeChecked()
  await expect(page).toHaveURL(/chains=veo/)
  if (info.project.name === 'mobile')
    await page.getByRole('button', { name: /Fermer/ }).click()
  else await page.keyboard.press('Escape')
  for (const venue of veoTheaters)
    await expect(
      page.getByRole('link', { name: new RegExp(venue.name) }).first(),
    ).toBeVisible()
  await expect(
    page.getByRole('link', { name: /Cinéma Playwright/ }),
  ).toHaveCount(0)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute(
    'data-playwright-ready',
    'true',
  )
  await expect(page).toHaveURL(/chains=veo/)
  await openPage(page, '/credits')
  const credit = page.getByRole('link', {
    name: 'Site officiel Véo Cinémas, ouverture dans un nouvel onglet',
    exact: true,
  })
  await expect(credit).toHaveAttribute('href', 'https://veocinemas.fr')
  await expect(credit.locator('img')).toHaveAttribute('alt', '')
  await expect(credit.locator('img')).toHaveJSProperty('complete', true)
})
