import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { computed, createSSRApp, h, type Component } from 'vue'
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
import { cinemovidaTheaters } from '../e2e/data.mjs'

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
        : id === '~/utils/statistics'
          ? { statisticsChainLabels, statisticsCount }
          : require(id)
  new Function('require', 'exports', 'computed', compiled)(
    localRequire,
    exports,
    computed,
  )
  return exports.default!
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
  app.component('NuxtLink', {
    props: ['to'],
    setup:
      (props, { slots }) =>
      () =>
        h('a', { href: props.to }, slots.default?.()),
  })
  return renderToString(app)
}

test('Ciné-Movida supplied square logo retains bytes, accessible names and contain sizing', async () => {
  const asset = await readFile(
    new URL('../app/assets/brands/cinemovida_logo.svg', import.meta.url),
  )
  assert.equal(
    createHash('sha256').update(asset).digest('hex'),
    'f0a18c0fc758f64caf6cd35a0602cd6b0dca52d3f5e979de2bbfd625e67e67d4',
  )
  assert.match(
    asset.toString(),
    /width="480" height="480" viewBox="0 0 480 480"/,
  )
  for (const variant of ['inline', 'display']) {
    const html = await render(BrandLogo!, { brand: 'Ciné-Movida', variant })
    assert.match(html, /cinemovida_logo.svg\?no-inline/)
    assert.match(html, /alt="Ciné-Movida"/)
    assert.match(html, /object-contain/)
    assert.match(
      html,
      variant === 'inline' ? /h-\[2em\] w-\[2em\]/ : /aspect-square w-20/,
    )
    assert.match(
      await render(BrandLogo!, {
        brand: 'Ciné-Movida',
        variant,
        decorative: true,
      }),
      /alt(?:="")? aria-hidden="true"/,
    )
  }
})

test('Ciné-Movida whole-word branding preserves cinema names without duplicate spoken brands', async () => {
  for (const name of [
    'Ciné-Movida',
    'CINE-MOVIDA',
    'Cine Movida',
    'Cinemovida',
    'Ciné-Movida'.normalize('NFD'),
  ]) {
    const text = `${name} Méga Castillet`
    assert.equal(
      ((await render(BrandedText!, { text })).match(/<img /g) || []).length,
      1,
    )
    for (const variant of ['inline', 'hero']) {
      const html = await render(TheaterName!, {
        name: text,
        provider: 'cinemovida',
        variant,
      })
      assert.match(html, /alt(?:="")? aria-hidden="true"/)
      assert.ok(html.includes(text))
    }
  }
  for (const name of [
    'éCiné-Movida',
    'Ciné-Movida2',
    'Ciné-Movida_centre',
    'Ciné-Movidament',
    'Le Sélect',
  ]) {
    assert.doesNotMatch(await render(BrandedText!, { text: name }), /<img /)
    assert.match(
      await render(TheaterName!, { name, provider: 'cinemovida' }),
      /alt="Ciné-Movida"/,
    )
  }
})

test('Ciné-Movida booking link stays safe, labeled and unavailable for invalid context', async () => {
  for (const props of [
    {
      provider: 'cinemovida',
      showtimeId: `cinemovida-showing-P0176-${'ab'.repeat(32)}`,
      theaterId: 'cinemovida-P0176',
      url: 'https://achat.cinema-castillet.com/reserver/r/123',
    },
    {
      provider: 'cinemovida',
      showtimeId: `cinemovida-showing-P0876-${'ab'.repeat(32)}`,
      theaterId: 'cinemovida-P0876',
      url: 'https://achat.concordecinema.com/r/123',
    },
  ]) {
    const html = await render(BookingLink!, props)
    assert.ok(html.includes(`href="${props.url}"`))
    assert.match(html, /target="_blank" rel="noopener noreferrer"/)
    assert.match(
      html,
      /aria-label="Réserver sur Ciné-Movida, ouverture dans un nouvel onglet"/,
    )
    assert.match(html, /Réserver sur /)
    assert.match(html, /cinemovida_logo/)
    for (const overrides of [
      { url: null },
      { url: props.url + '\n' },
      { theaterId: 'cinemovida-P0218' },
      { showtimeId: null },
      { provider: 'veo' },
    ]) {
      const disabled = await render(BookingLink!, { ...props, ...overrides })
      assert.doesNotMatch(disabled, /href=/)
      assert.match(disabled, /aria-disabled="true"/)
      assert.match(disabled, /Réservation indisponible/)
    }
  }
})

test('Ciné-Movida empty venue participates in directory, maps, images, admin and statistics', async () => {
  const theater = { ...cinemovidaTheaters[3], latitude: 44.1, longitude: 1.08 }
  assert.equal(THEATER_PROVIDER_LABELS.cinemovida, 'Ciné-Movida')
  assert.equal(THEATER_PROVIDER_COLORS.cinemovida, '#cb1730')
  assert.equal(cinemaProviderLabels.cinemovida, 'Ciné-Movida')
  assert.equal(statisticsChainLabels.cinemovida, 'Ciné-Movida')
  assert.deepEqual(cinemaDirectoryChains([theater]), ['cinemovida'])
  assert.deepEqual(normalizeCinemaChains(['CINEMOVIDA', 'cinemovida']), [
    'cinemovida',
  ])
  assert.deepEqual(
    filterCinemaDirectory([theater], 'moissac', ['cinemovida']),
    [theater],
  )
  assert.equal(
    cinemaApiQuery(parseCinemaRoute({ provider: 'cinemovida' })).provider,
    'cinemovida',
  )
  assert.equal(
    parseStatisticsQuery({ chain: 'cinemovida' }).query.chain,
    'cinemovida',
  )
  assert.equal(
    buildTheaterFeatureCollection([theater], new Set([theater.id])).features[0]!
      .properties.provider,
    'cinemovida',
  )
  assert.equal(
    publicCinemaImageUrl(
      {
        url: '/api/v1/theaters/cinemovida/P0876/image/1',
        width: 800,
        height: 600,
      },
      '',
    ),
    '/api/v1/theaters/cinemovida/P0876/image/1',
  )
  assert.equal(
    publicCinemaImageUrl(
      {
        url: '/api/v1/theaters/cinemovida/../P0876/image/1',
        width: 800,
        height: 600,
      },
      '',
    ),
    '',
  )
  assert.match(
    cinemaDescription({
      provider: 'cinemovida',
      name: theater.name,
      city: theater.city,
      availableDateCount: 0,
    }),
    /cinéma Ciné-Movida/,
  )
  const table = await render(StatisticsChainTable!, {
    rows: [
      {
        chain: 'cinemovida',
        showtime_count: 3,
        movie_count: 2,
        theater_count: 4,
      },
    ],
  })
  assert.match(table, /href="\/cinemas\?chains=cinemovida"/)
  assert.match(table, /cinemovida_logo/)
  assert.match(
    await source('components/CinemaTheaterMap.client.vue'),
    /'cinemovida',\s*THEATER_PROVIDER_COLORS.cinemovida/,
  )
})

test('Ciné-Movida manual/scheduled targets and latest all-run exposure follow existing admin flow', async () => {
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
    assert.ok(declaration)
    return declaration
  })
  const job = {
    id: 'cinemovida-run',
    target: 'all',
    state: 'succeeded',
    started_at: '2026-10-16T12:00:00Z',
    providers: Object.fromEntries(
      Object.keys(THEATER_PROVIDER_LABELS).map((provider) => [
        provider,
        { state: provider === 'cinemovida' ? 'succeeded' : 'not_requested' },
      ]),
    ),
  }
  const compiled = ts.transpileModule(declarations.join('\n'), {
    compilerOptions: { target: ts.ScriptTarget.ES2022 },
  }).outputText
  const result = runInNewContext(
    `${compiled}; ({targets, targetLabels, latest: latestRuns.value.cinemovida})`,
    {
      computed,
      selectLatestProviderRun,
      syncStatus: { value: { job, runs: [] } },
    },
  )
  assert.equal(
    result.targets.indexOf('cinemovida'),
    result.targets.indexOf('cineode') + 1,
  )
  assert.equal(
    result.targets.indexOf('tmdb_metadata_refresh'),
    result.targets.indexOf('ocine') + 1,
  )
  assert.equal(result.targetLabels.cinemovida, 'Ciné-Movida')
  assert.equal(result.latest, job)
  for (const page of ['sync', 'theater-locations', 'tmdb-matches'])
    assert.match(
      await source(`pages/admin/${page}.vue`),
      /cinemovida: 'Ciné-Movida'/,
    )
  assert.match(
    await source('pages/admin/sync.vue'),
    /'cineode',\s*'cinemovida',/,
  )
})
