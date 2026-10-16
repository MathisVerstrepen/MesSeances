import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { computed, createSSRApp, type Component } from 'vue'
import ts from 'typescript'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import { veoShowingId } from '../app/utils/veo.ts'
import {
  THEATER_PROVIDER_LABELS,
  THEATER_PROVIDER_COLORS,
  buildTheaterFeatureCollection,
} from '../app/utils/theaterMap.ts'
import {
  cinemaDirectoryChains,
  normalizeCinemaChains,
  filterCinemaDirectory,
  toggleCinemaChain,
} from '../app/utils/cinemaDirectoryChains.ts'
import {
  cinemaApiQuery,
  parseCinemaRoute,
  cinemaProviderLabels,
} from '../app/utils/adminCinemaFilters.ts'
import {
  parseStatisticsQuery,
  statisticsChainLabels,
} from '../app/utils/statistics.ts'
import { publicCinemaImageUrl } from '../app/utils/cinemaImage.ts'
import { theaterDisplayName } from '../app/utils/theaterDisplayName.ts'
import { cinemaDescription } from '../app/utils/entityDescriptions.ts'
import type { Theater } from '../app/types/api.ts'

const source = (path: string) =>
  readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')
const require = createRequire(import.meta.url)
interface CompiledComponentExports {
  default?: Component
}
async function component(name: string): Promise<Component> {
  const { descriptor } = parse(await source(`components/${name}.vue`))
  const script = compileScript(descriptor, { id: name, inlineTemplate: true })
  const compiled = ts.transpileModule(script.content, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText
  const exports: CompiledComponentExports = {}
  const localRequire = (id: string) =>
    id.startsWith('~/assets/')
      ? { __esModule: true, default: id }
      : id === '~/utils/bookingUrl'
        ? { safeBookingUrl }
        : require(id)
  new Function('require', 'exports', 'computed', compiled)(
    localRequire,
    exports,
    computed,
  )
  assert.ok(exports.default)
  return exports.default
}
const [BrandLogo, BrandedText, TheaterName, BookingLink] = await Promise.all(
  ['BrandLogo', 'BrandedText', 'TheaterName', 'BookingLink'].map(component),
)
async function render(
  root: Component,
  props: Parameters<typeof createSSRApp>[1],
) {
  const app = createSSRApp(root, props)
  app.component('BrandLogo', BrandLogo!)
  app.component('BrandedText', BrandedText!)
  return renderToString(app)
}

test('Véo supplied transparent artwork is statically imported, contained and accessible at both sizes', async () => {
  const logo = await source('assets/brands/veocinema_logo.svg')
  assert.match(logo, /viewBox="0 0 2357 1485"/)
  assert.match(logo, /Transparent background/)
  assert.doesNotMatch(logo, /<rect|<image|https?:\/\/(?!www.w3.org)/)
  for (const variant of ['inline', 'display']) {
    const html = await render(BrandLogo!, { brand: 'Véo Cinémas', variant })
    assert.match(html, /veocinema_logo.svg\?no-inline/)
    assert.match(html, /alt="Véo Cinémas"/)
    assert.match(html, /object-contain/)
    assert.doesNotMatch(html, /bg-white|bg-ink/)
  }
  assert.match(
    await render(BrandLogo!, { brand: 'Véo Cinémas', decorative: true }),
    /alt(?:="")? aria-hidden="true"/,
  )
  assert.match(
    await source('pages/credits.vue'),
    /brand: 'Véo Cinémas',\s*name: 'Véo Cinémas',\s*url: 'https:\/\/veocinemas.fr'/,
  )
})

test('Véo accent/case whole-word text recognition keeps source names and avoids duplicate spoken brand', async () => {
  for (const name of [
    'Véo',
    'Veo',
    'VÉO',
    'Véo Cinémas',
    'VEO CINEMAS',
    'Véo Cinémas'.normalize('NFD'),
  ]) {
    const text = `${name} Muret`
    const html = await render(BrandedText!, { text })
    assert.equal((html.match(/<img /g) || []).length, 1)
    assert.ok(html.includes(`class="sr-only">${text}`))
    const theater = await render(TheaterName!, { name: text, provider: 'veo' })
    assert.equal((theater.match(/<img /g) || []).length, 1)
    assert.match(theater, /alt(?:="")? aria-hidden="true"/)
    assert.ok(theater.includes(text))
    assert.equal(theaterDisplayName({ name, city: 'Muret' }), `${name} Muret`)
  }
  for (const name of ['Véolia', 'éVeo', 'Veo_centre', 'Veo2', 'Le Castelia']) {
    assert.doesNotMatch(await render(BrandedText!, { text: name }), /<img /)
    assert.match(
      await render(TheaterName!, { name, provider: 'veo' }),
      /alt="Véo Cinémas"/,
    )
    assert.equal(theaterDisplayName({ name, city: 'Muret' }), name)
  }
})

test('Véo BookingLink separates exact reservation and website fallback names and never emits unsafe hrefs', async () => {
  const ticket =
    'https://castelginest.miracinemas.fr/#showsession?id=emsx136100000001'
  const props = {
    url: ticket,
    provider: 'veo',
    showtimeId: veoShowingId('EMS1361', 'emsx136100000001'),
    theaterId: 'veo-EMS1361',
  }
  for (const url of [
    ticket,
    'https://castelginest.miracinemas.fr/',
    'https://achat.veocinemas.fr/muret/r/19',
  ]) {
    const html = await render(
      BookingLink!,
      url.includes('achat.')
        ? {
            url,
            provider: 'veo',
            theaterId: 'veo-P1234',
            showtimeId: veoShowingId('P1234', 'Source_001'),
          }
        : { ...props, url },
    )
    assert.match(html, /target="_blank" rel="noopener noreferrer"/)
    assert.match(
      html,
      url.endsWith('/')
        ? /aria-label="Site du cinéma Véo, ouverture dans un nouvel onglet"/
        : /aria-label="Réserver sur Véo Cinémas, ouverture dans un nouvel onglet"/,
    )
  }
  for (const overrides of [
    { url: `${ticket}&relay=1` },
    { theaterId: 'veo-P1234' },
    { showtimeId: null },
    { provider: 'cgr' },
    { url: null },
  ]) {
    const html = await render(BookingLink!, { ...props, ...overrides })
    assert.doesNotMatch(html, /href=/)
    assert.match(html, /aria-disabled="true"/)
    assert.match(html, /Réservation indisponible/)
  }
})

test('Véo is exhaustive across public/admin registries, chain filters, statistics, map and safe images', async () => {
  const theater: Theater = {
    provider: 'veo',
    id: 'veo-P1234',
    slug: 'veo-P1234',
    name: 'Véo Muret',
    city: 'Muret',
    city_slug: 'muret',
    postal_code: '31600',
    address: '',
    accepted_passes: [],
    available_dates: [],
    latitude: 43.46,
    longitude: 1.33,
  }
  assert.equal(THEATER_PROVIDER_LABELS.veo, 'Véo Cinémas')
  assert.equal(THEATER_PROVIDER_COLORS.veo, '#8e1a6f')
  assert.equal(cinemaProviderLabels.veo, 'Véo Cinémas')
  assert.equal(statisticsChainLabels.veo, 'Véo Cinémas')
  assert.deepEqual(cinemaDirectoryChains([theater]), ['veo'])
  assert.deepEqual(normalizeCinemaChains(['VEO', 'veo', 'unknown']), ['veo'])
  assert.deepEqual(toggleCinemaChain([], 'veo'), ['veo'])
  assert.deepEqual(filterCinemaDirectory([theater], 'muret', ['veo']), [
    theater,
  ])
  assert.deepEqual(filterCinemaDirectory([theater], '', ['ugc']), [])
  assert.equal(
    cinemaApiQuery(parseCinemaRoute({ provider: 'veo' })).provider,
    'veo',
  )
  assert.equal(parseStatisticsQuery({ chain: 'veo' }).query.chain, 'veo')
  assert.deepEqual(
    buildTheaterFeatureCollection([theater], new Set([theater.id])).features[0]!
      .properties,
    { id: theater.id, provider: 'veo', favorite: true },
  )
  assert.equal(
    publicCinemaImageUrl(
      { url: '/api/v1/theaters/veo/P1234/image/1', width: 800, height: 600 },
      'https://api.example.test',
    ),
    'https://api.example.test/api/v1/theaters/veo/P1234/image/1',
  )
  assert.equal(
    publicCinemaImageUrl(
      { url: '/api/v1/theaters/veo/../P1234/image/1', width: 800, height: 600 },
      '',
    ),
    '',
  )
  assert.match(
    cinemaDescription({
      provider: 'veo',
      name: theater.name,
      city: theater.city,
      availableDateCount: 1,
    }),
    /cinéma Véo Cinémas/,
  )
  for (const page of [
    'sync',
    'sync-schedules',
    'theater-locations',
    'tmdb-matches',
  ]) {
    const value = await source(`pages/admin/${page}.vue`)
    assert.match(value, /veo: 'Véo Cinémas'/)
    if (page.startsWith('sync'))
      assert.match(value, /'noecinemas',\s*'veo',\s*\] as const/)
  }
  assert.match(
    await source('pages/admin/sync-schedules.vue'),
    /veo: selectLatestProviderRun\(\s*'veo'/,
  )
  assert.match(
    await source('components/CinemaTheaterMap.client.vue'),
    /'veo',\s*THEATER_PROVIDER_COLORS.veo/,
  )
})
