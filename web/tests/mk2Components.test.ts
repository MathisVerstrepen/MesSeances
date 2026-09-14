import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = (path: string) => readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')

test('MK2 imports supplied SVG as an image for both sizes with accessible and decorative semantics', async () => {
  const svg = await source('assets/imgs/mk2_logo.svg')
  // apply_patch appends one terminal newline; all supplied SVG bytes are unchanged.
  assert.equal(createHash('sha256').update(svg.replace(/\n$/, '')).digest('hex'), 'd9ffc0b3862b6d4a2015aef14cab76b8d4829f50c939e34ef75b15e59b5f0b62')
  assert.doesNotMatch(svg, /<script|<foreignObject|(?:href|onload)=/i)
  const logo = await source('components/BrandLogo.vue')
  assert.match(logo, /mk2_logo\.svg\?no-inline/)
  assert.match(logo, /MK2: \{ inline: mk2Logo, display: mk2Logo \}/)
  assert.match(logo, /MK2: 'MK2'/)
  assert.match(logo, /:alt="decorative \? '' : accessibleNames\[brand\]"/)
  assert.match(await source('components/BrandedText.vue'), /brand === 'MK2'/)
  assert.match(await source('pages/credits.vue'), /brand: 'MK2', name: 'MK2', url: 'https:\/\/www\.mk2\.com\/'/)
})

test('MK2 appears in individual, all-run and schedule provider surfaces without adding controls', async () => {
  for (const page of ['sync', 'sync-schedules', 'tmdb-matches', 'theater-locations']) {
    const value = await source(`pages/admin/${page}.vue`)
    assert.match(value, /mk2: 'MK2'/)
    if (page.startsWith('sync')) assert.match(value, /\['ugc', 'kinepolis', 'pathe', 'cgr', 'megarama', 'cineville', 'mk2', 'cinewest', 'grandecran', 'noecinemas'\]/)
  }
  assert.match(await source('pages/admin/sync-schedules.vue'), /mk2: selectLatestProviderRun\('mk2'/)
  assert.match(await source('components/CinemaTheaterMap.client.vue'), /'mk2', THEATER_PROVIDER_COLORS\.mk2/)
  assert.match(await source('utils/theaterMap.ts'), /mk2: '#334155'/)
  assert.match(await source('utils/theaterMap.ts'), /mk2: 'MK2'/)
  assert.match(await source('components/BookingLink.vue'), /mk2: 'Réserver sur MK2'/)
})

test('every shared showtime surface suppresses silent language and binds booking to showing identity', async () => {
  for (const path of ['components/ShowtimeResultLine.vue', 'components/ShowtimeResultBox.vue']) {
    const value = await source(path)
    assert.match(value, /v-if="result\.language"/)
    assert.match(value, /v-if="result\.room"/)
    assert.equal((value.match(/<BookingLink\b/g) ?? []).length, (value.match(/:showtime-id="result\.showtimeId"/g) ?? []).length)
  }
  const film = await source('pages/film/[slug].vue')
  assert.match(film, /v-if="showtime\.language"/)
  assert.match(film, /:showtime-id="showtime\.id"/)
  const timeline = await source('components/TimelineMatrix.vue')
  assert.match(timeline, /v-if="item\.showtime\.language"/)
  assert.match(timeline, /v-if="selected\.showtime\.language"/)
  assert.match(timeline, /:showtime-id="selected\.showtime\.id"/)
})
