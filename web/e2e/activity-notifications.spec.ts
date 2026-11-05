import { test, expect, openPage } from './fixtures'
import { theater, secondTheater } from './data.mjs'

const complete = {
  enabled: true,
  state: 'complete',
  revision: '1',
  theater_ids: [theater.id, secondTheater.id],
}

test('header and activity menu dots appear for unseen activity and clear only after visiting the feed', async ({
  page,
  request,
  isMobile,
}, testInfo) => {
  await request.post('/__playwright/scenario', { data: complete })
  const ssr = await request.get('/compte')
  expect(await ssr.text()).not.toContain('data-activity-notification')
  expect(
    (await (await request.get('/__playwright/state')).json())
      .activityNotificationGets,
  ).toBe(0)
  await openPage(page, '/compte')
  const header = page
    .getByRole('navigation', { name: 'Navigation principale' })
    .getByRole('link', { name: 'Mon compte', exact: true })
  const activity = page
    .getByRole('navigation', { name: 'Rubriques du compte' })
    .getByRole('link', { name: 'Activité', exact: true })
  await expect(header.locator('[data-activity-notification]')).toBeVisible()
  await expect(header).toHaveAccessibleDescription(
    'Nouvelle activité disponible',
  )
  await expect(activity.locator('[data-activity-notification]')).toBeVisible()
  await expect(activity).toHaveAccessibleDescription(
    'Nouvelle activité disponible',
  )
  if (!isMobile) {
    const sidebar = page.getByRole('navigation', { name: 'Espace personnel' })
    await expect(
      sidebar
        .getByRole('link', { name: 'Activité', exact: true })
        .locator('[data-activity-notification]'),
    ).toBeVisible()
  }
  await page.screenshot({
    path: testInfo.outputPath('activity-notification.png'),
    fullPage: true,
  })
  await activity.click()
  await expect(page.locator('[data-event-id]')).toHaveCount(2)
  await expect(header.locator('[data-activity-notification]')).toHaveCount(0)
  await expect(header).not.toHaveAttribute('aria-description')
  await openPage(page, '/compte')
  await expect(activity.locator('[data-activity-notification]')).toHaveCount(0)
  await expect(page.locator('[data-activity-notification]')).toHaveCount(0)
})

test('focus reconciles a visit from another device and new activity arriving afterwards', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', { data: complete })
  await openPage(page, `/cinema/${theater.slug}`)
  const dot = page.locator('header [data-activity-notification]')
  await expect(dot).toBeVisible()
  // An independent client advances the account watermark, not this browser.
  const visit = await request.post('/api/v1/account/activity/visit', {
    data: {
      expected_username: 'fixture_alice',
      scope: 'followed',
      last_event_id: '103',
    },
  })
  expect(visit.ok()).toBe(true)
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(dot).toHaveCount(0)
  await page.route('**/api/v1/account/activity/notification', (route) =>
    route.fulfill({
      json: {
        username: 'fixture_alice',
        follows_revision: '1',
        has_new_activity: true,
      },
    }),
  )
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(dot).toBeVisible()
})

test('late notification responses cannot restore a dot after the account loses its session', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', { data: complete })
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  let readStarted!: () => void
  const reading = new Promise<void>((resolve) => {
    readStarted = resolve
  })
  await page.route('**/api/v1/account/activity/notification', async (route) => {
    readStarted()
    await held
    await route.fulfill({
      json: {
        username: 'fixture_alice',
        follows_revision: '1',
        has_new_activity: true,
      },
    })
  })
  await openPage(page, `/cinema/${theater.slug}`)
  await reading
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'anonymous' },
  })
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(
    page
      .getByRole('navigation', { name: 'Navigation principale' })
      .getByRole('link', { name: 'Connexion', exact: true }),
  ).toBeVisible()
  release()
  await expect(page.locator('[data-activity-notification]')).toHaveCount(0)
})

for (const state of ['empty', 'no-follows', 'pending']) {
  test(`activity notification stays absent for ${state}`, async ({
    page,
    request,
  }) => {
    const data = { ...complete, feed: 'populated' }
    if (state === 'empty') data.feed = 'empty'
    if (state === 'no-follows') data.theater_ids = []
    if (state === 'pending') data.state = 'pending_username'
    await request.post('/__playwright/scenario', { data })
    await openPage(page, `/cinema/${theater.slug}`)
    await expect(page.locator('[data-activity-notification]')).toHaveCount(0)
  })
}
