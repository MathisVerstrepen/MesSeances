import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { computed, createSSRApp, type Component } from 'vue'
import ts from 'typescript'
import { formatBrand, formatLabel } from '../app/utils/formats.ts'

const require = createRequire(import.meta.url)
interface ComponentModule {
  default?: Component
}
async function component(name: string): Promise<Component> {
  const source = await readFile(
    new URL(`../app/components/${name}.vue`, import.meta.url),
    'utf8',
  )
  const { descriptor } = parse(source)
  const script = compileScript(descriptor, { id: name, inlineTemplate: true })
  const compiled = ts.transpileModule(script.content, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText
  const exports: ComponentModule = {}
  const localRequire = (id: string) =>
    id.startsWith('~/assets/')
      ? { __esModule: true, default: id }
      : id === '~/utils/formats'
        ? { formatBrand, formatLabel }
        : require(id)
  new Function('require', 'exports', 'computed', compiled)(
    localRequire,
    exports,
    computed,
  )
  assert.ok(exports.default)
  return exports.default
}
const BrandLogo = await component('BrandLogo')
const ShowtimeFormat = await component('ShowtimeFormat')
async function render(
  root: Component,
  props: Record<string, string | boolean>,
) {
  const app = createSSRApp(root, props)
  app.component('BrandLogo', BrandLogo)
  return renderToString(app)
}

test('Infinity Vision logo variants retain accessible brand names', async () => {
  for (const variant of ['inline', 'display']) {
    const html = await render(BrandLogo, { brand: 'INFINITY_VISION', variant })
    assert.match(html, /alt="Infinity Vision"/)
    assert.doesNotMatch(html, /aria-hidden/)
  }
})

test('Infinity Vision decorative logos avoid duplicate accessible names', async () => {
  const html = await render(BrandLogo, {
    brand: 'INFINITY_VISION',
    decorative: true,
  })
  assert.match(html, /alt(?:="")? aria-hidden="true"/)
  assert.doesNotMatch(html, /alt="Infinity Vision"/)
})

test('Infinity Vision showtime badge has exactly one accessible label and a decorative image', async () => {
  const html = await render(ShowtimeFormat, { format: 'INFINITY_VISION' })
  assert.equal((html.match(/Infinity Vision/g) || []).length, 1)
  assert.match(html, /<span class="sr-only">Infinity Vision<\/span>/)
  assert.match(html, /infinity_vision_logo_small\.png\?no-inline/)
  assert.match(html, /alt(?:="")? aria-hidden="true"/)
  const decorative = await render(ShowtimeFormat, {
    format: 'INFINITY_VISION',
    decorative: true,
  })
  assert.match(decorative, /^<span aria-hidden="true">/)
  assert.doesNotMatch(decorative, /sr-only|Infinity Vision/)
})

test('ICE logo variants retain accessible brand names', async () => {
  for (const variant of ['inline', 'display']) {
    const html = await render(BrandLogo, { brand: 'ICE', variant })
    assert.match(html, /alt="ICE"/)
    assert.doesNotMatch(html, /aria-hidden/)
  }
})

test('ICE decorative logos and showtime badges preserve accessibility', async () => {
  const logo = await render(BrandLogo, {
    brand: 'ICE',
    decorative: true,
  })
  assert.match(logo, /alt(?:="")? aria-hidden="true"/)
  assert.doesNotMatch(logo, /alt="ICE"/)
  const badge = await render(ShowtimeFormat, { format: 'ICE' })
  assert.equal((badge.match(/ICE/g) || []).length, 1)
  assert.match(badge, /<span class="sr-only">ICE<\/span>/)
  assert.match(badge, /ice_logo_small\.webp\?no-inline/)
  assert.match(badge, /alt(?:="")? aria-hidden="true"/)
  const decorative = await render(ShowtimeFormat, {
    format: 'ICE',
    decorative: true,
  })
  assert.match(decorative, /^<span aria-hidden="true">/)
  assert.doesNotMatch(decorative, /sr-only|ICE|infinity_vision/)
})
