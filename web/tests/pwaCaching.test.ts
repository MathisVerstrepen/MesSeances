import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import {
  accountPageRoots,
  accountPrivacyHeaders,
  isAccountPrivatePath,
} from '../shared/accountPrivacy.ts'

type Headers = Record<string, string>
interface Response {
  headers: Headers
}
interface Context {
  event: { path: string; headers: Headers }
}
const read = (path: string) => readFile(new URL(path, import.meta.url), 'utf8')
async function hook() {
  let callback!: (response: Response, context: Context) => void
  runInNewContext(
    ts.transpileModule(await read('../server/plugins/document-cache.ts'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS },
    }).outputText,
    {
      exports: {},
      require: () => ({ isAccountPrivatePath }),
      defineNitroPlugin: (
        plugin: (app: {
          hooks: { hook: (name: string, value: typeof callback) => void }
        }) => void,
      ) =>
        plugin({
          hooks: {
            hook: (name: string, value: typeof callback) => {
              assert.equal(name, 'render:response')
              callback = value
            },
          },
        }),
      getRequestURL: (event: Context['event']) => ({ pathname: event.path }),
      getResponseHeaders: (event: Context['event']) => event.headers,
    },
  )
  return callback
}
test('public HTML alone receives no-cache; mixed-case aliases removed and other headers retained', async () => {
  const apply = await hook()
  for (const headers of [
    {
      'Content-Type': 'text/html;charset=utf-8',
      'cAcHe-CoNtRoL': 'public, max-age=600',
    },
    { 'content-type': 'text/html', 'Cache-Control': 'no-cache' },
  ]) {
    const response = {
      headers: {
        ...headers,
        Vary: 'Accept-Encoding',
        'X-Robots-Tag': 'noindex,follow',
      },
    }
    apply(response, { event: { path: '/credits', headers: {} } })
    assert.equal(response.headers['Cache-Control'], 'no-cache')
    assert.equal('cAcHe-CoNtRoL' in response.headers, false)
    assert.equal(response.headers.Vary, 'Accept-Encoding')
    assert.equal(response.headers['X-Robots-Tag'], 'noindex,follow')
  }
})
test('private/no-store and Set-Cookie in either rendered or event headers remain untouched', async () => {
  const apply = await hook()
  for (const stronger of [
    { 'CACHE-control': 'PRIVATE, max-age=0' },
    { 'cache-Control': 'public, no-store' },
    { 'Set-Cookie': 'synthetic=fixture' },
  ]) {
    for (const location of ['rendered', 'event']) {
      const response: Response = {
        headers: {
          'content-type': 'text/html',
        },
      }
      if (location === 'rendered') Object.assign(response.headers, stronger)
      const before = { ...response.headers }
      apply(response, {
        event: {
          path: '/credits',
          headers: location === 'event' ? stronger : {},
        },
      })
      assert.deepEqual(response.headers, before)
    }
  }
})
test('normalized private/account and admin roots or descendants are excluded, not similarly named public paths', async () => {
  const apply = await hook()
  for (const path of [
    ...accountPageRoots,
    '/COMPTE/PARAMETRES',
    '/%63ompte',
    '//compte//form',
    '/api/v1/account',
    '/admin',
    '/ADMIN/SYNC',
    '/%61dmin',
    '//admin//sync',
    '/admin\\sync',
    '/%61dmin%2Fsync',
  ]) {
    const response = { headers: { 'Content-Type': 'text/html' } }
    apply(response, { event: { path, headers: {} } })
    assert.equal('Cache-Control' in response.headers, false, path)
  }
  for (const path of ['/administration', '/compteur']) {
    const response: Response = { headers: { 'Content-Type': 'text/html' } }
    apply(response, { event: { path, headers: {} } })
    assert.equal(response.headers['Cache-Control'], 'no-cache')
  }
})
test('non-HTML payload/API/SW/manifest and hashed assets stay unchanged', async () => {
  const apply = await hook()
  for (const [path, type] of [
    ['/_nuxt/file.js', 'text/javascript'],
    ['/_nuxt/file.css', 'text/css'],
    ['/credits/_payload.json', 'application/json'],
    ['/api/v1/movies', 'application/json'],
    ['/sw.js', 'application/javascript'],
    ['/manifest.webmanifest', 'application/manifest+json'],
  ]) {
    const response = {
      headers: {
        'Content-Type': type!,
        'Cache-Control': 'public, max-age=31536000, immutable',
      },
    }
    const before = { ...response.headers }
    apply(response, { event: { path: path!, headers: {} } })
    assert.deepEqual(response.headers, before)
  }
  const response = {
    headers: {
      'Content-Type': 'application/json',
      'Cache-Control': 'private, no-store',
    },
  }
  apply(response, {
    event: {
      path: '/api/v1/account',
      headers: { 'content-type': 'text/html' },
    },
  })
  assert.equal(response.headers['Cache-Control'], 'private, no-store')
})
test('config preserves private policies, disables competing reloads, filters mutable pointer after Nuxt transform', async () => {
  let config!: {
    experimental: {
      checkOutdatedBuildInterval: boolean
      emitRouteChunkError: string
    }
    routeRules: Record<string, { headers: Headers }>
    pwa: {
      client: { registerPlugin: boolean }
      injectRegister: boolean
      registerType: string
      workbox: {
        navigateFallback: null
        runtimeCaching: {
          urlPattern: (input: { url: URL; sameOrigin: boolean }) => boolean
          handler: string
          options: { fetchOptions: { cache: string } }
        }[]
      }
    }
    hooks: Record<
      string,
      (options: {
        workbox: {
          manifestTransforms: ((entries: { url: string }[]) => {
            manifest: { url: string }[]
          })[]
        }
      }) => void
    >
  }
  runInNewContext(
    ts.transpileModule(await read('../nuxt.config.ts'), {
      compilerOptions: { module: ts.ModuleKind.CommonJS },
    }).outputText,
    {
      exports: {},
      require: (name: string) =>
        name.startsWith('@tailwind')
          ? { __esModule: true, default: () => ({}) }
          : { accountPageRoots, accountPrivacyHeaders },
      process: { env: {} },
      defineNuxtConfig: (value: typeof config) => {
        config = value
      },
    },
  )
  assert.equal(
    config.routeRules['/_nuxt/builds/latest.json']!.headers['Cache-Control'],
    'no-store',
  )
  assert.equal(
    config.routeRules['/compte/**']!.headers['Cache-Control'],
    'private, no-store',
  )
  assert.equal(config.experimental.checkOutdatedBuildInterval, false)
  assert.equal(config.experimental.emitRouteChunkError, 'manual')
  assert.equal(config.pwa.client.registerPlugin, false)
  assert.equal(config.pwa.injectRegister, false)
  assert.equal(config.pwa.registerType, 'prompt')
  assert.equal(config.pwa.workbox.navigateFallback, null)
  const rule = config.pwa.workbox.runtimeCaching[0]!
  assert.equal(rule.handler, 'NetworkOnly')
  assert.equal(rule.options.fetchOptions.cache, 'no-store')
  assert.equal(
    rule.urlPattern({
      url: new URL('https://test/_nuxt/builds/latest.json?fresh=123'),
      sameOrigin: true,
    }),
    true,
  )
  assert.equal(
    rule.urlPattern({
      url: new URL('https://test/_nuxt/builds/latest.json'),
      sameOrigin: false,
    }),
    false,
  )
  assert.equal(
    rule.urlPattern({
      url: new URL('https://test/_nuxt/file.js'),
      sameOrigin: true,
    }),
    false,
  )
  const previous = (entries: { url: string }[]) => ({
    manifest: [...entries, { url: '_nuxt/builds/latest.json' }],
  })
  const options = { workbox: { manifestTransforms: [previous] } }
  config.hooks['pwa:beforeBuildServiceWorker']!(options)
  assert.equal(options.workbox.manifestTransforms[0], previous)
  let entries = [
    { url: '_nuxt/builds/meta/a.json' },
    { url: 'manifest.webmanifest' },
  ]
  for (const transform of options.workbox.manifestTransforms)
    entries = transform(entries).manifest
  assert.deepEqual(
    entries.map((entry) => entry.url),
    ['_nuxt/builds/meta/a.json', 'manifest.webmanifest'],
  )
})
