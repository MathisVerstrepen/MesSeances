import type { Page } from '@playwright/test'
import { test, expect, openPage } from './fixtures'
import { date, movie, theater, showtimes } from './data.mjs'

test.beforeEach(async ({ request }) => {
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'complete', selected_ids: [theater.id] },
  })
})

const nextDate = new Date(`${date}T12:00:00Z`)
nextDate.setUTCDate(nextDate.getUTCDate() + 1)
const tomorrow = nextDate.toISOString().slice(0, 10)

async function mockTimeline(page: Page) {
  // Fix wall time without pausing requestAnimationFrame or application timers.
  await page.clock.setFixedTime(new Date(`${date}T13:00:00Z`))
  await page.route('**/api/v1/timeline?**', async (route) => {
    const day = new URL(route.request().url()).searchParams.get('date') || date
    const start = new Date(`${day}T07:00:00Z`)
    await route.fulfill({
      json: {
        date: day,
        timezone: 'Europe/Paris',
        window_start_time: start.toISOString(),
        window_end_time: `${day}T22:00:00Z`,
        theaters: [
          theater,
          { ...theater, id: 'second', name: 'Deuxième cinéma' },
        ].map((venue) => ({
          ...venue,
          showtimes: Array.from({ length: 16 }, (_, index) => ({
            ...showtimes().showtimes[0],
            id: `${venue.id}-${index}`,
            movie: {
              ...movie,
              slug: `film-${index}`,
              title: `Film ${index + 1}`,
            },
            start_time: `${day}T09:00:00Z`,
            end_time: `${day}T19:00:00Z`,
            start_offset_minutes: 120,
            duration_minutes: 600,
            format: '2D',
          })),
        })),
      },
    })
  })
}

async function expectedScroll(page: Page, target: number) {
  return page
    .locator('.timeline-scroller')
    .evaluate(
      (element, left) =>
        Math.min(left, element.scrollWidth - element.clientWidth),
      target,
    )
}

test('today opens before now, the return button restores it, and clock ticks do not move a manual position', async ({
  page,
}) => {
  await mockTimeline(page)
  await openPage(page, '/planning')
  const scroller = page.getByRole('region', { name: 'Horaires des séances' })
  await expect(
    page
      .locator('#planning-date-title')
      .locator('../..')
      .getByRole('button', { name: 'Revenir à l’heure actuelle' }),
  ).toBeVisible()
  const target = await expectedScroll(page, (360 - 30) * 2)
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollLeft))
    .toBe(target)
  await scroller.evaluate((element) => {
    element.scrollLeft = 100
  })
  await page.clock.setFixedTime(new Date(`${date}T13:01:00Z`))
  await page.evaluate(() =>
    document.dispatchEvent(new Event('visibilitychange')),
  )
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollLeft))
    .toBe(100)
  await page.getByRole('button', { name: 'Revenir à l’heure actuelle' }).click()
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollLeft))
    .toBe(await expectedScroll(page, (361 - 30) * 2))
})

test('another date opens near its earliest session and has no now action', async ({
  page,
}) => {
  await mockTimeline(page)
  await openPage(page, `/planning?date=${tomorrow}`)
  const scroller = page.getByRole('region', { name: 'Horaires des séances' })
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollLeft))
    .toBe(await expectedScroll(page, 120 * 2 - 12))
  await expect(
    page.getByRole('button', { name: 'Revenir à l’heure actuelle' }),
  ).toHaveCount(0)
})

test('mobile cinema headings and the ruler stay visible across both scroll axes and switch cinemas', async ({
  page,
}, info) => {
  test.skip(
    info.project.name !== 'mobile',
    'Mobile row headings replace the desktop side column',
  )
  await mockTimeline(page)
  await openPage(page, '/planning')
  const scroller = page.getByRole('region', { name: 'Horaires des séances' })
  const ruler = page.locator('.timeline-ruler')
  const headings = page.locator('.timeline-row-heading')
  await scroller.scrollIntoViewIfNeeded()
  const bounds = (await scroller.boundingBox())!
  expect(bounds.height).toBeLessThan(page.viewportSize()!.height)
  expect(
    await scroller.evaluate((element) =>
      getComputedStyle(element)
        .getPropertyValue('--timeline-label-width')
        .trim(),
    ),
  ).toBe('0px')
  await scroller.evaluate((element) => {
    element.scrollTop = 600
    element.scrollLeft = 450
  })
  await expect
    .poll(async () => Math.round((await ruler.boundingBox())!.y))
    .toBe(Math.round(bounds.y + 2))
  await expect
    .poll(async () => Math.round((await headings.first().boundingBox())!.y))
    .toBe(Math.round(bounds.y + 46))
  const heading = (await headings.first().boundingBox())!
  expect(Math.round(heading.x)).toBe(Math.round(bounds.x + 2))
  expect(Math.round(heading.width)).toBe(Math.round(bounds.width - 4))
  await expect(
    headings.first().getByRole('heading', { name: /Cinéma Playwright/ }),
  ).toBeInViewport()
  await page.screenshot({ path: info.outputPath('planning-sticky-cinema.png') })
  await scroller.evaluate((element) => {
    const row = element.querySelectorAll<HTMLElement>('.timeline-row')[1]!
    element.scrollTop = row.offsetTop - 44 + 100
  })
  await expect
    .poll(async () => Math.round((await headings.nth(1).boundingBox())!.y))
    .toBe(Math.round(bounds.y + 46))
  await expect(
    headings.nth(1).getByRole('heading', { name: /Deuxième cinéma/ }),
  ).toBeInViewport()
  await page.screenshot({ path: info.outputPath('planning-next-cinema.png') })
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true)
})

test('zoom preserves the visible time and a grid restored from an empty format starts near now', async ({
  page,
}) => {
  await mockTimeline(page)
  await openPage(page, '/planning')
  const scroller = page.getByRole('region', { name: 'Horaires des séances' })
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollLeft))
    .toBe(await expectedScroll(page, 660))
  await scroller.evaluate((element) => {
    element.scrollLeft = 120
  })
  const settings = page.getByRole('button', {
    name: 'Modifier les paramètres du planning',
  })
  const apply = page.getByRole('button', {
    name: 'Afficher le planning',
    exact: true,
  })
  await settings.click()
  await page.getByRole('button', { name: '15 min', exact: true }).click()
  await apply.click()
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollLeft))
    .toBe(240)
  await settings.click()
  await page.getByRole('button', { name: 'IMAX', exact: true }).click()
  await apply.click()
  await expect(scroller).toHaveCount(0)
  await settings.click()
  await page.getByRole('button', { name: '2D', exact: true }).click()
  await apply.click()
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollLeft))
    .toBe(await expectedScroll(page, (360 - 30) * 4))
})
