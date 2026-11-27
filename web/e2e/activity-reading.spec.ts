import { test, expect, openPage } from './fixtures'
import type { Page } from '@playwright/test'
import { theater, secondTheater } from './data.mjs'

const complete = {
  enabled: true,
  state: 'complete',
  revision: '1',
  theater_ids: [theater.id, secondTheater.id],
}

async function openSettings(page: Page) {
  await page
    .getByRole('button', { name: 'Paramètres de l’activité', exact: true })
    .click()
  const dialog = page.getByRole('dialog', { name: 'Paramètres de l’activité' })
  await expect(dialog).toBeVisible()
  return dialog
}

test('manual activity survives quick visits and poster badges toggle without opening the movie', async ({
  page,
  request,
}, testInfo) => {
  await request.post('/__playwright/scenario', {
    data: { ...complete, manualReading: true },
  })
  const visits: string[] = []
  page.on('request', (request) => {
    if (request.url().endsWith('/account/activity/visit'))
      visits.push(request.method())
  })
  await openPage(page, '/compte/activite')
  const mode = page.getByRole('checkbox', {
    name: 'Marquer manuellement les nouveautés comme consultées',
  })
  await expect(mode).toHaveCount(0)
  await openSettings(page)
  await expect(mode).toBeChecked()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(2)
  await page.reload()
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(2)
  const first = page.locator('[data-event-id="103"]')
  const badge = first.getByRole('button', { name: /comme consultée$/ })
  const box = await badge.boundingBox()
  expect(box!.height).toBeGreaterThanOrEqual(44)
  await badge.click()
  await expect(first).toHaveAttribute('data-activity-status', 'old')
  await expect(first.getByRole('button')).toHaveText('Consultée')
  await expect(first.getByRole('button')).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await expect(page).toHaveURL(/\/compte\/activite$/)
  const posterBox = await first
    .getByRole('link', { name: 'Film Playwright', exact: true })
    .first()
    .boundingBox()
  const labelBox = await first.getByRole('button').locator('span').boundingBox()
  expect(labelBox!.x + labelBox!.width).toBeLessThanOrEqual(
    posterBox!.x + posterBox!.width + 0.5,
  )
  await expect(page.locator('[data-event-id="102"]')).toHaveAttribute(
    'data-activity-status',
    'new',
  )
  await page.screenshot({
    path: testInfo.outputPath('activity-manual.png'),
    fullPage: true,
  })
  await page.reload()
  await expect(first).toHaveAttribute('data-activity-status', 'old')
  await first.getByRole('button').focus()
  await page.keyboard.press('Space')
  await expect(first).toHaveAttribute('data-activity-status', 'new')
  await expect(first.getByRole('button')).toHaveAttribute(
    'aria-pressed',
    'false',
  )
  await first
    .getByRole('link', { name: 'Film Playwright', exact: true })
    .first()
    .click()
  await expect(page).toHaveURL(/\/film\//)
  await page.goBack()
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(2)
  expect(visits).toEqual([])
})

test('mode persists and bulk consultation includes older unloaded activity', async ({
  page,
  request,
  isMobile,
}) => {
  await request.post('/__playwright/scenario', { data: complete })
  await openPage(page, '/compte/activite')
  const mode = page.getByRole('checkbox', {
    name: 'Marquer manuellement les nouveautés comme consultées',
  })
  await openSettings(page)
  await expect(mode).not.toBeChecked()
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(2)
  await mode.check()
  await expect(mode).toBeEnabled()
  await page.reload()
  const dialog = await openSettings(page)
  await expect(mode).toBeChecked()
  await page
    .getByRole('button', { name: 'Tout marquer comme consulté', exact: true })
    .click()
  await expect(dialog).not.toBeVisible()
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  await expect(page.locator('[data-activity-status="old"]')).toHaveCount(3)
  // Only the desktop account sidebar shows the activity dot on the feed page.
  const sidebarDot = page
    .getByRole('navigation', { name: 'Espace personnel' })
    .locator('[data-activity-notification]')
  await expect(sidebarDot).toHaveCount(0)
  await page.locator('[data-event-id="101"]').getByRole('button').click()
  await expect(page.locator('[data-event-id="101"]')).toHaveAttribute(
    'data-activity-status',
    'new',
  )
  if (!isMobile) await expect(sidebarDot).toBeVisible()
  await openSettings(page)
  await mode.uncheck()
  await expect(mode).toBeEnabled()
  await page.reload()
  await openSettings(page)
  await expect(mode).not.toBeChecked()
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(0)
})

test('focus updates an older loaded badge from another device while preserving the list', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { ...complete, manualReading: true },
  })
  await openPage(page, '/compte/activite')
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  const older = page.locator('[data-event-id="101"]')
  await expect(older).toHaveAttribute('data-activity-status', 'new')
  const saved = await request.post('/api/v1/account/activity/read', {
    headers: { 'X-Messeances-CSRF': '1' },
    data: {
      expected_username: 'fixture_alice',
      event_id: '101',
      consulted: 'true',
    },
  })
  expect(saved.ok()).toBeTruthy()
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(older).toHaveAttribute('data-activity-status', 'old')
  await expect(page.locator('[data-event-id]')).toHaveCount(3)
})

test('failed individual consultation keeps the badge and exposes a recoverable error', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { ...complete, manualReading: true },
  })
  await openPage(page, '/compte/activite')
  await page.route('**/api/v1/account/activity/read', (route) =>
    route.fulfill({
      status: 503,
      json: { error: { code: 'accounts_unavailable' } },
    }),
  )
  const first = page.locator('[data-event-id="103"]')
  await first.getByRole('button').click()
  await expect(page.getByRole('alert')).toContainText(
    'Impossible de mémoriser ce changement',
  )
  await expect(first).toHaveAttribute('data-activity-status', 'new')
  await expect(first.getByRole('button')).toBeEnabled()
  await page.unroute('**/api/v1/account/activity/read')
  await first.getByRole('button').click()
  await expect(first).toHaveAttribute('data-activity-status', 'old')
})

test('title settings modal keeps the list compact, contains keyboard focus and restores the opener', async ({
  page,
  request,
}, testInfo) => {
  await request.post('/__playwright/scenario', {
    data: { ...complete, manualReading: true },
  })
  await openPage(page, '/compte/activite')
  const trigger = page.getByRole('button', {
    name: 'Paramètres de l’activité',
    exact: true,
  })
  const titleBox = await page
    .getByRole('heading', { name: 'Activité', exact: true, level: 1 })
    .boundingBox()
  const triggerBox = await trigger.boundingBox()
  expect(
    Math.abs(
      triggerBox!.y +
        triggerBox!.height / 2 -
        titleBox!.y -
        titleBox!.height / 2,
    ),
  ).toBeLessThan(2)
  expect(triggerBox!.x).toBeGreaterThan(titleBox!.x + titleBox!.width)
  await expect(
    page.getByText('Marquer manuellement les nouveautés comme consultées', {
      exact: true,
    }),
  ).toHaveCount(0)
  await page.screenshot({
    path: testInfo.outputPath('activity-compact.png'),
    fullPage: true,
  })
  const overflow = await page.evaluate(() => document.body.style.overflow)
  const dialog = await openSettings(page)
  const close = dialog.getByRole('button', { name: 'Fermer les paramètres' })
  const bulk = dialog.getByRole('button', {
    name: 'Tout marquer comme consulté',
    exact: true,
  })
  await expect(close).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(bulk).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(close).toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('activity-settings.png') })
  await page.keyboard.press('Escape')
  await expect(dialog).not.toBeVisible()
  await expect(trigger).toBeFocused()
  expect(await page.evaluate(() => document.body.style.overflow)).toBe(overflow)
})

test('bulk consultation is available in automatic mode and clears the current visit badges', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', { data: complete })
  await openPage(page, '/compte/activite')
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(2)
  const dialog = await openSettings(page)
  await expect(dialog.getByRole('checkbox')).not.toBeChecked()
  await dialog
    .getByRole('button', { name: 'Tout marquer comme consulté', exact: true })
    .click()
  await expect(dialog).not.toBeVisible()
  await expect(page.locator('[data-activity-status="new"]')).toHaveCount(0)
  await expect(page.locator('[data-activity-status="old"]')).toHaveCount(2)
  await page.getByRole('button', { name: 'Afficher plus', exact: true }).click()
  await expect(page.locator('[data-activity-status="old"]')).toHaveCount(3)
})
