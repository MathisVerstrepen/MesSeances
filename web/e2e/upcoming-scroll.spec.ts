import { test, expect, openPage } from './fixtures'

for (const signedIn of [false, true]) {
  for (const query of ['', '?vue=historique&annee=2025&mois=10']) {
    test(`upcoming ${signedIn ? 'member' : 'guest'} ${query || 'default'} restores bottom after movie detail`, async ({
      page,
      request,
    }) => {
      await request.post('/__playwright/scenario', {
        data: {
          enabled: signedIn,
          state: signedIn ? 'complete' : 'anonymous',
          upcoming: 'delay',
        },
      })
      const path = `/films/prochainement${query}`
      const cards = page.locator('main section a[href^="/film/"]')
      await openPage(page, path)
      await expect(cards).toHaveCount(query ? 5 : 4)
      const last = cards.last()
      await last.scrollIntoViewIfNeeded()
      const saved = await page.evaluate(() => window.scrollY)
      expect(saved).toBeGreaterThan(1000)
      const detailPath = await last.getAttribute('href')
      await last.click()
      await expect(page).toHaveURL(detailPath!)
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
      await page.goBack()
      await expect(page).toHaveURL(path)
      await expect(cards).toHaveCount(query ? 5 : 4)
      await expect
        .poll(async () =>
          Math.abs((await page.evaluate(() => window.scrollY)) - saved),
        )
        .toBeLessThanOrEqual(5)
    })
  }
}
