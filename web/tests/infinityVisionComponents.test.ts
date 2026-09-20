import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
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

test('Infinity Vision uses unchanged supplied PNGs for inline and display horizontal logos', async () => {
  const variants = [
    {
      variant: 'inline',
      size: 'small',
      hash: 'a99ff06bf24296d208edb7f3d680e805a1aecdadd3c1955eb5d48ea39638b0d3',
      width: 300,
      height: 61,
    },
    {
      variant: 'display',
      size: 'large',
      hash: '02a74df0d5182f71a280ff5d3bd05e665842c707e1cd234d471bc0cd5ae06343',
      width: 1129,
      height: 228,
    },
  ]
  for (const { variant, size, hash, width, height } of variants) {
    const basename = `infinity_vision_logo_${size}.png`
    const bytes = await readFile(
      new URL(`../app/assets/imgs/${basename}`, import.meta.url),
    )
    assert.equal(createHash('sha256').update(bytes).digest('hex'), hash)
    assert.equal(bytes.readUInt32BE(16), width)
    assert.equal(bytes.readUInt32BE(20), height)
    const html = await render(BrandLogo, { brand: 'INFINITY_VISION', variant })
    assert.ok(html.includes(`${basename}?no-inline`))
    assert.match(html, /alt="Infinity Vision"/)
    assert.doesNotMatch(html, /aria-hidden|bg-ink|bg-white/)
    assert.match(html, /max-w-full/)
    assert.match(html, /object-contain/)
    if (variant === 'inline')
      assert.ok(html.includes('h-[0.68em] w-auto align-[-0.06em]'))
    else assert.match(html, /w-44 sm:w-52/)
  }
})

test('Infinity Vision decorative logos retain selected-state inversion without duplicate accessible names', async () => {
  const html = await render(BrandLogo, {
    brand: 'INFINITY_VISION',
    decorative: true,
    class: 'brightness-0 invert',
  })
  assert.match(html, /alt(?:="")? aria-hidden="true"/)
  assert.match(html, /brightness-0 invert/)
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
    logoClass: 'brightness-0 invert',
  })
  assert.match(decorative, /^<span aria-hidden="true">/)
  assert.match(decorative, /brightness-0 invert/)
  assert.doesNotMatch(decorative, /sr-only|Infinity Vision/)
  const ice = await render(ShowtimeFormat, { format: 'ICE' })
  assert.match(ice, /<span>ICE<\/span>/)
  assert.doesNotMatch(ice, /<img|infinity_vision/)
})
