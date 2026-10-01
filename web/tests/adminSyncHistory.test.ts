import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import { compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import * as vue from 'vue'
import type {
  AdminSyncJob,
  AdminSyncProviderStatus,
  Provider,
} from '../app/types/api.ts'

const source = await readFile(
  new URL('../app/pages/admin/sync.vue', import.meta.url),
  'utf8',
)
const { descriptor } = parse(source)
const script = descriptor.scriptSetup!.content
const providerList = script.match(
  /const providers = \[[\s\S]*?\] as const/,
)?.[0]
const requestedProviders = script.match(
  /function requestedProviders\([\s\S]*?\n\}/,
)?.[0]
const formatNewShowtimes = script.match(
  /function formatNewShowtimes\([\s\S]*?\n\}/,
)?.[0]
assert.ok(providerList && requestedProviders && formatNewShowtimes)
const compiled = ts.transpileModule(
  [providerList, requestedProviders, formatNewShowtimes].join('\n'),
  { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
).outputText

function run(
  statuses: Partial<Record<Provider, AdminSyncProviderStatus>>,
  state: AdminSyncJob['state'] = 'succeeded',
): AdminSyncJob {
  return {
    id: 'history-run',
    target: 'all',
    state,
    trigger: 'manual',
    started_at: '2026-09-29T10:00:00Z',
    finished_at: '2026-09-29T10:01:00Z',
    from: '2026-09-29',
    through: '2027-07-06',
    providers: {
      ugc: { state: 'not_requested' },
      kinepolis: { state: 'not_requested' },
      pathe: { state: 'not_requested' },
      cgr: { state: 'not_requested' },
      megarama: { state: 'not_requested' },
      cineville: { state: 'not_requested' },
      mk2: { state: 'not_requested' },
      cinewest: { state: 'not_requested' },
      grandecran: { state: 'not_requested' },
      noecinemas: { state: 'not_requested' },
      ...statuses,
    },
  }
}

function published(newShowtimes: number): AdminSyncProviderStatus {
  return {
    state: 'succeeded',
    outcome: {
      sync: {
        version: 1,
        cinemas: 10,
        movies: 20,
        new_movies: 3,
        showtimes: 50000,
        new_showtimes: newShowtimes,
        generated_at: '2026-09-29T10:00:00Z',
      },
      enrichment: { status: 'complete' },
    },
  }
}

function summary(job: AdminSyncJob): string {
  return runInNewContext(`${compiled}; formatNewShowtimes(run)`, { run: job })
}

test('single-provider history shows new showtimes, ungrouped and spaced after plus', () => {
  const job = run({ ugc: published(8395) })
  job.target = 'ugc'
  assert.equal(summary(job), '+ 8395 séances')
})

test('multi-provider history sums requested outcomes, not total showtimes or unrequested providers', () => {
  assert.equal(
    summary(
      run({
        ugc: published(8395),
        kinepolis: published(605),
        noecinemas: published(2),
        pathe: { ...published(999), state: 'not_requested' },
      }),
    ),
    '+ 9002 séances',
  )
})

test('zero is plural and one is singular', () => {
  assert.equal(summary(run({ ugc: published(0) })), '+ 0 séances')
  assert.equal(summary(run({ ugc: published(1) })), '+ 1 séance')
})

test('missing, failed and skipped outcomes contribute zero', () => {
  assert.equal(
    summary(
      run(
        {
          ugc: { state: 'failed', error_code: 'provider_sync_failed' },
          kinepolis: { state: 'skipped' },
          pathe: { state: 'succeeded' },
        },
        'failed',
      ),
    ),
    '+ 0 séances',
  )
})

test('failed multi-provider run keeps successful published counts', () => {
  assert.equal(
    summary(
      run(
        {
          ugc: published(12),
          kinepolis: published(8),
          pathe: { state: 'failed', error_code: 'replacement_failed' },
          cgr: { state: 'skipped' },
        },
        'failed',
      ),
    ),
    '+ 20 séances',
  )
})

test('history summary replaces date range while active range and expanded metrics remain', () => {
  const template = descriptor.template!.content
  const historySummary = template.match(/<summary\b[\s\S]*?<\/summary>/)?.[0]
  assert.ok(historySummary)
  assert.match(historySummary, /\{\{ formatNewShowtimes\(run\) \}\}/)
  assert.doesNotMatch(historySummary, /run\.(?:from|through)/)
  assert.match(
    template,
    /Du \{\{ activeJob\.from \}\} au \{\{ activeJob\.through \}\}/,
  )
  assert.match(
    template,
    /\{\{ run\.providers\[provider\]\.outcome\.sync\.new_showtimes \}\}/,
  )
  assert.match(
    template,
    /\{\{ run\.providers\[provider\]\.outcome\.sync\.showtimes \}\}/,
  )
})

const uiConstants = [
  'providers',
  'targets',
  'targetLabels',
  'providerBrands',
  'stateLabels',
  'triggerLabels',
]
const uiScript = uiConstants.map((name) => {
  const declaration = script.match(
    new RegExp(`const ${name} = [\\s\\S]*?(?=\\n\\n|\\nconst )`),
  )?.[0]
  assert.ok(declaration, `${name} declaration exists`)
  return declaration
})
const uiBindings = runInNewContext(
  `${ts.transpileModule(uiScript.join('\n'), {}).outputText}; ({${uiConstants.join(',')}})`,
)
const renderedTemplate = compileTemplate({
  source: descriptor.template!.content,
  filename: 'sync.vue',
  id: 'admin-sync-test',
})
assert.deepEqual(renderedTemplate.errors, [])
const templateRender = runInNewContext(
  `${
    ts.transpileModule(renderedTemplate.code, {
      compilerOptions: { module: ts.ModuleKind.CommonJS },
    }).outputText
  }; exports.render`,
  { exports: {}, require: () => vue },
)
assert.ok(templateRender)

async function renderPage(
  history: AdminSyncJob[] = [],
  startingTarget: Provider | 'all' | null = null,
) {
  const app = vue.createSSRApp({
    render: templateRender,
    setup: () => ({
      ...uiBindings,
      errorMessage: '',
      initialPending: false,
      activeJob: null,
      controlsDisabled: startingTarget !== null,
      startingTarget,
      history,
      requestedProviders: () => [],
      formatDateTime: () => '1 oct. 2026, 14:00',
      formatDuration: () => '1 min 0 s',
      formatNewShowtimes: summary,
      startSync: () => {},
      Check: {
        render: () => vue.h('span', { 'data-component': 'Check' }),
      },
      X: {
        render: () => vue.h('span', { 'data-component': 'X' }),
      },
    }),
  })
  app.component('NuxtLink', {
    setup:
      (_, { slots }) =>
      () =>
        vue.h('a', slots.default?.()),
  })
  app.component('BrandLogo', {
    props: { brand: String, decorative: Boolean, variant: String },
    setup: (props) => () =>
      vue.h('img', {
        'data-brand': props.brand,
        alt: props.decorative ? '' : props.brand,
        'aria-hidden': props.decorative ? 'true' : undefined,
      }),
  })
  for (const name of [
    'AlertTriangle',
    'ArrowLeft',
    'CalendarClock',
    'Check',
    'X',
    'ChevronDown',
    'RefreshCw',
    'LoaderCircle',
    'EditorialStatePanel',
  ]) {
    app.component(name, {
      setup:
        (_, { slots }) =>
        () =>
          vue.h('span', { 'data-component': name }, slots.default?.()),
    })
  }
  return renderToString(app)
}

test('idle launch buttons use all ten decorative chain logos with visible names; Tous keeps refresh', async () => {
  const html = await renderPage()
  const buttons = [...html.matchAll(/<button\b[\s\S]*?<\/button>/g)].map(
    (match) => match[0],
  )
  assert.equal(buttons.length, 11)
  assert.match(buttons[0]!, /data-component="RefreshCw"/)
  assert.match(buttons[0]!, />\s*Tous</)
  assert.doesNotMatch(buttons[0]!, /data-brand/)
  assert.doesNotMatch(html, /Tous les cinémas/)
  const expectedBrands = [
    'UGC',
    'KINEPOLIS',
    'PATHE',
    'CGR',
    'MEGARAMA',
    'CINEVILLE',
    'MK2',
    'CINEWEST',
    'Grand Ecran',
    'Noé Cinémas',
  ]
  uiBindings.providers.forEach((provider: Provider, index: number) => {
    const button = buttons[index + 1]!
    assert.match(button, new RegExp(`data-brand="${expectedBrands[index]}"`))
    assert.match(button, /alt(?:="")? aria-hidden="true"/)
    assert.ok(button.includes(uiBindings.targetLabels[provider]))
    assert.doesNotMatch(button, /data-component="RefreshCw"/)
    assert.match(button, /bg-surface/)
  })
})

test('pending launch preserves loader, label and disabled controls', async () => {
  for (const target of ['ugc', 'all'] as const) {
    const html = await renderPage([], target)
    const buttons = [...html.matchAll(/<button\b[\s\S]*?<\/button>/g)].map(
      (match) => match[0],
    )
    const button = buttons[target === 'all' ? 0 : 1]!
    assert.match(button, /data-component="LoaderCircle"/)
    assert.ok(button.includes(uiBindings.targetLabels[target]))
    assert.doesNotMatch(button, /data-brand|data-component="RefreshCw"/)
    assert.ok(buttons.every((entry) => /disabled/.test(entry)))
  }
})

test('history places decorative logo between success/failure status and text; all has none', async () => {
  const success = { ...run({ ugc: published(8395) }), target: 'ugc' as const }
  const failure = {
    ...run({ pathe: { state: 'failed' } }, 'failed'),
    target: 'pathe' as const,
  }
  const html = await renderPage([success, failure, run({})])
  const summaries = [...html.matchAll(/<summary\b[\s\S]*?<\/summary>/g)].map(
    (match) => match[0],
  )
  for (const [index, brand, icon] of [
    [0, 'UGC', 'Check'],
    [1, 'PATHE', 'X'],
  ] as const) {
    assert.match(summaries[index]!, new RegExp(`data-brand="${brand}"`))
    assert.match(summaries[index]!, /alt(?:="")? aria-hidden="true"/)
    assert.ok(
      summaries[index]!.indexOf(`data-component="${icon}"`) <
        summaries[index]!.indexOf('sync-history-logo'),
    )
    assert.ok(
      summaries[index]!.indexOf('sync-history-logo') <
        summaries[index]!.indexOf('min-w-0 flex-1'),
    )
  }
  assert.match(summaries[0]!, /\+ 8395 séances/)
  assert.match(summaries[1]!, /Échec/)
  assert.doesNotMatch(summaries[2]!, /data-brand|sync-history-logo/)
  assert.match(summaries[2]!, />Tous</)
  assert.match(summaries[0]!, /sync-history-logo h-12! w-12!/)
  assert.equal(descriptor.styles.length, 0)
})
