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
import { ocineShowingParts, ocineSlugs } from '../app/utils/ocine.ts'
import {
  parseShowtimeSelection,
  serializeShowtimeSelection,
  toTheaterShowtimeResults,
} from '../app/utils/showtimeResults.ts'
import { ocineTheaters, ocineShowtimes } from '../e2e/data.mjs'

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
  assert.equal(THEATER_PROVIDER_COLORS.cineode, '#06b6d4')
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
    result.targets.indexOf('ocine') + 1,
  )
  assert.equal(result.targetLabels.cineode, 'Cinéode')
  assert.equal(result.latest, job)
  for (const name of ['theater-locations', 'tmdb-matches'])
    assert.match(await source(`pages/admin/${name}.vue`), /cineode: 'Cinéode'/)
})

test('Ociné supplied safe SVG preserves artwork, ratio, sizing and one accessible brand announcement', async () => {
  const asset = await readFile(
    new URL('../app/assets/brands/ocine_logo.svg', import.meta.url),
  )
  assert.equal(
    createHash('sha256').update(asset).digest('hex'),
    '6d6e4ac875fd8828184731f3cd62d29620ac459d62927e0aec16a2658312cc22',
  )
  assert.match(
    asset.toString(),
    /width="369" height="261" viewBox="0 0 369 261"/,
  )
  assert.doesNotMatch(
    asset.toString(),
    /<script|<foreignObject|\bon\w+=|href="(?!#)|url\((?!#)/i,
  )
  for (const variant of ['inline', 'display']) {
    const html = await render(BrandLogo!, { brand: 'Ociné', variant })
    assert.match(html, /ocine_logo.svg\?no-inline/)
    assert.match(html, /alt="Ociné"/)
    assert.match(html, /object-contain/)
    assert.match(
      html,
      variant === 'inline' ? /h-\[1.6em\] w-auto/ : /w-24 sm:w-28/,
    )
  }
  for (const brand of ['Ociné', 'Ocine', 'OCINÉ', 'Ociné'.normalize('NFD')]) {
    const name = `${brand} Maubeuge`
    const html = await render(BrandedText!, { text: name })
    assert.equal((html.match(/<img /g) || []).length, 1)
    assert.ok(html.includes(`class="sr-only">${name}`))
    for (const variant of ['inline', 'hero']) {
      const html = await render(TheaterName!, {
        name,
        provider: 'ocine',
        variant,
      })
      assert.equal((html.match(/<img /g) || []).length, 1)
      assert.match(html, /alt(?:="")? aria-hidden="true"/)
      assert.ok(html.includes(name))
    }
  }
  for (const text of ['Ocinément', 'éOciné', 'Ocine2', 'Ocine_centre'])
    assert.doesNotMatch(await render(BrandedText!, { text }), /<img /)
  assert.match(
    await source('pages/credits.vue'),
    /brand: 'Ociné',\s*name: 'Ociné',\s*url: 'https:\/\/www\.ocine\.fr'/,
  )
})

test('Ociné checkout binds original URL bytes to manifest cinema, film, epoch and repeated hall', async () => {
  for (const [cinema, slug] of Object.entries(ocineSlugs)) {
    for (const film of ['631242', '636416', 'atelier-local', 'cars-2006']) {
      const id = `ocine-showing-${cinema}-${film}-1792238400-9586`
      const numericFilm = /^\d+$/.test(film) ? film : '636416'
      const url = `https://www.ocine.fr/${slug}/reserver/F${numericFilm}/D1792238400/VO/9586/`
      const expected = { provider: 'ocine', url, kind: 'booking' }
      assert.deepEqual(
        safeBookingUrl(url, 'ocine', id, `ocine-${cinema}`),
        expected,
      )
      assert.deepEqual(
        safeBookingUrl(url, undefined, id, `ocine-${cinema}`),
        expected,
      )
      for (const bad of [
        url + '?x=1',
        url + '#x',
        url + '\n',
        ' ' + url,
        url.replace('https:', 'http:'),
        url.replace('www.ocine.fr', 'ocine.fr'),
        url.replace('www.ocine.fr', 'WWW.ocine.fr'),
        url.replace('www.ocine.fr', 'www.ocine.fr:443'),
        url.replace('www.ocine.fr', 'user@www.ocine.fr'),
        url.replace('www.ocine.fr', 'www.ocine.fr.evil.test'),
        url.replace(`/${slug}/`, `/other/../${slug}/`),
        url.replace('/reserver/', '//reserver/'),
        url.replace('/reserver/', '/%72eserver/'),
        url.replace('D1792238400', 'D1792238401'),
        url.replace('/9586/', '/9587/'),
        url.replace('/VO/', '/VOSTFR/'),
        url.replace('/F', '/F0'),
        'https://www.ugc.fr/',
        'https://www.ocine.fr/' + slug + '/seances/',
      ])
        assert.equal(
          safeBookingUrl(bad, 'ocine', id, `ocine-${cinema}`),
          null,
          bad,
        )
      for (const [showing, theater] of [
        [id, null],
        [null, `ocine-${cinema}`],
        [id, 'ocine-999'],
        [id.replace('-9586', '-past'), `ocine-${cinema}`],
        [id.replace('1792238400', '0'), `ocine-${cinema}`],
      ])
        assert.equal(safeBookingUrl(url, 'ocine', showing, theater), null)
      if (/^\d+$/.test(film))
        assert.equal(
          safeBookingUrl(
            url.replace(`/F${film}/`, '/F123/'),
            'ocine',
            id,
            `ocine-${cinema}`,
          ),
          null,
        )
      assert.equal(safeBookingUrl(url, 'cineode', id, `ocine-${cinema}`), null)
    }
  }
  const props = {
    provider: 'ocine',
    theaterId: 'ocine-309',
    showtimeId: 'ocine-showing-309-631242-1792238400-9586',
    url: 'https://www.ocine.fr/maubeuge/reserver/F631242/D1792238400/VF/9586/',
  }
  const html = await render(BookingLink!, props)
  assert.match(
    html,
    /aria-label="Réserver sur Ociné, ouverture dans un nouvel onglet"/,
  )
  assert.match(html, /target="_blank" rel="noopener noreferrer"/)
  const unavailable = await render(BookingLink!, {
    ...props,
    url: '',
    showtimeId: props.showtimeId.replace('-9586', '-past'),
  })
  assert.doesNotMatch(unavailable, /href=/)
  assert.match(unavailable, /aria-disabled="true"/)
  assert.match(unavailable, /Réservation indisponible/)
})

test('Ociné compact selections round-trip full composite identities without accepting malformed or oversized tokens', () => {
  const parts = [
    '309-631242-1792238400-9586',
    '309-636416-1792238400-9586',
    '310-cars-2006-1792238400-9586',
    '461-atelier-local-1792238400-past',
  ]
  const keys = parts.map((id) => `ocine:ocine-showing-${id}`).sort()
  assert.equal(
    serializeShowtimeSelection(keys),
    parts
      .sort()
      .map((id) => `q${id}`)
      .join(','),
  )
  assert.deepEqual(
    parseShowtimeSelection(serializeShowtimeSelection(keys)),
    keys,
  )
  for (const bad of [
    '999-631242-1792238400-9586',
    '309-01-1792238400-9586',
    '309-film--local-1792238400-9586',
    '309-film-0-9586',
    '309-film-9223372036854775808-9586',
    '309-film-1792238400-0',
    '309-film-1792238400-9586\n',
    `309-${'x'.repeat(120)}-1792238400-9586`,
  ]) {
    assert.equal(ocineShowingParts(bad), null, bad)
    assert.deepEqual(parseShowtimeSelection(`q${bad}`), [])
    assert.equal(
      serializeShowtimeSelection([`ocine:ocine-showing-${bad}`]),
      undefined,
    )
  }
})

test('Ociné all identities flow through provider maps, directory, URL filters, images and canonical timing', async () => {
  assert.equal(THEATER_PROVIDER_LABELS.ocine, 'Ociné')
  assert.equal(THEATER_PROVIDER_COLORS.ocine, '#a5b4fc')
  assert.equal(cinemaProviderLabels.ocine, 'Ociné')
  assert.equal(statisticsChainLabels.ocine, 'Ociné')
  assert.deepEqual(normalizeCinemaChains(['OCINE', 'unknown', 'ocine']), [
    'ocine',
  ])
  assert.deepEqual(toggleCinemaChain([], 'ocine'), ['ocine'])
  assert.equal(
    cinemaApiQuery(parseCinemaRoute({ provider: 'ocine' })).provider,
    'ocine',
  )
  assert.equal(parseStatisticsQuery({ chain: 'ocine' }).query.chain, 'ocine')
  for (const [day, offset] of [
    ['2026-01-15', '+01:00'],
    ['2026-07-15', '+02:00'],
  ]) {
    const session = ocineShowtimes(ocineTheaters[0], day).showtimes[0]
    assert.equal(session.start_time, `${day}T14:00:00${offset}`)
    assert.equal(session.end_time, `${day}T15:45:00${offset}`)
  }
  for (const venue of ocineTheaters) {
    assert.deepEqual(cinemaDirectoryChains([venue]), ['ocine'])
    assert.deepEqual(filterCinemaDirectory([venue], '', ['ocine']), [venue])
    assert.deepEqual(filterCinemaDirectory([venue], '', ['ugc']), [])
    assert.equal(
      buildTheaterFeatureCollection(
        [{ ...venue, latitude: 50.2, longitude: 3.9 }],
        new Set(),
      ).features[0]!.properties.provider,
      'ocine',
    )
    assert.match(
      cinemaDescription({
        provider: 'ocine',
        name: venue.name,
        city: venue.city,
        availableDateCount: 2,
      }),
      /Ociné/,
    )
    const results = toTheaterShowtimeResults(ocineShowtimes(venue))
    assert.equal(results[0]!.end?.estimated, false)
    assert.equal(results[1]!.end?.estimated, true)
    assert.equal(results[2]!.end, null)
    assert.equal(results[3]!.end, null)
    assert.equal(results[3]!.language, '')
    assert.equal(results[3]!.room, '')
    assert.equal(results[3]!.movieRuntimeMinutes, 0)
    assert.equal(results[3]!.posterUrl, null)
  }
  assert.equal(
    publicCinemaImageUrl(
      { url: '/api/v1/theaters/ocine/309/image/1', width: 800, height: 600 },
      '',
    ),
    '/api/v1/theaters/ocine/309/image/1',
  )
  for (const url of [
    '/api/v1/theaters/ocine/../309/image/1',
    '/api/v1/theaters/ocine/%33309/image/1',
    '/api/v1/theaters/ocine/309/image/1?x=1',
    '/api/v1/theaters/ocine/309/anything',
  ])
    assert.equal(publicCinemaImageUrl({ url, width: 800, height: 600 }, ''), '')
})

test('Ociné admin ordering, latest all-run status and public statistics use coherent provider admission', async () => {
  const { descriptor } = parse(await source('pages/admin/sync-schedules.vue'))
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
    id: 'ocine-run',
    target: 'all',
    state: 'succeeded',
    started_at: '2026-10-17T12:00:00Z',
    providers: Object.fromEntries(
      Object.keys(THEATER_PROVIDER_LABELS).map((provider) => [
        provider,
        { state: provider === 'ocine' ? 'succeeded' : 'not_requested' },
      ]),
    ),
  }
  const compiled = ts.transpileModule(declarations.join('\n'), {
    compilerOptions: { target: ts.ScriptTarget.ES2022 },
  }).outputText
  const result = runInNewContext(
    `${compiled}; ({targets, targetLabels, latest: latestRuns.value.ocine})`,
    {
      computed,
      selectLatestProviderRun,
      syncStatus: { value: { job, runs: [] } },
    },
  )
  assert.equal(
    result.targets.indexOf('ocine'),
    result.targets.indexOf('cinemovida') + 1,
  )
  assert.equal(
    result.targets.indexOf('tmdb_metadata_refresh'),
    result.targets.indexOf('ocine') + 1,
  )
  assert.equal(result.targetLabels.ocine, 'Ociné')
  assert.equal(result.latest, job)
  for (const name of ['sync', 'theater-locations', 'tmdb-matches'])
    assert.match(await source(`pages/admin/${name}.vue`), /ocine: 'Ociné'/)
  assert.match(
    await source('components/CinemaTheaterMap.client.vue'),
    /'ocine',\s*THEATER_PROVIDER_COLORS.ocine/,
  )
  const table = await render(StatisticsChainTable!, {
    rows: [
      { chain: 'ocine', showtime_count: 4, movie_count: 2, theater_count: 3 },
    ],
  })
  assert.match(table, /href="\/cinemas\?chains=ocine"/)
  assert.match(table, /ocine_logo/)
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
