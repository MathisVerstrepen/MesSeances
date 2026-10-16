import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import * as vue from 'vue'

const require = createRequire(import.meta.url)

export function executeModule<Exports>(
  source: string,
  globals = {},
  imports = {},
): Exports {
  const exports = {}
  const dependencies = new Map(Object.entries(imports))
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText
  const context = {
    ...globals,
    exports,
    require: (id: string) =>
      dependencies.has(id) ? dependencies.get(id) : require(id),
  }
  new Function(...Object.keys(context), compiled)(...Object.values(context))
  // SAFETY: callers declare exports of the actual compiled application module; execution assertions verify those entry points.
  return exports as Exports
}

export async function renderVue(
  path: string,
  props: Parameters<typeof vue.createSSRApp>[1] = {},
  imports = {},
) {
  const source = await readFile(
    new URL(`../../app/${path}`, import.meta.url),
    'utf8',
  )
  const { descriptor } = parse(source)
  const script = compileScript(descriptor, { id: path, inlineTemplate: true })
  const component = executeModule<{ default: vue.Component }>(
    script.content,
    vue,
    imports,
  ).default
  const app = vue.createSSRApp(component, props)
  app.component('BrandedText', {
    props: ['text', 'decorative'],
    setup: (props) => () => props.text,
  })
  return renderToString(app)
}

export async function renderPageTemplate<Context extends object>(
  path: string,
  context: Context,
) {
  const source = await readFile(
    new URL(`../../app/${path}`, import.meta.url),
    'utf8',
  )
  const { descriptor } = parse(source)
  assert.ok(descriptor.template)
  const template = compileTemplate({
    source: descriptor.template.content,
    filename: path,
    id: path,
  })
  assert.deepEqual(template.errors, [])
  const { render } = executeModule<{ render: vue.RenderFunction }>(
    template.code,
  )
  const app = vue.createSSRApp({ setup: () => context, render })
  for (const name of [
    'NuxtLink',
    'BrandLogo',
    'ArrowLeft',
    'RefreshCw',
    'CalendarClock',
    'ChevronDown',
    'AlertTriangle',
    'LoaderCircle',
    'EditorialStatePanel',
  ])
    app.component(name, { render: () => null })
  return renderToString(app)
}
