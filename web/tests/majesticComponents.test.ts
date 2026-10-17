import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { computed, createSSRApp, type Component } from 'vue'
import ts from 'typescript'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
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
import { cinemaDescription } from '../app/utils/entityDescriptions.ts'
import type { Theater } from '../app/types/api.ts'

// Use the existing Véo SFC compiler/SSR acceptance pattern, without a browser server.
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

test('Majestic supplied SVG stays byte-identical and both logo variants have accessible/decorative semantics', async () => {
  const asset = await readFile(
    new URL('../app/assets/brands/majestic_logo.svg', import.meta.url),
  )
  assert.equal(
    createHash('sha256').update(asset).digest('hex'),
    '78ed3aaeeea940f7436ecf91df9d1bc0d1801a255ca9f73d7d2dbdd2653f7032',
  )
  for (const variant of ['inline', 'display']) {
    const html = await render(BrandLogo!, {
      brand: 'Majestic Cinémas',
      variant,
    })
    assert.match(html, /majestic_logo.svg\?no-inline/)
    assert.match(html, /alt="Majestic Cinémas"/)
    assert.match(
      await render(BrandLogo!, {
        brand: 'Majestic Cinémas',
        variant,
        decorative: true,
      }),
      /alt(?:="")? aria-hidden="true"/,
    )
  }
  assert.match(
    await source('pages/credits.vue'),
    /brand: 'Majestic Cinémas',\s*name: 'Majestic Cinémas',\s*url: 'https:\/\/www\.majestic-cinemas\.com\/'/,
  )
})

test('Majestic whole words brand source names without swallowing unrelated substrings or duplicating spoken identity', async () => {
  for (const name of [
    'Majestic',
    'MAJESTIC CINEMAS',
    'Majestic Cinémas',
    'Majestic Cinémas'.normalize('NFD'),
  ]) {
    const text = `${name} Rive Gauche`
    const html = await render(BrandedText!, { text })
    assert.equal((html.match(/<img /g) || []).length, 1)
    assert.ok(html.includes(`class="sr-only">${text}`))
    const theater = await render(TheaterName!, {
      name: text,
      provider: 'majestic',
    })
    assert.match(theater, /alt(?:="")? aria-hidden="true"/)
    assert.ok(theater.includes(text))
  }
  for (const name of [
    'Majestically',
    'éMajestic',
    'Majestic2',
    'Majestic_centre',
    'Ciné Laon',
    'Ciné Vendôme',
  ]) {
    assert.doesNotMatch(await render(BrandedText!, { text: name }), /<img /)
    for (const variant of ['inline', 'hero']) {
      const html = await render(TheaterName!, {
        name,
        provider: 'majestic',
        variant,
      })
      assert.equal((html.match(/<img /g) || []).length, 1)
      assert.match(html, /alt="Majestic Cinémas"/)
      assert.ok(html.includes(name))
    }
  }
})

test('Majestic BookingLink preserves global/local source hrefs, distinct website labels, safe targets and disabled states', async () => {
  const props = {
    provider: 'majestic',
    showtimeId: 'majestic-showing-emsx070000000001',
    theaterId: 'majestic-EMS0700',
  }
  const global =
    'https://www.ticketingcine.com/EMS0700/FRZH1EX#showsession?id=emsx070000000001'
  const local =
    'https://www.ticketingcine.com?EMS0700#showsession?id=emsx070000000001'
  for (const url of [
    global,
    local,
    `${global}&ps=ticketingcine`,
    `${local}&ps=ticketingcine`,
    'https://majestic-compiegne.fr/',
  ]) {
    const html = await render(BookingLink!, { ...props, url })
    assert.ok(html.includes(`href="${url.replaceAll('&', '&amp;')}"`))
    assert.match(html, /target="_blank" rel="noopener noreferrer"/)
    assert.match(
      html,
      url.endsWith('/')
        ? /aria-label="Site du cinéma Majestic, ouverture dans un nouvel onglet"/
        : /aria-label="Réserver sur Majestic Cinémas, ouverture dans un nouvel onglet"/,
    )
  }
  for (const overrides of [
    { url: null },
    { url: '' },
    { url: `${global}&x=1` },
    { url: 'https://douai.majestic-cinemas.com/' },
    { url: 'https://www.ticketingcine.com/' },
    { theaterId: 'majestic-EMS0654' },
    { showtimeId: null },
    { provider: 'megarama' },
  ]) {
    const html = await render(BookingLink!, {
      ...props,
      url: global,
      ...overrides,
    })
    assert.doesNotMatch(html, /href=/)
    assert.match(html, /aria-disabled="true"/)
    assert.match(html, /Réservation indisponible/)
  }
})

test('Majestic public filters, image routes, map, descriptions and admin registries include the typed provider', async () => {
  const theater: Theater = {
    provider: 'majestic',
    id: 'majestic-EMS1299',
    slug: 'majestic-EMS1299',
    name: 'Ciné Laon',
    city: 'Laon',
    city_slug: 'laon',
    postal_code: '02000',
    address: '',
    accepted_passes: [],
    available_dates: [],
    latitude: 49.57,
    longitude: 3.62,
  }
  assert.equal(THEATER_PROVIDER_LABELS.majestic, 'Majestic Cinémas')
  assert.equal(THEATER_PROVIDER_COLORS.majestic, '#bb1837')
  assert.equal(cinemaProviderLabels.majestic, 'Majestic Cinémas')
  assert.equal(statisticsChainLabels.majestic, 'Majestic Cinémas')
  assert.deepEqual(cinemaDirectoryChains([theater]), ['majestic'])
  assert.deepEqual(normalizeCinemaChains(['MAJESTIC', 'majestic', 'unknown']), [
    'majestic',
  ])
  assert.deepEqual(toggleCinemaChain([], 'majestic'), ['majestic'])
  assert.deepEqual(filterCinemaDirectory([theater], 'laon', ['majestic']), [
    theater,
  ])
  assert.deepEqual(filterCinemaDirectory([theater], '', ['ugc']), [])
  assert.equal(
    cinemaApiQuery(parseCinemaRoute({ provider: 'majestic' })).provider,
    'majestic',
  )
  assert.equal(
    parseStatisticsQuery({ chain: 'majestic' }).query.chain,
    'majestic',
  )
  assert.deepEqual(
    buildTheaterFeatureCollection([theater], new Set([theater.id])).features[0]!
      .properties,
    { id: theater.id, provider: 'majestic', favorite: true },
  )
  assert.equal(
    publicCinemaImageUrl(
      {
        url: '/api/v1/theaters/majestic/EMS1299/image/1',
        width: 800,
        height: 600,
      },
      'https://api.example.test',
    ),
    'https://api.example.test/api/v1/theaters/majestic/EMS1299/image/1',
  )
  for (const path of [
    'majestic/../EMS1299',
    'majestic/EMS%313299',
    'majestic/EMS1299/image/1?x=1',
    'majestic//EMS1299',
  ])
    assert.equal(
      publicCinemaImageUrl(
        { url: `/api/v1/theaters/${path}/image/1`, width: 800, height: 600 },
        '',
      ),
      '',
    )
  assert.match(
    cinemaDescription({
      provider: 'majestic',
      name: theater.name,
      city: theater.city,
      availableDateCount: 1,
    }),
    /cinéma Majestic Cinémas/,
  )
  for (const page of [
    'sync',
    'sync-schedules',
    'theater-locations',
    'tmdb-matches',
  ]) {
    const value = await source(`pages/admin/${page}.vue`)
    assert.match(value, /majestic: 'Majestic Cinémas'/)
    if (page.startsWith('sync'))
      assert.match(
        value,
        /'veo',\s*'majestic',\s*'cineode',\s*'cinemovida',\s*'ocine',\s*\] as const/,
      )
  }
  assert.match(
    await source('pages/admin/sync-schedules.vue'),
    /majestic: selectLatestProviderRun\(\s*'majestic'/,
  )
  assert.match(
    await source('components/CinemaTheaterMap.client.vue'),
    /'majestic',\s*THEATER_PROVIDER_COLORS.majestic/,
  )
})
