import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = (path: string) =>
  readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')

test('Cinewest uses the unchanged supplied logo for both sizes with accessible names and credits', async () => {
  const bytes = await readFile(
    new URL('../app/assets/imgs/cinewest_logo_small.webp', import.meta.url),
  )
  assert.equal(
    createHash('sha256').update(bytes).digest('hex'),
    '158b83dd20ab4fd7f8ef655bffb2ee0270a64878d51651cd384a84291a28de0c',
  )
  const logo = await source('components/BrandLogo.vue')
  assert.match(logo, /cinewest_logo_small\.webp\?no-inline/)
  assert.match(
    logo,
    /CINEWEST: \{ inline: cinewestLogoSmall, display: cinewestLogoSmall \}/,
  )
  assert.match(logo, /CINEWEST: 'Cinewest'/)
  assert.match(logo, /:alt="decorative \? '' : accessibleNames\[brand\]"/)
  assert.match(logo, /:aria-hidden="decorative \? 'true' : undefined"/)
  const branded = await source('components/BrandedText.vue')
  assert.match(branded, /MK2\|Cinewest/)
  assert.match(branded, /brand === 'CINEWEST'/)
  assert.match(
    branded,
    /<span v-if="!decorative" class="sr-only">\{\{ text \}\}<\/span>/,
  )
  assert.match(
    await source('pages/credits.vue'),
    /brand: 'CINEWEST', name: 'Cinewest', url: 'https:\/\/www\.cinewest\.fr\/'/,
  )
})

test('Cinewest follows MK2 in all existing admin provider surfaces and map without new controls', async () => {
  for (const page of [
    'sync',
    'sync-schedules',
    'tmdb-matches',
    'theater-locations',
  ]) {
    const value = await source(`pages/admin/${page}.vue`)
    assert.match(value, /cinewest: 'Cinewest'/)
    if (page.startsWith('sync'))
      assert.match(
        value,
        /\[\s*'ugc',\s*'kinepolis',\s*'pathe',\s*'cgr',\s*'megarama',\s*'cineville',\s*'mk2',\s*'cinewest',\s*'grandecran',\s*'noecinemas',?\s*\]/,
      )
  }
  assert.match(
    await source('pages/admin/sync.vue'),
    /const targets = \['all', \.\.\.providers\]/,
  )
  const schedules = await source('pages/admin/sync-schedules.vue')
  assert.match(
    schedules,
    /const targets = \[\s*\.\.\.providers,\s*'tmdb_metadata_refresh',\s*'tmdb_upcoming_movies',?\s*\]/,
  )
  assert.match(schedules, /cinewest: selectLatestProviderRun\(\s*'cinewest'/)
  assert.match(
    await source('components/CinemaTheaterMap.client.vue'),
    /'cinewest',\s+THEATER_PROVIDER_COLORS\.cinewest/,
  )
  assert.match(
    await source('components/BookingLink.vue'),
    /cinewest: 'Réserver sur Cinewest'/,
  )
  assert.match(
    await source('components/BookingLink.vue'),
    /booking\.provider === 'cinewest'\s*\? 'Site du cinéma Cinewest'\s*: 'Site du cinéma Megarama'/,
  )
})

test('Cinewest uses existing canonical, estimated and unknown end renderers and silent language guards', async () => {
  for (const path of [
    'components/ShowtimeResultLine.vue',
    'components/ShowtimeResultBox.vue',
  ]) {
    const value = await source(path)
    assert.match(value, /<ShowtimeEndTime\s+:end="result\.end"/)
    assert.match(value, /v-if="result\.language"/)
    assert.doesNotMatch(value, /result\.endTime/)
  }
  const film = await source('pages/film/[slug].vue')
  assert.match(film, /<ShowtimeEndTime\s+:end="showtime\.end"/)
  assert.match(film, /v-if="showtime\.language"/)
  const timeline = await source('components/TimelineMatrix.vue')
  assert.match(timeline, /<ShowtimeEndTime\s+:end="selectedEnd"/)
  assert.match(timeline, /resolveShowtimeEnd\(item\.showtime\)/)
  assert.match(
    timeline,
    /width: showtimeWidth\(item\.showtime\.duration_minutes\)/,
  )
  assert.match(
    timeline,
    /\? item\.showtime\.duration_minutes\s*: showtimeWidth\(0\) \/ pixelsPerMinute\.value/,
  )
  assert.match(timeline, /v-if="selected\.showtime\.language"/)
  const cinema = await source('pages/cinema/[slug].vue')
  assert.match(
    cinema,
    /if \(hasCanonicalShowtimeEnd\(showtime\.start_time, showtime\.end_time\)\)\s+event\.endDate = showtime\.end_time/,
  )
})
