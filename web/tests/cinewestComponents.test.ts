import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = (path: string) => readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')

test('Cinewest uses the unchanged supplied logo for both sizes with accessible names and credits', async () => {
  const bytes = await readFile(new URL('../app/assets/imgs/cinewest_logo_small.webp', import.meta.url))
  assert.equal(createHash('sha256').update(bytes).digest('hex'), 'cdd24d3c2085d890b3713ba2ca5ccc4e52278bcd94171e784aadeaae19532c65')
  const logo = await source('components/BrandLogo.vue')
  assert.match(logo, /cinewest_logo_small\.webp\?no-inline/)
  assert.match(logo, /CINEWEST: \{ inline: cinewestLogoSmall, display: cinewestLogoSmall \}/)
  assert.match(logo, /CINEWEST: 'Cinewest'/)
  assert.match(logo, /:alt="decorative \? '' : accessibleNames\[brand\]"/)
  assert.match(logo, /:aria-hidden="decorative \? 'true' : undefined"/)
  const branded = await source('components/BrandedText.vue')
  assert.match(branded, /MK2\|Cinewest/)
  assert.match(branded, /brand === 'CINEWEST'/)
  assert.match(branded, /<span v-if="!decorative" class="sr-only">\{\{ text \}\}<\/span>/)
  assert.match(await source('pages/credits.vue'), /brand: 'CINEWEST', name: 'Cinewest', url: 'https:\/\/www\.cinewest\.fr\/'/)
})

test('Cinewest follows MK2 in all existing admin provider surfaces and map without new controls', async () => {
  for (const page of ['sync', 'sync-schedules', 'tmdb-matches', 'theater-locations']) {
    const value = await source(`pages/admin/${page}.vue`)
    assert.match(value, /cinewest: 'Cinewest'/)
    if (page.startsWith('sync')) assert.match(value, /\['ugc', 'kinepolis', 'pathe', 'cgr', 'megarama', 'cineville', 'mk2', 'cinewest'\]/)
  }
  assert.match(await source('pages/admin/sync.vue'), /const targets = \['all', \.\.\.providers\]/)
  const schedules = await source('pages/admin/sync-schedules.vue')
  assert.match(schedules, /const targets = \[\.\.\.providers, 'tmdb_metadata_refresh', 'tmdb_upcoming_movies'\]/)
  assert.match(schedules, /cinewest: selectLatestProviderRun\('cinewest'/)
  assert.match(await source('components/CinemaTheaterMap.client.vue'), /'cinewest', THEATER_PROVIDER_COLORS\.cinewest/)
  assert.match(await source('components/BookingLink.vue'), /cinewest: 'Réserver sur Cinewest'/)
  assert.match(await source('components/BookingLink.vue'), /booking\.provider === 'cinewest' \? 'Site du cinéma Cinewest' : 'Site du cinéma Megarama'/)
})

test('Cinewest uses existing canonical, estimated and unknown end renderers and silent language guards', async () => {
  for (const path of ['components/ShowtimeResultLine.vue', 'components/ShowtimeResultBox.vue']) {
    const value = await source(path)
    assert.match(value, /<ShowtimeEndTime :end="result\.end"/)
    assert.match(value, /v-if="result\.language"/)
    assert.doesNotMatch(value, /result\.endTime/)
  }
  const film = await source('pages/film/[slug].vue')
  assert.match(film, /<ShowtimeEndTime :end="showtime\.end"/)
  assert.match(film, /v-if="showtime\.language"/)
  const timeline = await source('components/TimelineMatrix.vue')
  assert.match(timeline, /<ShowtimeEndTime :end="selectedEnd"/)
  assert.match(timeline, /resolveShowtimeEnd\(item\.showtime\)/)
  assert.match(timeline, /width: showtimeWidth\(item\.showtime\.duration_minutes\)/)
  assert.match(timeline, /\? item\.showtime\.duration_minutes : showtimeWidth\(0\) \/ pixelsPerMinute\.value/)
  assert.match(timeline, /v-if="selected\.showtime\.language"/)
  const cinema = await source('pages/cinema/[slug].vue')
  assert.match(cinema, /if \(hasCanonicalShowtimeEnd\(showtime\.start_time, showtime\.end_time\)\) event\.endDate = showtime\.end_time/)
})
