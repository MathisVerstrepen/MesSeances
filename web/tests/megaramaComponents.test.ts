import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = (path: string) => readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')

test('uses the exact supplied logo for both sizes with accessible branding and credit', async () => {
  const logoBytes = await readFile(new URL('../app/assets/imgs/megarama_logo_small.webp', import.meta.url))
  assert.equal(createHash('sha256').update(logoBytes).digest('hex'), 'b18a9facc4c8289ee25893a45d01bf1ebe453e0d1b612fef77663102b00beff3')
  const logo = await source('components/BrandLogo.vue')
  assert.match(logo, /MEGARAMA: \{ inline: megaramaLogoSmall, display: megaramaLogoSmall \}/)
  assert.match(logo, /MEGARAMA: 'Megarama'/)
  assert.match(await source('components/BrandedText.vue'), /CGR\|Megarama\|IMAX/)
  assert.match(await source('pages/credits.vue'), /brand: 'MEGARAMA', name: 'Megarama', url: 'https:\/\/www\.megarama\.fr\/'/)
})

test('integrates Megarama into admin targets, labels, latest runs, and the labeled map palette', async () => {
  for (const page of ['sync', 'sync-schedules', 'tmdb-matches', 'theater-locations']) {
    const value = await source(`pages/admin/${page}.vue`)
    assert.match(value, /megarama: 'Megarama'/)
    if (page.startsWith('sync')) assert.match(value, /\['ugc', 'kinepolis', 'pathe', 'cgr', 'megarama'\]/)
  }
  assert.match(await source('pages/admin/sync-schedules.vue'), /megarama: selectLatestProviderRun\('megarama'/)
  assert.match(await source('components/CinemaTheaterMap.client.vue'), /'megarama', THEATER_PROVIDER_COLORS\.megarama/)
  assert.match(await source('utils/theaterMap.ts'), /megarama: 'Megarama'/)
})

test('website fallback overrides reservation aria text and all custom slots show the truthful label', async () => {
  const booking = await source('components/BookingLink.vue')
  assert.match(booking, /booking\.kind === 'website' \? 'Site du cinéma Megarama'/)
  assert.match(booking, /reservation\.kind === 'website' \? reservation\.label : ariaLabel \|\| reservation\.label/)
  assert.match(booking, /:kind="reservation\.kind" :label="reservation\.label"/)
  assert.match(booking, /megarama: 'Réserver sur Megarama'/)
  for (const path of ['components/ShowtimeResultLine.vue', 'components/ShowtimeResultBox.vue', 'pages/film/[slug].vue']) {
    const value = await source(path)
    assert.doesNotMatch(value, /(?:v-slot|#default)="\{ available \}"/)
    assert.match(value, /kind === 'website'/)
    assert.match(value, /\{ available, kind, label \}/)
  }
})

test('every end-time renderer uses the shared sentinel helper, with reachable unknown timeline items', async () => {
  for (const path of ['components/ShowtimeResultLine.vue', 'components/ShowtimeResultBox.vue']) {
    const value = await source(path)
    assert.match(value, /hasKnownShowtimeEnd\(props\.result\.provider, props\.result\.advertisedStartTime, props\.result\.endTime\)/)
    const endRenderers = value.match(/<(?:template|span) v-if="hasKnownEnd"[^>]*>(?:→|fin) \{\{ formatParisTime\(result\.endTime\) \}\}<\/(?:template|span)>/g) ?? []
    const allEndRenderers = value.match(/formatParisTime\(result\.endTime\)/g) ?? []
    assert.equal(endRenderers.length, allEndRenderers.length)
    assert.ok(endRenderers.length >= 2)
  }
  assert.match(await source('pages/film/[slug].vue'), /v-if="hasKnownShowtimeEnd\(showtime\.provider, showtime\.start_time, showtime\.end_time\)"/)
  const timeline = await source('components/TimelineMatrix.vue')
  assert.match(timeline, /v-if="hasKnownShowtimeEnd\(selected\.showtime\.provider, selected\.showtime\.start_time, selected\.showtime\.end_time\)"/)
  assert.match(timeline, /Math\.max\(durationMinutes \* pixelsPerMinute\.value, 56\)/)
  assert.match(timeline, /showtimeWidth\(0\) \/ pixelsPerMinute\.value/)
  const cinema = await source('pages/cinema/[slug].vue')
  assert.match(cinema, /unknownMegaramaEnd = showtime\.provider === 'megarama' && end === start/)
  assert.match(cinema, /if \(hasKnownShowtimeEnd\(showtime\.provider, showtime\.start_time, showtime\.end_time\)\) event\.endDate = showtime\.end_time/)
})
