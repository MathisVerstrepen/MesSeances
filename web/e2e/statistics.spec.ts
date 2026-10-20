import type { Locator, Page } from '@playwright/test'
import { test, expect, openPage } from './fixtures'
import {
  movie,
  date,
  cityDetail,
  theater,
  secondTheater,
  statisticsFrom,
  statisticsWednesday,
} from './data.mjs'

const chartSection = (page: Page) =>
  page.locator('section[aria-labelledby="statistics-daily"]')
const plotOf = (page: Page) =>
  chartSection(page).getByRole('group', {
    name: 'Évolution du nombre de séances par jour',
  })
const route = (film = movie.slug) =>
  `/statistiques?period=all${film ? `&film=${film}` : ''}`

test.beforeEach(async ({ page }) => {
  // Same Paris calendar as SSR and the synthetic preset resolver.
  await page.clock.install({
    time: new Date(`${date}T12:00:00Z`),
  })
})

async function expectContainedLabels(section: Locator) {
  const labels = section.locator('[data-week-label]')
  const plot = section.getByRole('group', {
    name: 'Évolution du nombre de séances par jour',
  })
  const bounds = (await plot.boundingBox())!
  let right = bounds.x
  for (const label of await labels.all()) {
    const box = (await label.boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(right - 1)
    expect(box.x + box.width).toBeLessThanOrEqual(bounds.x + bounds.width + 1)
    right = box.x + box.width + 8
  }
}

test('film chart exposes exact hover and keyboard values, today and release-relative Wednesdays', async ({
  page,
}, info) => {
  await openPage(page, route())
  const section = chartSection(page)
  const plot = plotOf(page)
  await plot.scrollIntoViewIfNeeded()
  const today = plot.locator('[data-today-marker]')
  await expect(today).toHaveAttribute('data-date', date)
  const wednesdays = plot.locator('[data-week-marker]')
  await expect(wednesdays).toHaveCount(5)
  const todayIndex =
    (Date.parse(date) - Date.parse(statisticsFrom)) / 86_400_000
  expect(Number.parseFloat((await today.getAttribute('x1'))!)).toBeCloseTo(
    (todayIndex / 28) * 100,
  )
  await expect(wednesdays.nth(1)).toHaveAttribute(
    'data-date',
    statisticsWednesday,
  )
  await expect(section.locator('ul')).toContainText('Semaine 2')
  await expect(section.locator('ul')).toContainText('Semaine 6')
  await expect(section.locator('ul')).not.toContainText('Semaine 1 :')
  const box = (await plot.boundingBox())!
  // Full plotting area is the hit target, not a tiny native SVG title.
  await page.mouse.move(box.x + (box.width * 2) / 28, box.y + 128)
  const tooltip = section.getByRole('tooltip')
  await expect(tooltip).toContainText('111 séances')
  await expect(tooltip.locator('time')).toHaveAttribute(
    'datetime',
    new Date(Date.parse(`${statisticsFrom}T00:00:00Z`) + 2 * 86_400_000)
      .toISOString()
      .slice(0, 10),
  )
  if (info.project.name === 'mobile') {
    await plot.tap({ position: { x: (box.width * 2) / 28, y: 128 } })
    await expect(tooltip).toContainText('111 séances')
  }
  await plot.focus()
  await plot.press('Home')
  await expect(tooltip).toContainText('37 séances')
  expect(
    await plot.evaluate((element) => getComputedStyle(element).outlineStyle),
  ).toBe('solid')
  await plot.press('ArrowRight')
  await expect(tooltip).toContainText('0 séances')
  await plot.press('End')
  await expect(tooltip).toContainText('1 073 séances')
  await expectContainedLabels(section)
  const tooltipBox = (await tooltip.boundingBox())!
  expect(tooltipBox.x).toBeGreaterThanOrEqual(box.x - 1)
  expect(tooltipBox.x + tooltipBox.width).toBeLessThanOrEqual(
    box.x + box.width + 1,
  )
  await section.screenshot({
    path: info.outputPath('statistics-film-tooltip.png'),
  })
  await plot.press('Escape')
  await expect(tooltip).toHaveCount(0)
  await plot.press('Home')
  await expect(tooltip).toContainText('37 séances')
  await plot.press('Tab')
  await expect(tooltip).toHaveCount(0)
  await expect(section.getByText('Voir les données par jour')).toBeFocused()
  await section.getByText('Voir les données par jour').click()
  await expect(section.getByRole('row')).toHaveCount(30)
  await expect(
    section.getByRole('cell', { name: '0', exact: true }),
  ).toHaveCount(1)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)
})

test('general chart keeps today but no film boundaries after removing applied film', async ({
  page,
}, info) => {
  await openPage(page, route())
  await page.getByRole('button', { name: 'Retirer le film' }).click()
  await expect(plotOf(page).locator('[data-week-marker]')).toHaveCount(5)
  await page.getByRole('button', { name: 'Appliquer', exact: true }).click()
  const plot = plotOf(page)
  await expect(plot.locator('[data-week-marker]')).toHaveCount(0)
  await expect(plot.locator('[data-today-marker]')).toHaveCount(1)
  await expect(
    page.getByRole('heading', {
      name: 'Films les plus programmés',
      exact: true,
    }),
  ).toBeVisible()
  await plot.scrollIntoViewIfNeeded()
  await chartSection(page).screenshot({
    path: info.outputPath('statistics-general.png'),
  })
})

test('missing French release keeps boundaries without using international release', async ({
  page,
  request,
}) => {
  const response = await request.get('/film/film-no-release')
  expect(response.status()).toBe(200)
  expect(await response.text()).toContain('Film sans sortie française')
  await openPage(page, route('film-no-release'))
  await expect(plotOf(page).locator('[data-week-marker]')).toHaveCount(5)
  await expect(chartSection(page).locator('ul')).toContainText('Mercredi')
  await expect(chartSection(page).locator('ul')).not.toContainText('Semaine')
})

test('dense history labels do not collide at mobile or desktop widths', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', {
    data: { statisticsCollection: 'dense' },
  })
  await openPage(page, route())
  const plot = plotOf(page)
  const section = chartSection(page)
  await plot.scrollIntoViewIfNeeded()
  await expect(plot.locator('[data-week-marker]')).toHaveCount(105)
  await expectContainedLabels(section)
  expect(await section.locator('[data-week-label]').count()).toBeLessThan(20)
  await plot.focus()
  await plot.press('End')
  await expect(section.getByRole('tooltip')).toContainText('27 010 séances')
  await section.screenshot({ path: info.outputPath('statistics-dense.png') })
  await page.setViewportSize({ width: 320, height: 844 })
  await expectContainedLabels(section)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)
  await section.screenshot({ path: info.outputPath('statistics-narrow.png') })
})

test('single collected day centers today; Wednesday-only collection excludes off-range today', async ({
  page,
  request,
}, info) => {
  await request.post('/__playwright/scenario', {
    data: { statisticsCollection: 'today' },
  })
  await openPage(page, route())
  const section = chartSection(page)
  const plot = plotOf(page)
  await expect(plot.locator('[data-today-marker]')).toHaveAttribute('x1', '50%')
  await expect(plot.locator('[data-week-marker]')).toHaveCount(
    date === statisticsWednesday ? 1 : 0,
  )
  await plot.focus()
  await expect(section.getByRole('tooltip')).toContainText('37 séances')
  await section.screenshot({ path: info.outputPath('statistics-single.png') })
  await request.post('/__playwright/scenario', {
    data: { statisticsCollection: 'singleWednesday' },
  })
  await page.reload()
  await expect(plotOf(page).locator('[data-week-marker]')).toHaveAttribute(
    'x1',
    '50%',
  )
  await request.post('/__playwright/scenario', {
    data: { statisticsCollection: 'pastWednesday' },
  })
  await page.reload()
  await expect(plotOf(page).locator('[data-today-marker]')).toHaveCount(0)
  await expect(plotOf(page).locator('[data-week-marker]')).toHaveCount(1)
  await expect(chartSection(page).locator('ul')).toContainText('Mercredi')
  await expect(chartSection(page).locator('ul')).not.toContainText('Semaine')
})

test('four presets send explicit periods without dates, preserve filters and restore reset/navigation state', async ({
  page,
  request,
}) => {
  await openPage(page, '/statistiques?date=obsolete&date_to=obsolete')
  const period = page.getByRole('combobox', { name: 'Période', exact: true })
  const form = page.getByRole('form', { name: 'Filtres des statistiques' })
  await expect(period.locator('option')).toHaveText([
    '7 prochains jours',
    '30 prochains jours',
    'Semaine courante',
    'Depuis le début de la collecte',
  ])
  await expect(form.locator('input[type="date"]')).toHaveCount(0)
  await expect(period).toHaveValue('next7')
  for (const choice of ['next7', 'next30', 'all', 'week']) {
    await period.selectOption(choice)
    await form.getByRole('button', { name: 'Appliquer', exact: true }).click()
    await expect(page).toHaveURL(
      choice === 'next7' ? '/statistiques' : `/statistiques?period=${choice}`,
    )
    await expect(page.locator('#statistics-catalog')).toHaveAttribute(
      'aria-busy',
      'false',
    )
    const url = new URL(page.url())
    expect(url.searchParams.has('date')).toBe(false)
    expect(url.searchParams.has('date_to')).toBe(false)
    expect(url.searchParams.get('period')).toBe(
      choice === 'next7' ? null : choice,
    )
    const state = await (await request.get('/__playwright/state')).json()
    const queries = state.publicQueries.filter(
      (item: { path: string }) => item.path === '/api/v1/statistics/history',
    )
    expect(queries.at(-1).query).toEqual({ period: choice })
  }
  await form.getByText('Filtres avancés', { exact: true }).click()
  await page
    .getByRole('combobox', { name: 'Circuit', exact: true })
    .selectOption('ugc')
  await form.getByRole('button', { name: 'Appliquer', exact: true }).click()
  await expect(page).toHaveURL('/statistiques?chain=ugc&period=week')
  await expect(page.locator('#statistics-catalog')).toHaveAttribute(
    'aria-busy',
    'false',
  )
  const state = await (await request.get('/__playwright/state')).json()
  expect(
    state.publicQueries
      .filter(
        (item: { path: string }) => item.path === '/api/v1/statistics/history',
      )
      .at(-1).query,
  ).toEqual({ chain: 'ugc', period: 'week' })
  await form.getByRole('button', { name: 'Réinitialiser', exact: true }).click()
  await expect(page).toHaveURL('/statistiques')
  await expect(period).toHaveValue('next7')
  await form.getByText('Filtres avancés', { exact: true }).click()
  await expect(
    page.getByRole('combobox', { name: 'Circuit', exact: true }),
  ).toHaveValue('')
  await page.goBack()
  await expect(period).toHaveValue('week')
  await expect(
    page.getByRole('combobox', { name: 'Circuit', exact: true }),
  ).toHaveValue('ugc')
  await page.goForward()
  await expect(period).toHaveValue('next7')
  await expect(page.locator('#statistics-catalog')).toHaveAttribute(
    'aria-busy',
    'false',
  )
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)
})

test('removed, invalid and repeated periods never request statistics', async ({
  page,
  request,
}) => {
  for (const query of [
    'period=custom&date=2020-01-01&date_to=2030-01-01',
    'period=bad',
    'period=',
    'period=week&period=all',
  ]) {
    await openPage(page, `/statistiques?${query}`)
    await expect(page.getByRole('alert')).toContainText('Période invalide')
    const state = await (await request.get('/__playwright/state')).json()
    expect(
      state.publicQueries.filter(
        (item: { path: string }) => item.path === '/api/v1/statistics/history',
      ),
    ).toEqual([])
  }
})

test('unavailable preset retries into valid empty data and reset recovers default results', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { statisticsState: 'unavailable' },
  })
  await openPage(page, '/statistiques?period=week')
  await expect(page.getByRole('alert')).toContainText(
    'L’historique est temporairement indisponible',
  )
  await request.post('/__playwright/scenario', {
    data: { statisticsState: 'empty' },
  })
  await page.getByRole('button', { name: 'Réessayer', exact: true }).click()
  await expect(
    page.getByText('Aucune séance enregistrée pour ces filtres.', {
      exact: true,
    }),
  ).toBeVisible()
  await expect(
    page.getByRole('combobox', { name: 'Période', exact: true }),
  ).toHaveValue('week')
  await expect(page.getByRole('alert')).toHaveCount(0)
  await request.post('/__playwright/scenario', { data: {} })
  await page
    .getByRole('form', { name: 'Filtres des statistiques' })
    .getByRole('button', { name: 'Réinitialiser', exact: true })
    .click()
  await expect(
    page.getByRole('combobox', { name: 'Période', exact: true }),
  ).toHaveValue('next7')
  await expect(plotOf(page)).toBeVisible()
})

test('circuit names align across logo widths and link to filtered cinemas with either column set', async ({
  page,
}, info) => {
  for (const film of [movie.slug, '']) {
    await openPage(page, route(film))
    const section = page.locator('section[aria-labelledby="statistics-chains"]')
    const region = section.getByRole('region')
    const table = section.getByRole('table')
    await region.scrollIntoViewIfNeeded()
    await expect(table.getByRole('columnheader')).toHaveText(
      film
        ? ['Circuit', 'Séances', 'Cinémas']
        : ['Circuit', 'Séances', 'Films', 'Cinémas'],
    )
    await expect(table.getByRole('row').nth(2).getByRole('cell')).toHaveText(
      film ? ['8', '1'] : ['8', '1', '1'],
    )
    const links = table.getByRole('link')
    await expect(links).toHaveCount(3)
    for (const [name, chain] of [
      ['UGC', 'ugc'],
      ['MK2', 'mk2'],
      ['Kinepolis', 'kinepolis'],
    ]) {
      await expect(
        table.getByRole('link', { name, exact: true }),
      ).toHaveAttribute('href', `/cinemas?chains=${chain}`)
    }
    await expect
      .poll(() =>
        table
          .locator('img')
          .evaluateAll((images) =>
            images.every(
              (image) =>
                image instanceof HTMLImageElement &&
                image.complete &&
                image.naturalWidth > 0,
            ),
          ),
      )
      .toBe(true)
    const positions = await links.evaluateAll((elements) =>
      elements.map((link) => {
        const name = link.firstElementChild!
        const image = name.querySelector('img')!
        const text = [...name.childNodes].find(
          (node) =>
            node.nodeType === Node.TEXT_NODE && node.textContent?.trim(),
        )!
        const range = document.createRange()
        range.selectNodeContents(text)
        const label = range.getBoundingClientRect()
        const logo = image.getBoundingClientRect()
        return { x: label.x, gap: label.x - logo.right, width: logo.width }
      }),
    )
    expect(
      Math.max(...positions.map(({ x }) => x)) -
        Math.min(...positions.map(({ x }) => x)),
    ).toBeLessThan(1)
    expect(
      Math.max(...positions.map(({ width }) => width)) -
        Math.min(...positions.map(({ width }) => width)),
    ).toBeGreaterThan(4)
    for (const { gap } of positions) expect(gap).toBeGreaterThanOrEqual(12)
    await region.focus()
    await region.press('Tab')
    const ugc = table.getByRole('link', { name: 'UGC', exact: true })
    await expect(ugc).toBeFocused()
    expect(
      await ugc.evaluate((element) => getComputedStyle(element).outlineStyle),
    ).toBe('solid')
    expect(
      await ugc.evaluate((element) => getComputedStyle(element).outlineWidth),
    ).toBe('3px')
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true)
    await section.screenshot({
      path: info.outputPath(
        `statistics-circuits-${film ? 'film' : 'general'}.png`,
      ),
    })
    if (film) {
      await ugc.press('Enter')
      await expect(page).toHaveURL(/\/cinemas\?chains=ugc$/)
      await expect(
        page.getByRole('link', { name: /^Voir les séances :/ }),
      ).toHaveCount(2)
    } else {
      await table.getByRole('link', { name: 'MK2', exact: true }).click()
      await expect(page).toHaveURL(/\/cinemas\?chains=mk2$/)
      await expect(
        page.getByText('Aucun cinéma ne correspond à votre recherche.', {
          exact: true,
        }),
      ).toBeVisible()
      await expect(
        page.getByRole('link', { name: /^Voir les séances :/ }),
      ).toHaveCount(0)
    }
  }
})

test('local names link to city and canonical cinema pages without changing row geometry or sorting', async ({
  page,
}, info) => {
  for (const film of [movie.slug, '']) {
    await openPage(page, route(film))
    const section = page.locator('section[aria-labelledby="statistics-local"]')
    const table = section.getByRole('table')
    for (const mode of ['Villes', 'Cinémas']) {
      await section.getByRole('radio', { name: mode, exact: true }).check()
      await expect(
        section.getByRole('radio', { name: mode, exact: true }),
      ).toBeChecked()
      await expect(table.getByRole('columnheader')).toHaveText(
        (mode === 'Villes'
          ? ['Ville', ...(film ? [] : ['Films']), 'Séances', 'Cinémas']
          : ['Cinéma', ...(film ? [] : ['Films']), 'Séances', 'Ville']
        ).map((name) => new RegExp(`^${name} [↕↓]$`)),
      )
      const names =
        mode === 'Villes'
          ? ['Lille', 'Roubaix']
          : [theater.name, secondTheater.name]
      const paths =
        mode === 'Villes'
          ? ['/ville/lille/cinemas', '/ville/roubaix/cinemas']
          : [`/cinema/${theater.slug}`, `/cinema/${secondTheater.slug}`]
      await expect(table.getByRole('rowheader')).toHaveText(names)
      await expect(table.getByRole('link')).toHaveCount(2)
      for (const [index, name] of names.entries()) {
        await expect(
          table.getByRole('link', { name, exact: true }),
        ).toHaveAttribute('href', paths[index]!)
      }
      await expect(table.getByRole('row').nth(1).getByRole('cell')).toHaveText(
        mode === 'Villes'
          ? [...(film ? [] : ['1']), '12', '1']
          : [...(film ? [] : ['1']), '12', 'Lille'],
      )
      // Same rendered table, same width and CSS, only anchors replaced with their text.
      // Detached duplicate lives offscreen for synchronous measurement, then is removed.
      const geometry = await table.evaluate((element) => {
        const baseline = element.cloneNode(true)
        if (!(baseline instanceof HTMLTableElement))
          throw new Error('Expected a table baseline')
        baseline.style.cssText = `position:fixed;left:-10000px;top:0;width:${element.getBoundingClientRect().width}px`
        for (const link of baseline.querySelectorAll('a'))
          link.replaceWith(document.createTextNode(link.textContent!))
        element.parentElement!.append(baseline)
        const measure = (target: Element) =>
          [...target.querySelectorAll('tbody tr')].map((row) => ({
            height: row.getBoundingClientRect().height,
            lineHeight: getComputedStyle(row).lineHeight,
            cells: [...row.children].map((cell) => {
              const style = getComputedStyle(cell)
              return {
                height: cell.getBoundingClientRect().height,
                lineHeight: style.lineHeight,
                padding: style.padding,
              }
            }),
          }))
        const result = { linked: measure(element), unlinked: measure(baseline) }
        baseline.remove()
        return result
      })
      expect(geometry.linked).toEqual(geometry.unlinked)
      expect(geometry.linked.map((row) => row.height)).toEqual([52.5, 53])
      for (const row of geometry.linked) {
        expect(row.lineHeight).toBe('20px')
        for (const cell of row.cells) {
          expect(cell.lineHeight).toBe('20px')
          expect(cell.padding).toBe('16px 12px')
        }
      }
      await info.attach(
        `local-row-geometry-${film ? 'film' : 'general'}-${mode}`,
        {
          body: JSON.stringify(geometry, null, 2),
          contentType: 'application/json',
        },
      )
      const link = table.getByRole('link', { name: names[0]!, exact: true })
      await section.getByRole('region').focus()
      for (const button of await table.getByRole('button').all()) {
        await page.keyboard.press('Tab')
        await expect(button).toBeFocused()
      }
      await page.keyboard.press('Tab')
      await expect(link).toBeFocused()
      expect(
        await link.evaluate((element) => {
          const style = getComputedStyle(element)
          return {
            display: style.display,
            lineHeight: style.lineHeight,
            padding: style.padding,
            outline: style.outlineStyle,
            outlineWidth: style.outlineWidth,
          }
        }),
      ).toEqual({
        display: 'inline',
        lineHeight: '20px',
        padding: '0px',
        outline: 'solid',
        outlineWidth: '3px',
      })
      const sortHeader = table.getByRole('columnheader').first()
      await sortHeader.getByRole('button').click()
      await expect(sortHeader).toHaveAttribute('aria-sort', 'ascending')
      await expect(table.getByRole('rowheader')).toHaveText(names)
      await sortHeader.getByRole('button').click()
      await expect(sortHeader).toHaveAttribute('aria-sort', 'descending')
      await expect(table.getByRole('rowheader')).toHaveText(
        [...names].reverse(),
      )
      await section.screenshot({
        path: info.outputPath(
          `statistics-local-${film ? 'film' : 'general'}-${mode}.png`,
        ),
      })
    }
    await table.getByRole('link', { name: theater.name, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/cinema/${theater.slug}$`))
    await expect(page.getByRole('heading', { level: 1 })).toContainText(
      theater.name,
    )
    await openPage(page, route(film))
    await page
      .locator('section[aria-labelledby="statistics-local"]')
      .getByRole('link', { name: 'Lille', exact: true })
      .click()
    await expect(page).toHaveURL(/\/ville\/lille\/cinemas$/)
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Lille')
    const cinemaListing = page
      .locator('section[aria-labelledby="city-cinemas-heading"]')
      .getByRole('link')
    const venues = cityDetail('lille').theaters
    await expect(cinemaListing).toHaveCount(venues.length)
    expect(
      await cinemaListing.evaluateAll((links) =>
        links.map((link) => link.getAttribute('href')),
      ),
    ).toEqual(
      venues.map((venue) => `/cinema/${encodeURIComponent(venue.slug)}`),
    )
    for (const [index, venue] of venues.entries())
      await expect(cinemaListing.nth(index)).toContainText(venue.name)
    const canonicalCinema = cinemaListing.filter({ hasText: theater.name })
    await expect(canonicalCinema).toHaveCount(1)
    await expect(canonicalCinema).toHaveAttribute(
      'href',
      `/cinema/${theater.slug}`,
    )
    await canonicalCinema.click()
    await expect(page).toHaveURL(new RegExp(`/cinema/${theater.slug}$`))
    await expect(page.getByRole('heading', { level: 1 })).toContainText(
      theater.name,
    )
  }
})
