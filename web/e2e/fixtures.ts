import { test as base, expect, type Page } from '@playwright/test'

export const test = base.extend<{ scenario: void }>({
  // Auto fixtures run for every importing spec, unlike module-level hooks,
  // which are registered only on the first file to import a cached module.
  scenario: [
    async ({ request }, use) => {
      const ready = await request.post('/__playwright/scenario', { data: {} })
      expect(ready.ok()).toBeTruthy()
      await use()
      const response = await request.get('/__playwright/state')
      expect(response.ok()).toBeTruthy()
      expect(
        (await response.json()).unexpected,
        'Unmocked API requests',
      ).toEqual([])
    },
    { auto: true },
  ],
  page: async ({ page }, use) => {
    const errors: string[] = []
    const external: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.context().route('**/*', (route) => {
      const url = new URL(route.request().url())
      if (url.hostname === '127.0.0.1' || url.protocol === 'data:')
        return route.continue()
      external.push(url.origin)
      return route.abort()
    })
    await use(page)
    expect(errors, 'Uncaught browser errors').toEqual([])
    expect(external, 'Attempted external requests').toEqual([])
  },
})

export { expect }

export async function openPage(page: Page, path: string) {
  const response = await page.goto(path)
  // The fixture's app:mounted hook marks hydration completion, without sleeps.
  await expect(page.locator('html')).toHaveAttribute(
    'data-playwright-ready',
    'true',
  )
  return response
}

// Directory filters render inline on desktop and in a bottom sheet on mobile.
export async function openDirectoryFilters(page: Page) {
  const trigger = page.getByRole('button', { name: /^Filtres/ })
  if (!(await trigger.isVisible())) return page.locator('#cinema-results')
  await trigger.click()
  const sheet = page.getByRole('dialog', { name: 'Filtres', exact: true })
  await expect(sheet).toBeVisible()
  return sheet
}

export async function closeDirectoryFilters(page: Page) {
  const close = page.getByRole('button', { name: 'Fermer les filtres' })
  if (await close.isVisible()) await close.click()
  await expect(page.getByRole('dialog', { name: 'Filtres' })).toHaveCount(0)
}
