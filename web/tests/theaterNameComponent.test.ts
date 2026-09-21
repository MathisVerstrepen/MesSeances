import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { computed, createSSRApp, type Component } from 'vue'
import ts from 'typescript'
import type { Provider } from '../app/types/api.ts'

const require = createRequire(import.meta.url)
const source = (path: string) =>
  readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')
interface ComponentModule {
  default?: Component
}

// Render the actual SFCs with Nuxt's computed auto-import and bundled asset imports supplied locally.
async function component(name: string): Promise<Component> {
  const { descriptor } = parse(await source(`components/${name}.vue`))
  const script = compileScript(descriptor, { id: name, inlineTemplate: true })
  const compiled = ts.transpileModule(script.content, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText
  const exports: ComponentModule = {}
  const localRequire = (id: string) =>
    id.startsWith('~/assets/') ? { __esModule: true, default: id } : require(id)
  new Function('require', 'exports', 'computed', compiled)(
    localRequire,
    exports,
    computed,
  )
  assert.ok(exports.default)
  return exports.default
}

const [TheaterName, BrandLogo, BrandedText] = await Promise.all(
  ['TheaterName', 'BrandLogo', 'BrandedText'].map(component),
)

async function render(
  name: string,
  provider: Provider,
  options: { decorative?: boolean; logoClass?: string } = {},
) {
  const app = createSSRApp(TheaterName!, { name, provider, ...options })
  app.component('BrandLogo', BrandLogo!)
  app.component('BrandedText', BrandedText!)
  return renderToString(app)
}

test('Cinewest theater names get one provider logo and preserve actual source names', async () => {
  for (const name of [
    'Cinéma GALAXY',
    'Étoile Cinémas',
    'Les Toiles du Moun',
    'Cinéma Liberté',
    'Capitole Studios',
  ]) {
    const html = await render(name, 'cinewest')
    assert.equal((html.match(/<img /g) || []).length, 1)
    assert.match(html, /cinewest_logo_small\.webp/)
    assert.match(html, /alt="Cinewest"/)
    assert.ok(html.includes(` ${name}</span>`), html)
    assert.doesNotMatch(html, /class="sr-only"/)
  }
})

test('Cinewest already in the name keeps its text without duplicate logos or accessible brand names', async () => {
  for (const name of [
    'Cinewest Le Lido',
    'Cinéma Cinewest',
    'CINEWEST GALAXY',
  ]) {
    const html = await render(name, 'cinewest')
    assert.equal((html.match(/<img /g) || []).length, 1)
    assert.match(html, /alt(?:="")? aria-hidden="true"/)
    assert.ok(html.includes(` ${name}</span>`))
    assert.doesNotMatch(html, /alt="Cinewest"/)
  }
  const decorative = await render('Cinéma GALAXY', 'cinewest', {
    decorative: true,
  })
  assert.match(decorative, /^<span aria-hidden="true">/)
  assert.match(decorative, /alt(?:="")? aria-hidden="true"/)
})

const providers = {
  ugc: 'UGC',
  cgr: 'CGR Cinémas',
  kinepolis: 'Kinepolis',
  pathe: 'Pathé',
  megarama: 'Megarama',
  cineville: 'Cinéville',
  mk2: 'MK2',
  cinewest: 'Cinewest',
  grandecran: 'Grand Ecran',
  noecinemas: 'Noé Cinémas',
} satisfies Record<Provider, string>

// SAFETY: providers is a local literal exhaustively checked against Record<Provider, string> above.
const providerKeys = Object.keys(providers) as Provider[]

for (const provider of providerKeys) {
  const asset =
    provider === 'mk2'
      ? 'mk2_logo.svg'
      : provider === 'grandecran'
        ? 'grand_ecran_logo_small.webp'
        : provider === 'noecinemas'
          ? 'noe_cinema_logo_small.webp'
          : `${provider}_logo_small.webp`
  test(`${provider}: provider logo is unique and source name stays exact with or without its brand`, async () => {
    for (const name of [
      'Cinéma GALAXY',
      `Le ${providers[provider]} Centre`,
      `${providers[provider]} ${providers[provider]}`,
    ]) {
      const html = await render(name, provider)
      assert.equal((html.match(/<img /g) || []).length, 1)
      assert.ok(html.includes(asset), html)
      assert.ok(html.includes(` ${name}</span>`), html)
      assert.doesNotMatch(html, /class="sr-only"/)
      if (name === 'Cinéma GALAXY')
        assert.ok(html.includes(`alt="${providers[provider]}"`), html)
      else assert.match(html, /alt(?:="")? aria-hidden="true"/)
    }
  })

  test(`${provider}: other brand and format tokens cannot select extra logos; decorative and classes work`, async () => {
    const otherBrand = provider === 'ugc' ? 'CGR' : 'UGC'
    const name = `Le ${otherBrand} IMAX Dolby 4DX ScreenX 3D`
    const html = await render(name, provider)
    assert.equal((html.match(/<img /g) || []).length, 1)
    assert.ok(html.includes(asset), html)
    assert.ok(html.includes(`alt="${providers[provider]}"`), html)
    assert.ok(html.includes(` ${name}</span>`), html)
    const decorative = await render(name, provider, {
      decorative: true,
      logoClass: 'brightness-0 invert',
    })
    assert.match(decorative, /^<span aria-hidden="true">/)
    assert.match(decorative, /alt(?:="")? aria-hidden="true"/)
    assert.match(decorative, /brightness-0 invert/)
    assert.equal((decorative.match(/<img /g) || []).length, 1)
  })
}

test('brand detection folds accents and case but never matches a partial name token', async () => {
  for (const provider of providerKeys) {
    for (const name of [
      `Le ${providers[provider].toUpperCase()} Centre`,
      `(${provider}) Centre`,
      `${providers[provider].normalize('NFD')} Centre`,
    ]) {
      const html = await render(name, provider)
      assert.match(html, /alt(?:="")? aria-hidden="true"/)
      assert.ok(html.includes(` ${name}</span>`), html)
    }
    for (const name of [
      `${provider}ville`,
      `é${provider}`,
      `${provider}_centre`,
      `${provider}2`,
    ]) {
      const html = await render(name, provider)
      assert.ok(html.includes(`alt="${providers[provider]}"`), html)
    }
  }
})

test('Grand Ecran multi-word names preserve accents, locations and one accessible brand', async () => {
  for (const name of [
    'Grand Ecran Langon',
    'Grand Écran Vichy',
    'GRAND ÉCRAN Montaigu-Vendée',
    'Grand E\u0301cran La Chapelle-sur-Erdre',
    'Grand  Ecran',
  ]) {
    const html = await render(name, 'grandecran')
    assert.equal((html.match(/<img /g) || []).length, 1)
    assert.match(html, /grand_ecran_logo_small\.webp/)
    assert.match(html, /alt(?:="")? aria-hidden="true"/)
    assert.match(html, /bg-ink/)
    assert.ok(html.includes(` ${name}</span>`), html)
  }
  for (const name of [
    'Grand Ecranville',
    'éGrand Ecran',
    'Grand Ecran_centre',
    'Grand Ecran2',
    'Le Club',
  ]) {
    assert.match(await render(name, 'grandecran'), /alt="Grand Ecran"/)
  }
})

test('all public theater-name surfaces supply provider metadata, while timeline movies keep generic text', async () => {
  for (const path of [
    'pages/cinemas.vue',
    'pages/recherche.vue',
    'pages/cinema/[slug].vue',
    'pages/ville/[slug]/cinemas.vue',
    'pages/film/[slug].vue',
    'components/ShowtimeResultLine.vue',
    'components/ShowtimeResultBox.vue',
    'components/TimelineMatrix.vue',
    'components/CinemaTheaterMap.client.vue',
  ]) {
    const value = await source(path)
    assert.match(value, /<TheaterName[^>]*:provider=/, path)
    assert.doesNotMatch(
      value,
      /<BrandedText :text="(?:theater\.name|result\.theaterName|selected\.theater\.name)"/,
      path,
    )
    assert.doesNotMatch(value, /<TheaterName[^>]*\bplain\b/, path)
  }
  const cinemas = await source('pages/cinemas.vue')
  assert.match(cinemas, /:provider="row\.theater\.provider"/)
  assert.match(cinemas, /:provider="theater\.provider"/)
  const timeline = await source('components/TimelineMatrix.vue')
  assert.match(
    timeline,
    /<BrandedText\s+v-if="mode === 'theater'"\s+:text="item\.showtime\.movie\.title"/,
  )
  assert.match(timeline, /<BrandedText v-else :text="row\.label"/)
})
