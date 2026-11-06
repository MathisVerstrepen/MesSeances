import { test, expect, openPage } from './fixtures'
import { movie, movieSchedule, theater } from './data.mjs'

const mp4 = 'https://fr.vid.web.acsta.net/nmedia/33/synthetic_hd.mp4'

test('provider MP4 loads on demand, handles errors, and restores keyboard focus', async ({
  page,
}, testInfo) => {
  await page.route('**/api/v1/movies/*/showtimes*', async (route) => {
    const payload = movieSchedule(
      movie.slug,
      new URL(route.request().url()).searchParams,
    )
    payload.movie.trailer = {
      type: 'mp4',
      url: mp4,
      source: 'cgr',
      language: null,
    }
    await route.fulfill({ json: payload })
  })
  let requests = 0
  let release!: () => void
  const response = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.context().route(mp4, async (route) => {
    requests++
    await response
    await route.fulfill({ status: 503, body: '' })
  })
  await openPage(page, `/cinema/${theater.slug}`)
  await page.locator(`a[href="/film/${movie.slug}"]`).click()
  const trigger = page.getByRole('button', {
    name: `Voir la bande-annonce de ${movie.title}`,
    exact: true,
  })
  await expect(trigger).toBeVisible()
  expect(requests).toBe(0)
  await trigger.focus()
  await trigger.press('Enter')
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  const video = dialog.locator('video')
  await expect(video).toHaveAttribute('src', mp4)
  await expect(video).toHaveAttribute('controls', '')
  await expect(
    dialog.getByRole('button', { name: /Fermer la bande-annonce/ }),
  ).toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('provider-trailer.png') })
  release()
  await expect(dialog.getByRole('status')).toContainText(
    'momentanément indisponible',
  )
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(page.locator('video')).toHaveCount(0)
  await expect(trigger).toBeFocused()
})

for (const tmdb of [false, true]) {
  test(`YouTube trailer remains lazy and ${tmdb ? 'TMDB takes precedence' : 'Cinéville fills the missing trailer'}`, async ({
    page,
  }) => {
    await page.route('**/api/v1/movies/*/showtimes*', async (route) => {
      const payload = movieSchedule(
        movie.slug,
        new URL(route.request().url()).searchParams,
      )
      Object.assign(payload.movie, {
        trailer: {
          type: 'youtube',
          url: 'https://www.youtube.com/watch?v=AbC-12_xYz9',
          source: 'cineville',
          language: null,
        },
        trailer_vf_youtube_key: tmdb ? 'VoT-98_rQp7' : null,
      })
      await route.fulfill({ json: payload })
    })
    let requests = 0
    const key = tmdb ? 'VoT-98_rQp7' : 'AbC-12_xYz9'
    const embed = `https://www.youtube-nocookie.com/embed/${key}?autoplay=1`
    await page.context().route(embed, (route) => {
      requests++
      return route.fulfill({
        contentType: 'text/html',
        body: '<title>Synthetic player</title>',
      })
    })
    await openPage(page, `/cinema/${theater.slug}`)
    await page.locator(`a[href="/film/${movie.slug}"]`).click()
    const trigger = page.getByRole('button', { name: /Voir la bande-annonce/ })
    await expect(trigger).toBeVisible()
    expect(requests).toBe(0)
    await trigger.click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.locator('iframe')).toHaveAttribute('src', embed)
    await expect(dialog.locator('video')).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(trigger).toBeFocused()
  })
}
