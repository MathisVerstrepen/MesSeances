import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = (path: string) => readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')

test('Cinéville uses both exact supplied assets, no-inline imports, accessible names and credits', async () => {
  for (const [size, hash] of [
    ['small', '81ffddedbe8c9e75a759b57842b6bbcf5fe9a64354a6007f93d758e376dcb1f8'],
    ['large', '55900e7de3f80168d12058eecebcca8e5f6c753ed86e20ac054ded291d1fcd90']
  ]) {
    const bytes = await readFile(new URL(`../app/assets/imgs/cineville_logo_${size}.webp`, import.meta.url))
    assert.equal(createHash('sha256').update(bytes).digest('hex'), hash)
    assert.ok((await source('components/BrandLogo.vue')).includes(`cineville_logo_${size}.webp?no-inline`))
  }
  const logo = await source('components/BrandLogo.vue')
  assert.match(logo, /CINEVILLE: \{ inline: cinevilleLogoSmall, display: cinevilleLogoLarge \}/)
  assert.match(logo, /CINEVILLE: 'Cinéville'/)
  assert.match(logo, /:alt="decorative \? '' : accessibleNames\[brand\]"/)
  const branded = await source('components/BrandedText.vue')
  assert.match(branded, /Cinéville\|Cineville/)
  assert.match(branded, /brand === 'CINEVILLE'/)
  assert.match(await source('pages/credits.vue'), /brand: 'CINEVILLE', name: 'Cinéville', url: 'https:\/\/www\.cineville\.fr\/'/)
})

test('Cinéville appears in every existing admin provider surface, map and booking label', async () => {
  for (const page of ['sync', 'sync-schedules', 'tmdb-matches', 'theater-locations']) {
    const value = await source(`pages/admin/${page}.vue`)
    assert.match(value, /cineville: 'Cinéville'/)
    if (page.startsWith('sync')) assert.match(value, /\['ugc', 'kinepolis', 'pathe', 'cgr', 'megarama', 'cineville', 'mk2', 'cinewest', 'grandecran'\]/)
  }
  assert.match(await source('pages/admin/sync-schedules.vue'), /cineville: selectLatestProviderRun\('cineville'/)
  assert.match(await source('components/CinemaTheaterMap.client.vue'), /'cineville', THEATER_PROVIDER_COLORS\.cineville/)
  assert.match(await source('utils/theaterMap.ts'), /cineville: 'Cinéville'/)
  assert.match(await source('components/BookingLink.vue'), /cineville: 'Réserver sur Cinéville'/)
})

test('Cinéville ends use resolved provenance and shared rendering, with canonical-only cinema JSON-LD', async () => {
  for (const path of ['components/ShowtimeResultLine.vue', 'components/ShowtimeResultBox.vue']) {
    const value = await source(path)
    const renderers = value.match(/<ShowtimeEndTime :end="result\.end"/g) ?? []
    assert.ok(renderers.length >= 2)
    assert.doesNotMatch(value, /hasKnownShowtimeEnd|result\.endTime/)
  }
  assert.match(await source('pages/film/[slug].vue'), /<ShowtimeEndTime :end="showtime\.end"/)
  const timeline = await source('components/TimelineMatrix.vue')
  assert.match(timeline, /<ShowtimeEndTime :end="selectedEnd"/)
  assert.match(timeline, /resolveShowtimeEnd\(item\.showtime\)/)
  assert.match(timeline, /Math\.max\(durationMinutes \* pixelsPerMinute\.value, 56\)/)
  assert.match(timeline, /showtimeWidth\(0\) \/ pixelsPerMinute\.value/)
  const cinema = await source('pages/cinema/[slug].vue')
  assert.match(cinema, /unknownEnd = end === start/)
  assert.match(cinema, /if \(hasCanonicalShowtimeEnd\(showtime\.start_time, showtime\.end_time\)\) event\.endDate = showtime\.end_time/)
})
