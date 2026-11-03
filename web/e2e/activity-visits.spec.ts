import { test, expect, openPage } from './fixtures'
import { theater, secondTheater } from './data.mjs'

const activityPath = `/cinema/${theater.slug}?view=activity`
const markerKey = `messeances:activity-visit:v1:theater:${theater.id}`
const complete = {
  enabled: true,
  state: 'complete',
  revision: '1',
  theater_ids: [theater.id, secondTheater.id],
}

test('guest activity distinguishes new and old entries, preserves labels while paging, and remembers a return visit', async ({
  page,
}, testInfo) => {
  await openPage(page, `/cinema/${theater.slug}`)
  await page.evaluate((key) => localStorage.setItem(key, '202'), markerKey)
  await page
    .getByRole('navigation', { name: 'Vue de la programmation' })
    .getByRole('link', { name: 'Activité', exact: true })
    .click()
  const rows = page.locator('[data-event-id]')
  await expect(rows.first().getByText('Nouveau', { exact: true })).toBeVisible()
  await expect(page.getByText('Ancien', { exact: true })).toHaveCount(0)
  await expect(rows.nth(1).getByRole('separator')).toBeVisible()
  await expect(rows.nth(1).getByRole('separator')).toHaveText(
    'Avant votre dernière visite',
  )
  await expect(
    page.getByRole('separator', {
      name: 'Activité antérieure à votre dernière visite',
    }),
  ).toHaveCount(1)
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  await expect(rows).toHaveCount(3)
  await expect(rows.last().getByText('Nouveau', { exact: true })).toHaveCount(0)
  await expect(rows.last().getByRole('separator')).toHaveCount(0)
  await expect(rows.first().getByText('Nouveau', { exact: true })).toBeVisible()
  const sessionRefresh = page.waitForResponse('**/api/v1/auth/session')
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await sessionRefresh
  await expect(rows.first().getByText('Nouveau', { exact: true })).toBeVisible()
  await expect
    .poll(() => page.evaluate((key) => localStorage.getItem(key), markerKey))
    .toBe('203')
  await page.evaluate(() => window.scrollTo(0, 0))
  await page.screenshot({
    path: testInfo.outputPath('activity-new-old.png'),
    fullPage: true,
  })
  await page.reload()
  await expect(page.locator('[data-activity-status="old"]')).toHaveCount(3)
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(0)
  await expect(rows.first().getByRole('separator')).toBeVisible()
  // A different cinema has its own history, even when it exposes the same IDs.
  await openPage(page, `/cinema/${secondTheater.slug}?view=activity`)
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(2)
  await expect(
    page.getByRole('separator', {
      name: 'Activité antérieure à votre dernière visite',
    }),
  ).toHaveCount(0)
})

test('failed local storage leaves the timeline usable', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(Storage.prototype, 'setItem', {
      configurable: true,
      value() {
        throw new Error('Storage disabled')
      },
    })
  })
  await openPage(page, activityPath)
  await expect(page.locator('[data-event-id]')).toHaveCount(2)
  await expect(
    page.getByText('Impossible de mémoriser votre visite.'),
  ).toBeVisible()
  await expect(page.locator('[data-activity-status]')).toHaveCount(0)
  await expect(page.locator('[data-activity-older-separator]')).toHaveCount(0)
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  await expect(page.locator('[data-event-id]')).toHaveCount(3)
})

test('account activity visits survive a fresh browser context and remain independent of cinema and guest histories', async ({
  page,
  request,
  browser,
  baseURL,
}, testInfo) => {
  await request.post('/__playwright/scenario', { data: complete })
  await openPage(page, '/compte/activite')
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(2)
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(3)
  const device = await browser.newContext({
    baseURL,
    serviceWorkers: 'block',
    viewport: testInfo.project.use.viewport,
  })
  try {
    const other = await device.newPage()
    const errors: string[] = []
    other.on('pageerror', (error) => errors.push(error.message))
    await openPage(other, '/compte/activite')
    await expect(other.locator('[data-activity-status="old"]')).toHaveCount(2)
    await expect(other.locator('[data-activity-status="new"]')).toHaveCount(0)
    await expect(
      other.locator('[data-event-id]').first().getByRole('separator'),
    ).toBeVisible()
    await expect(other.getByText('Ancien', { exact: true })).toHaveCount(0)
    await openPage(other, activityPath)
    await expect(other.locator('[data-activity-status="new"]')).toHaveCount(2)
    await other.reload()
    await expect(other.locator('[data-activity-status="old"]')).toHaveCount(2)
    expect(
      await other.evaluate(() =>
        Object.keys(localStorage).filter((key) =>
          key.includes('activity-visit'),
        ),
      ),
    ).toEqual([])
    expect(errors).toEqual([])
  } finally {
    await device.close()
  }
})
