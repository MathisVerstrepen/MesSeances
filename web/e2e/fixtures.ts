import { test as base, expect, type Page } from '@playwright/test'

export const test = base.extend({
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

test.afterEach(async ({ request }) => {
  const response = await request.get('/__playwright/state')
  expect(response.ok()).toBeTruthy()
  expect((await response.json()).unexpected, 'Unmocked API requests').toEqual(
    [],
  )
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
