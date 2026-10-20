import type { Page, TestInfo } from '@playwright/test'
import { test, expect, openPage } from './fixtures'
import { date, movie } from './data.mjs'

const cases = [
  {
    name: 'planning',
    path: '/planning?mode=movie&zoom=60&language=VF',
    signal: '#planning-catalog',
    endpoint: '/api/v1/timeline',
    ready: '.timeline-matrix',
    rows: 'button[aria-controls="timeline-showtime-inspector"]',
    count: 10,
  },
  {
    name: 'search',
    path: `/recherche?theaters=&date=${date}&start_after=12:00&finish_before=18:00&language=VF`,
    signal: '#search-catalog',
    endpoint: '/api/v1/search/slot',
    ready: 'section[aria-label="Résultats de recherche"]',
    rows: 'a[href^="/film/"]',
    count: 10,
  },
  {
    name: 'film',
    path: `/film/${movie.slug}?language=VF&sort=next`,
    signal: '#film-catalog',
    endpoint: `/api/v1/movies/${movie.slug}/showtimes`,
    ready: '#showtime-panel',
    rows: '.theater-section',
    count: 10,
  },
  {
    name: 'statistics',
    path: `/statistiques?period=all&film=${movie.slug}`,
    signal: '#statistics-catalog',
    endpoint: '/api/v1/statistics/history',
    ready: 'section[aria-labelledby="statistics-chains"]',
    rows: 'tbody tr',
    count: 3,
  },
]
const top = (page: Page) => page.evaluate(() => window.scrollY)
async function settled(page: Page, item: (typeof cases)[number]) {
  await expect(page.locator(item.ready).locator(item.rows)).toHaveCount(
    item.count,
  )
}
async function cinemas(page: Page) {
  await page
    .locator('header a[href="/cinemas"]')
    .evaluate((link: HTMLAnchorElement) => link.click())
  await expect(page).toHaveURL('/cinemas')
  await expect.poll(() => top(page)).toBe(0)
}
async function measure(
  page: Page,
  info: TestInfo,
  name: string,
  saved: number,
) {
  await expect.soft
    .poll(async () => Math.abs((await top(page)) - saved))
    .toBeLessThanOrEqual(5)
  const restored = await top(page)
  const result = {
    name,
    project: info.project.name,
    route: new URL(page.url()).pathname + new URL(page.url()).search,
    saved,
    restored,
    delta: Math.abs(saved - restored),
  }
  await info.attach(name, {
    body: JSON.stringify(result),
    contentType: 'application/json',
  })
  console.log(JSON.stringify(result))
  return result.delta
}

for (const item of cases) {
  test(`${item.name} delayed Back preserves scroll and route state`, async ({
    page,
    request,
  }, info) => {
    await request.post('/__playwright/scenario', {
      data: { scrollRoute: 'long' },
    })
    await openPage(page, item.path)
    await settled(page, item)
    // No click-induced scrolling before leaving: activate the real NuxtLink in place.
    await page.evaluate(() =>
      window.scrollTo(
        0,
        document.documentElement.scrollHeight - innerHeight - 50,
      ),
    )
    const saved = await top(page)
    expect(saved).toBeGreaterThan(500)
    const path = new URL(page.url()).pathname + new URL(page.url()).search
    await cinemas(page)
    await page.goBack()
    await expect(page).toHaveURL(path)
    await settled(page, item)
    expect.soft(await measure(page, info, 'Back', saved)).toBeLessThanOrEqual(5)
  })

  test(`${item.name} delayed Forward preserves scroll and route state`, async ({
    page,
    request,
  }, info) => {
    await request.post('/__playwright/scenario', {
      data: { scrollRoute: 'long' },
    })
    await openPage(page, '/cinemas')
    if (item.name === 'film') {
      await page.locator('header a[href="/films"]').click()
      await page
        .getByRole('list', { name: 'Films à l’affiche' })
        .getByRole('link')
        .first()
        .click()
    } else if (item.name === 'statistics') {
      await page.locator('footer a[href="/statistiques"]').click()
    } else {
      await page
        .locator(
          `header a[href="/${item.name === 'search' ? 'recherche' : 'planning'}"]`,
        )
        .click()
      if (item.name === 'search')
        await page
          .getByRole('button', { name: 'Trouver une séance', exact: true })
          .click()
    }
    await settled(page, item)
    await page.evaluate(() =>
      window.scrollTo(
        0,
        document.documentElement.scrollHeight - innerHeight - 50,
      ),
    )
    const saved = await top(page)
    expect(saved).toBeGreaterThan(500)
    const path = new URL(page.url()).pathname + new URL(page.url()).search
    await page.goBack()
    // Films is film detail's immediately preceding SPA entry; others return to cinemas.
    await expect(page).toHaveURL(item.name === 'film' ? '/films' : '/cinemas')
    if (item.name === 'film')
      await expect(page.locator('#films-catalog')).toHaveAttribute(
        'aria-busy',
        'false',
      )
    else await expect(page.locator('#cinemas-title')).toBeVisible()
    await page.goForward()
    await expect(page).toHaveURL(path)
    await settled(page, item)
    expect
      .soft(await measure(page, info, 'Forward', saved))
      .toBeLessThanOrEqual(5)
  })

  for (const mode of ['short', 'empty', 'error']) {
    test(`${item.name} ${mode} response terminates at reachable position`, async ({
      page,
      request,
    }) => {
      await request.post('/__playwright/scenario', {
        data: { scrollRoute: 'long' },
      })
      await openPage(page, item.path)
      await settled(page, item)
      await page.evaluate(() =>
        window.scrollTo(
          0,
          document.documentElement.scrollHeight - innerHeight - 50,
        ),
      )
      const saved = await top(page)
      await cinemas(page)
      await request.post('/__playwright/scenario', {
        data: { scrollRoute: mode },
      })
      await page.goBack()
      await expect(page.locator(item.signal)).toHaveAttribute(
        'aria-busy',
        'false',
      )
      const reachable = await page.evaluate(
        (saved) =>
          Math.min(
            saved,
            Math.max(0, document.documentElement.scrollHeight - innerHeight),
          ),
        saved,
      )
      await expect
        .poll(async () => Math.abs((await top(page)) - reachable))
        .toBeLessThanOrEqual(5)
      await cinemas(page)
    })
  }

  test(`${item.name} superseding navigation cancels pending restore`, async ({
    page,
    request,
  }) => {
    await request.post('/__playwright/scenario', {
      data: { scrollRoute: 'long' },
    })
    await openPage(page, item.path)
    await settled(page, item)
    await page.evaluate(() =>
      window.scrollTo(
        0,
        document.documentElement.scrollHeight - innerHeight - 50,
      ),
    )
    await cinemas(page)
    let release!: () => void
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route(`**${item.endpoint}?**`, async (route) => {
      await held
      await route.continue()
    })
    // Statistics cancels its fetch on unmount; other loaders finish off-screen.
    const aborted =
      item.name === 'statistics'
        ? page.waitForEvent('requestfailed', {
            predicate: (request) =>
              new URL(request.url()).pathname === item.endpoint,
          })
        : undefined
    await page.goBack()
    await expect(page.locator(item.signal)).toHaveAttribute('aria-busy', 'true')
    await cinemas(page)
    const response = aborted
      ? undefined
      : page.waitForResponse(
          (response) => new URL(response.url()).pathname === item.endpoint,
        )
    release()
    if (aborted) await aborted
    if (response) await (await response).finished()
    await page.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    )
    await expect.poll(() => top(page)).toBe(0)
    await expect(page).toHaveURL('/cinemas')
  })
}
