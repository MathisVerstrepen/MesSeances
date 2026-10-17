import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import {
  computed,
  createSSRApp,
  h,
  nextTick,
  onMounted,
  ref,
  watch,
  type Component,
} from 'vue'
import ts from 'typescript'
import { safeBookingUrl } from '../app/utils/bookingUrl.ts'
import { safeBackdropUrl, safePosterUrl } from '../app/utils/safeImageUrl.ts'
import type { ShowtimeResultViewModel } from '../app/types/showtimeResults.ts'
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
  statisticsCount,
} from '../app/utils/statistics.ts'
import { selectLatestProviderRun } from '../app/utils/adminSyncSchedules.ts'
import { publicCinemaImageUrl } from '../app/utils/cinemaImage.ts'
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
        : id === '~/utils/safeImageUrl'
          ? { safeBackdropUrl, safePosterUrl }
          : id === '~/utils/statistics'
            ? { statisticsChainLabels, statisticsCount }
            : require(id)
  new Function(
    'require',
    'exports',
    'computed',
    'ref',
    'watch',
    'onMounted',
    'nextTick',
    compiled,
  )(localRequire, exports, computed, ref, watch, onMounted, nextTick)
  assert.ok(exports.default)
  return exports.default
}
const [BrandLogo, BrandedText, TheaterName, BookingLink, StatisticsChainTable] =
  await Promise.all(
    [
      'BrandLogo',
      'BrandedText',
      'TheaterName',
      'BookingLink',
      'StatisticsChainTable',
    ].map(component),
  )
async function render(
  root: Component,
  props: Parameters<typeof createSSRApp>[1],
) {
  const app = createSSRApp(root, props)
  app.component('BrandLogo', BrandLogo!)
  app.component('BrandedText', BrandedText!)
  app.component('TheaterName', TheaterName!)
  for (const name of [
    'PosterImage',
    'WatchedBadge',
    'ShowtimeResultLine',
    'ShowtimeResultBox',
  ])
    app.component(name, { render: () => h('span') })
  app.component('NuxtLink', {
    props: ['to'],
    setup:
      (props, { slots }) =>
      () =>
        h('a', { href: props.to }, slots.default?.()),
  })
  return renderToString(app)
}

test('Cinéode logo bytes, natural ratio, inline/display sizing and decorative/accessibility semantics stay intact', async () => {
  const asset = await readFile(
    new URL('../app/assets/brands/cineode_logo.svg', import.meta.url),
  )
  assert.equal(
    createHash('sha256').update(asset).digest('hex'),
    'a428df15cb7aa5ced90b3347e37138e202b1c253d8a6a502b0d0d4c9f90994d3',
  )
  assert.match(
    asset.toString(),
    /width="310" height="139" viewBox="0 0 310 139"/,
  )
  for (const variant of ['inline', 'display']) {
    const html = await render(BrandLogo!, { brand: 'Cinéode', variant })
    assert.match(html, /cineode_logo.svg\?no-inline/)
    assert.match(html, /alt="Cinéode"/)
    assert.match(
      html,
      variant === 'inline' ? /h-\[1.15em\] w-auto/ : /w-36 sm:w-40/,
    )
    assert.match(
      await render(BrandLogo!, { brand: 'Cinéode', variant, decorative: true }),
      /alt(?:="")? aria-hidden="true"/,
    )
  }
  assert.match(
    await source('pages/credits.vue'),
    /brand: 'Cinéode',\s*name: 'Cinéode',\s*url: 'https:\/\/www\.cineode\.fr\/'/,
  )
})

test('Cinéode whole-word accent folding preserves source names and speaks provider only once', async () => {
  for (const name of [
    'Cinéode',
    'Cineode',
    'CINÉODE',
    'Cinéode'.normalize('NFD'),
  ]) {
    const text = `${name} Le Zoetrope`
    const html = await render(BrandedText!, { text })
    assert.equal((html.match(/<img /g) || []).length, 1)
    assert.ok(html.includes(`class="sr-only">${text}`))
    for (const variant of ['inline', 'hero']) {
      const venue = await render(TheaterName!, {
        name: text,
        provider: 'cineode',
        variant,
      })
      assert.equal((venue.match(/<img /g) || []).length, 1)
      assert.match(venue, /alt(?:="")? aria-hidden="true"/)
      assert.ok(venue.includes(text))
    }
  }
  for (const name of [
    'Cinéodement',
    'éCinéode',
    'Cinéode2',
    'Cinéode_centre',
    'Le Don Camillo',
    'La Grange',
  ]) {
    assert.doesNotMatch(await render(BrandedText!, { text: name }), /<img /)
    for (const variant of ['inline', 'hero']) {
      const html = await render(TheaterName!, {
        name,
        provider: 'cineode',
        variant,
      })
      assert.equal((html.match(/<img /g) || []).length, 1)
      assert.match(html, /alt="Cinéode"/)
      assert.ok(html.includes(name))
    }
  }
})

test('Cinéode website-only BookingLink has truthful visible/spoken label, safe new tab and disabled invalid links', async () => {
  const props = {
    provider: 'cineode',
    showtimeId: 'cineode-showing-991-atelier-local-1792188900',
    theaterId: 'cineode-991',
    url: 'https://www.cineode.fr/le-don-camillo/seances/',
  }
  const html = await render(BookingLink!, {
    ...props,
    ariaLabel: 'Réserver maintenant',
  })
  assert.match(
    html,
    /href="https:\/\/www.cineode.fr\/le-don-camillo\/seances\/"/,
  )
  assert.match(html, /target="_blank" rel="noopener noreferrer"/)
  assert.match(
    html,
    /aria-label="Site du cinéma Cinéode, ouverture dans un nouvel onglet"/,
  )
  assert.match(html, /Site du cinéma /)
  assert.doesNotMatch(html, /Réserver/)
  for (const overrides of [
    { url: null },
    { url: '' },
    { url: props.url + '?x=1' },
    { url: 'https://www.cineode.fr/la-grange/seances/' },
    { theaterId: null },
    { showtimeId: null },
    { provider: 'veo' },
  ]) {
    const unavailable = await render(BookingLink!, { ...props, ...overrides })
    assert.doesNotMatch(unavailable, /href=/)
    assert.match(unavailable, /aria-disabled="true"/)
    assert.match(unavailable, /Réservation indisponible/)
  }
})

test('Cinéode empty venues remain in chain directory, maps, admin filters, statistics table and safe image routes', async () => {
  const theater: Theater = {
    provider: 'cineode',
    id: 'cineode-3955',
    slug: 'cineode-3955',
    name: 'La Grange',
    city: 'Vaux-le-Pénil',
    city_slug: 'vaux-le-penil',
    postal_code: '',
    address: '',
    available_dates: [],
    accepted_passes: [],
    latitude: 48.43,
    longitude: 2.68,
  }
  assert.equal(THEATER_PROVIDER_LABELS.cineode, 'Cinéode')
  assert.equal(THEATER_PROVIDER_COLORS.cineode, '#000000')
  assert.equal(cinemaProviderLabels.cineode, 'Cinéode')
  assert.equal(statisticsChainLabels.cineode, 'Cinéode')
  assert.deepEqual(cinemaDirectoryChains([theater]), ['cineode'])
  assert.deepEqual(normalizeCinemaChains(['CINEODE', 'cineode', 'unknown']), [
    'cineode',
  ])
  assert.deepEqual(toggleCinemaChain([], 'cineode'), ['cineode'])
  assert.deepEqual(filterCinemaDirectory([theater], 'vaux', ['cineode']), [
    theater,
  ])
  assert.deepEqual(filterCinemaDirectory([theater], '', ['ugc']), [])
  assert.equal(
    cinemaApiQuery(parseCinemaRoute({ provider: 'cineode' })).provider,
    'cineode',
  )
  assert.equal(
    parseStatisticsQuery({ chain: 'cineode' }).query.chain,
    'cineode',
  )
  assert.deepEqual(
    buildTheaterFeatureCollection([theater], new Set([theater.id])).features[0]!
      .properties,
    { id: theater.id, provider: 'cineode', favorite: true },
  )
  assert.equal(
    publicCinemaImageUrl(
      { url: '/api/v1/theaters/cineode/3955/image/1', width: 800, height: 600 },
      'https://api.example.test',
    ),
    'https://api.example.test/api/v1/theaters/cineode/3955/image/1',
  )
  for (const path of [
    'cineode/../3955',
    'cineode/%333955',
    'cineode//3955',
    'cineode/3955/image/1?x=1',
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
      provider: 'cineode',
      name: theater.name,
      city: theater.city,
      availableDateCount: 0,
    }),
    /cinéma Cinéode/,
  )
  const table = await render(StatisticsChainTable!, {
    rows: [
      {
        chain: 'cineode',
        showtime_count: 42,
        movie_count: 3,
        theater_count: 37,
      },
    ],
  })
  assert.match(table, /href="\/cinemas\?chains=cineode"/)
  assert.match(table, /cineode_logo/)
  assert.match(table, />42</)
  assert.match(table, />37</)
})

test('Cinéode schedule target availability and latest all-run status use existing provider selection flow', async () => {
  const page = await source('pages/admin/sync-schedules.vue')
  const { descriptor } = parse(page)
  const script = descriptor.scriptSetup!.content
  const declarations = [
    'providers',
    'targets',
    'targetLabels',
    'latestRuns',
  ].map((name) => {
    const declaration = script.match(
      new RegExp(`const ${name} = [\\s\\S]*?(?=\\n\\n|\\nconst )`),
    )?.[0]
    assert.ok(declaration, name)
    return declaration
  })
  const job = {
    id: 'cineode-run',
    target: 'all',
    state: 'succeeded',
    started_at: '2026-10-16T12:00:00Z',
    providers: Object.fromEntries(
      Object.keys(THEATER_PROVIDER_LABELS).map((provider) => [
        provider,
        { state: provider === 'cineode' ? 'succeeded' : 'not_requested' },
      ]),
    ),
  }
  const compiled = ts.transpileModule(declarations.join('\n'), {
    compilerOptions: { target: ts.ScriptTarget.ES2022 },
  }).outputText
  const result = runInNewContext(
    `${compiled}; ({targets, targetLabels, latest: latestRuns.value.cineode})`,
    {
      computed,
      selectLatestProviderRun,
      syncStatus: { value: { job, runs: [] } },
    },
  )
  assert.equal(
    result.targets.indexOf('cineode'),
    result.targets.indexOf('majestic') + 1,
  )
  assert.equal(
    result.targets.indexOf('tmdb_metadata_refresh'),
    result.targets.indexOf('cinemovida') + 1,
  )
  assert.equal(result.targetLabels.cineode, 'Cinéode')
  assert.equal(result.latest, job)
  for (const name of ['theater-locations', 'tmdb-matches'])
    assert.match(await source(`pages/admin/${name}.vue`), /cineode: 'Cinéode'/)
})

test('Cinéode grouped programme suppresses unknown runtime in single and multi-cinema headers', async () => {
  const group = await component('ShowtimeMovieGroup')
  const result: ShowtimeResultViewModel = {
    key: 'cineode:cineode-showing-991-atelier-local-1792188900',
    showtimeId: 'cineode-showing-991-atelier-local-1792188900',
    provider: 'cineode',
    movieKey: 'cineode:atelier-local',
    movieSlug: 'cineode-film-atelier-local',
    movieTitle: "L'été",
    movieOriginalLanguage: null,
    movieRuntimeMinutes: 0,
    theaterName: 'Le Don Camillo',
    theaterId: 'cineode-991',
    advertisedStartTime: '2026-10-17T00:15:00+02:00',
    effectiveStartTime: '2026-10-17T00:15:00+02:00',
    end: null,
    language: 'VOSTFR',
    format: '2D',
    room: '',
    bookingUrl: null,
    posterUrl: null,
    backdropUrl: null,
  }
  for (const scope of ['single-theater', 'multi-theater']) {
    const html = await render(group, {
      results: [result],
      layout: 'boxes',
      scope,
    })
    assert.doesNotMatch(html, />\s*0 min\s*</)
    assert.match(
      await render(group, {
        results: [{ ...result, movieRuntimeMinutes: 100 }],
        layout: 'boxes',
        scope,
      }),
      />\s*100 min\s*</,
    )
  }
})
