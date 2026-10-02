import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test, { type TestContext } from 'node:test'
import {
  computed,
  effectScope,
  nextTick,
  ref,
  watch,
  type ComputedRef,
  type Ref,
} from 'vue'
import ts from 'typescript'
import type { PublicTheaterImage } from '../app/types/api.ts'
import { publicCinemaImageUrl } from '../app/utils/cinemaImage.ts'

const path = '/api/v1/theaters/ugc/25/image/1'
const photo = (url = path): PublicTheaterImage => ({
  url,
  width: 1200,
  height: 800,
})

test('public photo URLs use only the browser API base and normalized public metadata', () => {
  assert.equal(publicCinemaImageUrl(null, ''), '')
  assert.equal(publicCinemaImageUrl(photo(), ''), path)
  assert.equal(
    publicCinemaImageUrl(photo(), 'https://api.example.test/'),
    `https://api.example.test${path}`,
  )
  assert.equal(publicCinemaImageUrl(photo(), '/proxy'), `/proxy${path}`)
  for (const provider of [
    'ugc',
    'kinepolis',
    'pathe',
    'cgr',
    'megarama',
    'cineville',
    'mk2',
    'cinewest',
    'grandecran',
    'noecinemas',
  ]) {
    for (const id of [
      '0004',
      'webediamovies-W8400',
      'ABC_25',
      'x'.repeat(128),
    ]) {
      const url = `/api/v1/theaters/${provider}/${id}/image/${Number.MAX_SAFE_INTEGER}`
      assert.equal(publicCinemaImageUrl(photo(url), ''), url)
    }
  }
  for (const dimension of [1, 1600])
    assert.equal(
      publicCinemaImageUrl(
        { url: path, width: dimension, height: dimension },
        '',
      ),
      path,
    )
})

test('public photo URLs reject unsafe paths and noncanonical revisions', () => {
  for (const url of [
    'https://external.example.test' + path,
    '//external.example.test' + path,
    '/api/v1/admin/theaters/ugc/25/image/1',
    '/api/v1/theaters/unknown/25/image/1',
    '/api/v1/theaters/UGC/25/image/1',
    '/api/v1/theaters/ugc//image/1',
    `/api/v1/theaters/ugc/${'x'.repeat(129)}/image/1`,
    '/api/v1/theaters/ugc/../image/1',
    '/api/v1/theaters/ugc/%2e%2e/image/1',
    '/api/v1/theaters/ugc/25%2F26/image/1',
    '/api/v1/theaters/ugc/25%5c26/image/1',
    '/api/v1/theaters/ugc/25%252F26/image/1',
    '/api/v1/theaters/ugc/25\\26/image/1',
    `${path}?token=x`,
    `${path}#image`,
    `${path}/`,
    `${path}\n`,
    ` ${path}`,
    ...[
      '0',
      '-1',
      '+1',
      '01',
      '1.0',
      '1e3',
      '%31',
      '9007199254740992',
      '9'.repeat(400),
    ].map((revision) => `/api/v1/theaters/ugc/25/image/${revision}`),
  ])
    assert.equal(publicCinemaImageUrl(photo(url), ''), '', url)
})

test('public photo metadata rejects missing, fractional and out-of-range dimensions', () => {
  for (const invalid of [0, -1, 1601, 1.5, NaN, Infinity, undefined, '1200']) {
    for (const field of ['width', 'height']) {
      // Deliberately malformed wire metadata exercises runtime validation.
      const image = Object.assign(photo(), { [field]: invalid })
      assert.equal(publicCinemaImageUrl(image, ''), '')
    }
  }
  assert.equal(
    publicCinemaImageUrl(Object.assign(photo(), { url: null }), ''),
    '',
  )
})

const page = await readFile(
  new URL('../app/pages/cinema/[slug].vue', import.meta.url),
  'utf8',
)
const script = page.slice(
  page.indexOf('const cinemaImageUrl ='),
  page.indexOf('const canonicalUrl ='),
)
assert.ok(script.includes('onMounted(checkCinemaImage)'))
const compiled = ts.transpileModule(
  `${script}\nreturn { cinemaImageUrl, cinemaImage, failedCinemaImageUrl, hasCinemaImage, onCinemaImageError, checkCinemaImage }`,
  {
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
    },
  },
).outputText

interface TestImage {
  complete: boolean
  naturalWidth: number
  getAttribute: (name: string) => string | null
}
interface HeaderState {
  cinemaImageUrl: ComputedRef<string>
  cinemaImage: Ref<TestImage | null>
  failedCinemaImageUrl: Ref<string>
  hasCinemaImage: ComputedRef<boolean>
  onCinemaImageError: (event: { target: TestImage | null }) => void
  checkCinemaImage: () => void
}

// Execute the actual page's image state/functions with Vue reactivity, not a duplicate state machine.
function header(t: TestContext, image: PublicTheaterImage | null = photo()) {
  const response = ref({ theater: { image } })
  const mounted: (() => void)[] = []
  const scope = effectScope()
  t.after(() => scope.stop())
  const state: HeaderState = scope.run(() =>
    new Function(
      'computed',
      'ref',
      'watch',
      'onMounted',
      'response',
      'config',
      'publicCinemaImageUrl',
      compiled,
    )(
      computed,
      ref,
      watch,
      (callback: () => void) => mounted.push(callback),
      response,
      { apiBase: 'http://ssr-private.invalid', public: { apiBase: '' } },
      publicCinemaImageUrl,
    ),
  )
  return {
    ...state,
    response,
    mount: () => mounted.forEach((callback) => callback()),
  }
}
const element = (
  url: string,
  complete = true,
  naturalWidth = 1200,
): TestImage => ({
  complete,
  naturalWidth,
  getAttribute: (name) => (name === 'src' ? url : null),
})

test('null or invalid photos render fallback; loading reserves readable photo state without a readiness loader', (t) => {
  const state = header(t, null)
  assert.equal(state.hasCinemaImage.value, false)
  state.response.value.theater.image = photo()
  assert.equal(state.cinemaImageUrl.value, path)
  assert.equal(state.hasCinemaImage.value, true)
  state.cinemaImage.value = element(path, false, 0)
  state.mount()
  assert.equal(state.hasCinemaImage.value, true)
  state.response.value.theater.image = photo(
    '/api/v1/admin/theaters/ugc/25/image/1',
  )
  assert.equal(state.hasCinemaImage.value, false)
})

test('current image failure stays local and a new revision resets it; late old errors cannot hide replacement', async (t) => {
  const state = header(t)
  const old = element(path, true, 0)
  state.onCinemaImageError({ target: old })
  assert.equal(state.failedCinemaImageUrl.value, path)
  assert.equal(state.hasCinemaImage.value, false)
  state.response.value.theater.image = photo()
  assert.equal(state.hasCinemaImage.value, false)
  const replacement = `${path.slice(0, -1)}2`
  state.response.value.theater.image = photo(replacement)
  assert.equal(state.failedCinemaImageUrl.value, '')
  assert.equal(state.hasCinemaImage.value, true)
  state.onCinemaImageError({ target: old })
  state.cinemaImage.value = old
  await nextTick()
  assert.equal(state.hasCinemaImage.value, true)
  state.cinemaImage.value = element(replacement)
  state.mount()
  assert.equal(state.hasCinemaImage.value, true)
  state.onCinemaImageError({ target: element(replacement, true, 0) })
  assert.equal(state.hasCinemaImage.value, false)
  state.response.value.theater.image = null
  assert.equal(state.hasCinemaImage.value, false)
  state.response.value.theater.image = photo()
  assert.equal(state.hasCinemaImage.value, true)
})

test('mount detects a failed SSR image whose error preceded hydration, but not pending or successful images', (t) => {
  for (const [complete, naturalWidth, visible] of [
    [true, 0, false],
    [false, 0, true],
    [true, 1200, true],
  ] as const) {
    const state = header(t)
    state.cinemaImage.value = element(path, complete, naturalWidth)
    state.mount()
    assert.equal(state.hasCinemaImage.value, visible)
  }
})

test('a replaced element already complete with zero width also enters fallback', async (t) => {
  const state = header(t)
  const next = '/api/v1/theaters/mk2/0004/image/3'
  state.response.value.theater.image = photo(next)
  state.cinemaImage.value = element(next, true, 0)
  await nextTick()
  assert.equal(state.hasCinemaImage.value, false)
  assert.equal(state.failedCinemaImageUrl.value, next)
})

test('header source guards attached photo/fallback, decorative media, retained SEO and unchanged programme controls', () => {
  const headerMarkup = page.slice(
    page.indexOf('<header'),
    page.indexOf('</header>'),
  )
  assert.match(
    headerMarkup,
    /lg:grid-cols-\[minmax\(0,7fr\)_minmax\(17rem,3fr\)\]/,
  )
  for (const value of [
    'min-h-[240px]',
    'lg:min-h-[380px]',
    'items-end',
    'shadow-[8px_8px_0_#27272a]',
    'variant="hero"',
    "hasCinemaImage ? 'text-white' : 'text-ink'",
    'object-[center_40%]',
    'text-[2.125rem]',
    'lg:text-[3.75rem]',
    '-top-10',
    'rgba(39,39,42,0.82)_calc(100%_-_4rem)',
    'overflow-wrap:anywhere',
  ])
    assert.ok(headerMarkup.includes(value), value)
  assert.doesNotMatch(headerMarkup, /bg-ink\/60|sm:grid-cols-2/)
  const details = headerMarkup.slice(headerMarkup.indexOf('<dl'))
  assert.match(details, /flex flex-col gap-3[^"]*lg:gap-6/)
  assert.equal((details.match(/border-t-2/g) || []).length, 1)
  assert.equal((details.match(/border-l-2/g) || []).length, 1)
  assert.match(page, /<section\s+class="mt-8 lg:mt-12"/)
  assert.match(headerMarkup, /<img\s+v-if="hasCinemaImage"/)
  for (const value of [
    ':key="cinemaImageUrl"',
    'ref="cinemaImage"',
    ':width="response.theater.image?.width"',
    ':height="response.theater.image?.height"',
    'alt=""',
    'aria-hidden="true"',
    'loading="eager"',
    'fetchpriority="high"',
    'decoding="async"',
    'referrerpolicy="no-referrer"',
  ])
    assert.ok(headerMarkup.includes(value), value)
  assert.equal((headerMarkup.match(/<h1\b/g) || []).length, 1)
  assert.match(
    headerMarkup,
    /v-if="displayLocation.address \|\| displayLocation.locality"/,
  )
  assert.match(headerMarkup, /response.theater.available_dates.length/)
  assert.doesNotMatch(
    headerMarkup,
    /pageDescription|Statistiques|itinéraire|indisponible|rounded-|truncate|line-clamp/,
  )
  assert.match(page, /description: pageDescription.value/)
  assert.match(page, /description: pageDescription,/)
  assert.match(page, /config.public.apiBase/)
  assert.doesNotMatch(page, /config.apiBase/)
  for (const control of [
    '<ShareButton',
    '<ShowtimeDateBar',
    '<ResultSettingMenu',
    '<ShowtimeResults',
    '<MovieCatalogControls',
    '<CinemaActivity',
    "viewQuery('activity')",
    'mergeOwnedQuery',
    'router.replace',
    'router.push',
  ])
    assert.ok(page.includes(control), control)
})

test('hero city recases only uppercase source labels without changing accents, separators or mixed casing', () => {
  const cityScript = page.slice(
    page.indexOf('function formatCinemaCity('),
    page.indexOf('function normalizeLocationPart('),
  )
  const compiledCity = ts.transpileModule(
    `${cityScript}\nreturn formatCinemaCity`,
    {
      compilerOptions: { target: ts.ScriptTarget.ES2022 },
    },
  ).outputText
  const formatCity: (city: string) => string = new Function(compiledCity)()
  for (const [source, expected] of [
    ['LILLE', 'Lille'],
    ['ÉVRY-COURCOURONNES', 'Évry-Courcouronnes'],
    ["L'HAŸ-LES-ROSES", "L'Haÿ-Les-Roses"],
    ['SAINT-OUEN-L’AUMÔNE', 'Saint-Ouen-L’Aumône'],
    ["Villeneuve-d'Ascq", "Villeneuve-d'Ascq"],
    ['Paris 15e', 'Paris 15e'],
    ['Le Touquet-Paris-Plage', 'Le Touquet-Paris-Plage'],
    ['', ''],
  ] as const)
    assert.equal(formatCity(source), expected)
  assert.match(page, /formatCinemaCity\(response.theater.city\)/)
})
