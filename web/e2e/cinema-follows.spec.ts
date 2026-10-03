import { test, expect, openPage } from './fixtures'
import { date, secondTheater, theater } from './data.mjs'

const path = `/cinema/${theater.slug}?date=${date}`

test('disabled accounts hide follow but keep statistics', async ({ page }) => {
  await openPage(page, path)
  await expect(
    page.getByRole('button', { name: /suivre ce cinéma/i }),
  ).toHaveCount(0)
  await expect(
    page
      .locator('main header')
      .getByRole('link', { name: 'Statistiques', exact: true }),
  ).toBeVisible()
})

for (const [state, destination] of [
  ['anonymous', '/connexion'],
  ['pending_email', '/verification'],
  ['pending_username', '/finaliser'],
]) {
  test(`follow ${state} uses existing admission destination without POST`, async ({
    page,
    request,
  }) => {
    await request.post('/__playwright/scenario', {
      data: { enabled: true, state },
    })
    await openPage(page, path)
    const follow = page.getByRole('button', {
      name: 'Suivre ce cinéma',
      exact: true,
    })
    await expect(follow).not.toHaveAttribute('aria-pressed')
    await follow.click()
    await expect(page).toHaveURL(new RegExp(`${destination}$`))
    const result = await (await request.get('/__playwright/state')).json()
    expect(result.followPosts).toBe(0)
    expect(result.followGets).toBe(0)
  })
}

test('committed icon follow persists reload, stays independent of selection, then unfollows', async ({
  page,
  request,
}, testInfo) => {
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'complete' },
  })
  const html = await (await request.get(path)).text()
  expect(html).not.toContain('theater_ids')
  expect(html).not.toContain('follows_revision')
  await openPage(page, path)
  const follow = page.getByRole('button', {
    name: 'Suivre ce cinéma',
    exact: true,
  })
  await expect(follow).toHaveAttribute('aria-pressed', 'false')
  expect(await follow.textContent()).toBe('')
  await follow.focus()
  await page.keyboard.press('Enter')
  const followed = page.getByRole('button', {
    name: 'Ne plus suivre ce cinéma',
    exact: true,
  })
  await expect(followed).toHaveAttribute('aria-pressed', 'true')
  await expect(followed).toBeFocused()
  await page.screenshot({
    path: testInfo.outputPath('followed-header.png'),
    fullPage: true,
  })
  const selected = await (
    await page.request.get('/api/v1/account/theaters')
  ).json()
  expect(selected.theater_ids).toEqual([secondTheater.id])
  const state = await (await request.get('/__playwright/state')).json()
  expect(state.followGets).toBe(1)
  expect(state.followPosts).toBe(1)
  await page.reload()
  await expect(followed).toHaveAttribute('aria-pressed', 'true')
  await page
    .getByRole('navigation', { name: 'Navigation principale' })
    .getByRole('link', { name: 'Mon compte', exact: true })
    .click()
  await page
    .getByRole('navigation', { name: 'Rubriques du compte' })
    .getByRole('link', { name: 'Activité', exact: true })
    .click()
  await expect(page.locator('[data-event-id]')).toHaveCount(2)
  await expect(page.locator('[data-event-id="102"]')).toHaveCount(0)
  await page.goBack()
  await page.goBack()
  await expect(followed).toHaveAttribute('aria-pressed', 'true')
  const storage = await page.evaluate(() => ({
    local: { ...localStorage },
    session: { ...sessionStorage },
  }))
  expect(JSON.stringify(storage)).not.toMatch(
    /follow|fixture-cinema|follows_revision/,
  )
  await followed.click()
  await expect(follow).toHaveAttribute('aria-pressed', 'false')
  expect(
    (await (await request.get('/__playwright/state')).json()).followPosts,
  ).toBe(2)
  await page
    .getByRole('navigation', { name: 'Navigation principale' })
    .getByRole('link', { name: 'Mon compte', exact: true })
    .click()
  await page
    .getByRole('navigation', { name: 'Rubriques du compte' })
    .getByRole('link', { name: 'Activité', exact: true })
    .click()
  await expect(
    page.getByRole('heading', { name: 'Aucun cinéma suivi', exact: true }),
  ).toBeVisible()
})

test('pending committed toggle disables both controls, focus restored only to initiating visible button', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'complete' },
  })
  await openPage(page, path)
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  let entered!: () => void
  const intercepted = new Promise<void>((resolve) => {
    entered = resolve
  })
  await page.route('**/api/v1/account/theater-follows', async (route) => {
    if (route.request().method() !== 'POST') return route.continue()
    entered()
    await pending
    await route.continue()
  })
  const follow = page.getByRole('button', {
    name: 'Suivre ce cinéma',
    exact: true,
  })
  await follow.focus()
  await page.keyboard.press('Enter')
  await intercepted
  await expect(page.locator('button[title="Suivre ce cinéma"]')).toHaveCount(2)
  for (const button of await page
    .locator('button[title="Suivre ce cinéma"]')
    .all())
    await expect(button).toBeDisabled()
  await expect(follow).toHaveAttribute('aria-pressed', 'false')
  release()
  await expect(
    page.getByRole('button', { name: 'Ne plus suivre ce cinéma' }),
  ).toBeFocused()
})

test('conflict reconciles once without replay, failed readback blocks until retry', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'complete' },
  })
  await openPage(page, path)
  let posts = 0
  let brokenRead = true
  // Hydration does not imply the client-only private snapshot has arrived.
  await expect(
    page.getByRole('button', { name: 'Suivre ce cinéma', exact: true }),
  ).toBeEnabled()
  await page.route('**/api/v1/account/theater-follows', (route) => {
    if (route.request().method() === 'POST') {
      posts++
      return route.fulfill({
        status: 409,
        json: { error: { code: 'theater_follows_changed' } },
      })
    }
    return brokenRead
      ? route.fulfill({
          status: 503,
          json: { error: { code: 'accounts_unavailable' } },
        })
      : route.continue()
  })
  await page
    .getByRole('button', { name: 'Suivre ce cinéma', exact: true })
    .click()
  await expect(page.getByRole('alert')).toContainText('indisponible')
  await expect(
    page.getByRole('button', {
      name: 'Suivi indisponible pendant la vérification',
    }),
  ).toBeDisabled()
  expect(posts).toBe(1)
  brokenRead = false
  await page.getByRole('button', { name: 'Réessayer', exact: true }).click()
  await expect(
    page.getByRole('button', { name: 'Suivre ce cinéma', exact: true }),
  ).toBeEnabled()
  expect(posts).toBe(1)
})

test('late cinema error cannot appear after departure or move focus', async ({
  page,
  request,
}) => {
  await request.post('/__playwright/scenario', {
    data: { enabled: true, state: 'complete' },
  })
  await openPage(page, path)
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  let entered!: () => void
  const intercepted = new Promise<void>((resolve) => {
    entered = resolve
  })
  await page.route('**/api/v1/account/theater-follows', async (route) => {
    if (route.request().method() !== 'POST') return route.continue()
    entered()
    await held
    await route.fulfill({
      status: 409,
      json: { error: { code: 'theater_follows_changed' } },
    })
  })
  await page
    .getByRole('button', { name: 'Suivre ce cinéma', exact: true })
    .click()
  await intercepted
  // Existing navigation, no full-document reload: retain shared resource.
  await page.getByRole('link', { name: 'Mon compte', exact: true }).click()
  release()
  await expect(page).toHaveURL(/\/compte$/)
  await expect(page.getByText('Vos cinémas suivis ont changé.')).toHaveCount(0)
  expect(
    await page.evaluate(() => document.activeElement?.getAttribute('title')),
  ).not.toBe('Suivre ce cinéma')
})
