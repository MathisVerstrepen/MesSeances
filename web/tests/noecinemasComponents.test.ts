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

const source = (path: string) => readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')
const require = createRequire(import.meta.url)
interface ComponentModule { default?: Component }
interface RenderProps {
  brand?: string
  variant?: string
  decorative?: boolean
  text?: string
  name?: string
  provider?: string
  url?: string | null
  showtimeId?: string | null
  theaterId?: string | null
}
async function component(name: string): Promise<Component> {
  const { descriptor } = parse(await source(`components/${name}.vue`))
  const script = compileScript(descriptor, { id: name, inlineTemplate: true })
  const compiled = ts.transpileModule(script.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const exports: ComponentModule = {}
  const localRequire = (id: string) => id.startsWith('~/assets/') ? { __esModule: true, default: id } : id === '~/utils/bookingUrl' ? { safeBookingUrl } : require(id)
  new Function('require', 'exports', 'computed', compiled)(localRequire, exports, computed)
  assert.ok(exports.default)
  return exports.default
}
const [BrandLogo, BrandedText, TheaterName, BookingLink] = await Promise.all(['BrandLogo', 'BrandedText', 'TheaterName', 'BookingLink'].map(component))
async function render(root: Component, props: RenderProps) {
  const app = createSSRApp(root, props)
  app.component('BrandLogo', BrandLogo!)
  app.component('BrandedText', BrandedText!)
  return renderToString(app)
}

test('Noé exact supplied bytes serve both logo sizes on light backing with accessible brand', async () => {
  const image = await readFile(new URL('../app/assets/imgs/noe_cinema_logo_small.webp', import.meta.url))
  assert.equal(createHash('sha256').update(image).digest('hex'), '78231e6aa9d9026f1bc68d266419d45477aaf263fa8ef1f959f2356278179e5a')
  for (const variant of ['inline', 'display']) {
    const html = await render(BrandLogo!, { brand: 'Noé Cinémas', variant })
    assert.match(html, /noe_cinema_logo_small\.webp\?no-inline/)
    assert.match(html, /alt="Noé Cinémas"/)
    assert.match(html, /bg-white/)
    assert.match(html, /object-contain/)
    assert.doesNotMatch(html, /bg-ink/)
  }
  assert.match(await render(BrandLogo!, { brand: 'Noé Cinémas', decorative: true }), /alt(?:="")? aria-hidden="true"/)
  assert.match(await source('pages/credits.vue'), /brand: 'Noé Cinémas', name: 'Noé Cinémas', url: 'https:\/\/www\.noecinemas\.com\/'/)
})

test('Noé text recognition uses full accented or unaccented names and preserves location/source text', async () => {
  for (const name of ['Noé Cinémas', 'Noe Cinemas', 'NOÉ CINÉMAS', 'Noé Cinémas'.normalize('NFD'), 'Noe  Cinemas']) {
    const text = `Cinéma ${name} L'Aigle`
    const html = await render(BrandedText!, { text })
    assert.equal((html.match(/<img /g) || []).length, 1)
    assert.match(html, /noe_cinema_logo_small/)
    assert.ok(html.includes('class="sr-only"'))
    const theater = await render(TheaterName!, { name: text, provider: 'noecinemas' })
    assert.equal((theater.match(/<img /g) || []).length, 1)
    assert.match(theater, /alt(?:="")? aria-hidden="true"/)
    assert.ok(theater.includes(name))
  }
  for (const name of ['Noe Cinemascope', 'éNoe Cinemas', 'Noe Cinemas_centre', 'Noe Cinemas2', 'Le Club']) {
    assert.doesNotMatch(await render(BrandedText!, { text: name }), /<img /)
    assert.match(await render(TheaterName!, { name, provider: 'noecinemas' }), /alt="Noé Cinémas"/)
  }
})

test('Noé BookingLink renders safe new-tab link or unavailable text, never unsafe href', async () => {
  const url = 'https://achat.cinema-laigle.com/reserver/r/19'
  const props = { url, provider: 'noecinemas', showtimeId: `noecinemas-showing-P8088-${'a'.repeat(64)}`, theaterId: 'noecinemas-P8088' }
  const html = await render(BookingLink!, props)
  assert.match(html, /href="https:\/\/achat\.cinema-laigle\.com\/reserver\/r\/19"/)
  assert.match(html, /target="_blank" rel="noopener noreferrer"/)
  assert.match(html, /aria-label="Réserver sur Noé Cinémas, ouverture dans un nouvel onglet"/)
  for (const overrides of [{ url: `${url}?relay=1` }, { theaterId: 'noecinemas-B0181' }, { showtimeId: null }, { provider: 'cgr' }, { url: null }]) {
    const unavailable = await render(BookingLink!, { ...props, ...overrides })
    assert.doesNotMatch(unavailable, /href=/)
    assert.match(unavailable, /aria-disabled="true"/)
    assert.match(unavailable, /Réservation indisponible/)
  }
})

test('Noé follows Grand Ecran throughout admin, latest-run, map and labels', async () => {
  for (const page of ['sync', 'sync-schedules', 'theater-locations', 'tmdb-matches']) {
    const value = await source(`pages/admin/${page}.vue`)
    assert.match(value, /noecinemas: 'Noé Cinémas'/)
    if (page.startsWith('sync')) assert.match(value, /const providers = \['ugc', 'kinepolis', 'pathe', 'cgr', 'megarama', 'cineville', 'mk2', 'cinewest', 'grandecran', 'noecinemas'\] as const/)
  }
  assert.match(await source('pages/admin/sync-schedules.vue'), /noecinemas: selectLatestProviderRun\('noecinemas'/)
  assert.match(await source('components/CinemaTheaterMap.client.vue'), /'noecinemas', THEATER_PROVIDER_COLORS\.noecinemas/)
})
