import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { type Context, runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import { type Component, computed, createSSRApp, ref } from 'vue'
import * as accountState from '../app/utils/accountState.ts'

const require = createRequire(import.meta.url)
const read = (path: string) => readFile(new URL(path, import.meta.url), 'utf8')

interface ComponentModule {
  default?: Component
}

async function component(path: string, globals: Context = {}) {
  const { descriptor } = parse(await read(path))
  const exports: ComponentModule = {}
  runInNewContext(
    ts.transpileModule(
      compileScript(descriptor, { id: path, inlineTemplate: true }).content,
      {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2022,
        },
      },
    ).outputText,
    {
      exports,
      computed,
      require: (id: string) =>
        id === '~/utils/accountState' ? accountState : require(id),
      ...globals,
    },
  )
  assert.ok(exports.default)
  return exports.default
}

const WatchlistIcon = await component('../app/components/WatchlistIcon.vue')

test('watchlist uses an unfilled inherited-color clock with distinct plus/minus and no navigation badge', async () => {
  for (const variant of ['navigation', 'add', 'remove'] as const) {
    for (const size of [18, 20, 24]) {
      const html = await renderToString(
        createSSRApp(WatchlistIcon, { variant, size }),
      )
      assert.match(html, /aria-hidden="true"/)
      assert.match(html, /focusable="false"/)
      assert.ok(html.includes(`width="${size}" height="${size}"`))
      assert.ok(html.includes(`data-watchlist-icon="${variant}"`))
      assert.match(html, /viewBox="0 0 24 24"/)
      assert.match(html, /fill="none" stroke="currentColor" stroke-width="2"/)
      assert.doesNotMatch(
        html,
        /fill="(?!none)[^"]*"|fill-surface|text-surface/,
      )
      assert.doesNotMatch(
        html,
        /text-accent|text-primary|#1f6f78|#991b1b|M7\.5 10 12 12 17 6\.5|Bookmark/,
      )
      if (variant === 'navigation') {
        assert.match(html, /<circle cx="12" cy="12" r="10"/)
        assert.match(html, /d="M12 6v6l4 2"/)
        assert.equal([...html.matchAll(/<circle/g)].length, 1)
        assert.equal([...html.matchAll(/<path/g)].length, 1)
      } else {
        assert.doesNotMatch(html, /<circle|<g[ >]/)
        assert.match(html, /d="M21\.92 13\.267a10 10 0 1 0-8\.653 8\.653"/)
        assert.match(html, /d="M12 6v6l3\.644 1\.822"/)
        assert.match(html, /d="M16 19h6"/)
        assert.equal(html.includes('d="M19 16v6"'), variant === 'add')
        assert.equal(
          [...html.matchAll(/<path/g)].length,
          variant === 'add' ? 4 : 3,
        )
      }
    }
  }
  const plain = await renderToString(createSSRApp(WatchlistIcon))
  assert.match(plain, /data-watchlist-icon="navigation"/)
})

test('button keeps labels, 48px target and busy/pressed semantics across clock states', async () => {
  for (const state of [
    { saved: false, ready: true, saving: false, unknown: false },
    { saved: true, ready: true, saving: false, unknown: false },
    { saved: false, ready: true, saving: true, unknown: false },
    { saved: true, ready: true, saving: true, unknown: false },
    { saved: true, ready: false, saving: false, unknown: true },
    {
      saved: false,
      ready: true,
      saving: false,
      unknown: false,
      revalidating: true,
    },
    {
      saved: true,
      ready: true,
      saving: false,
      unknown: false,
      revalidating: true,
    },
    {
      saved: false,
      ready: false,
      saving: false,
      unknown: false,
      anonymous: true,
      revalidating: true,
    },
  ]) {
    const Button = await component('../app/components/WatchlistButton.vue', {
      useAccountSession: () => ({
        session: ref(
          'anonymous' in state ? null : { enabled: true, state: 'complete' },
        ),
        status: ref('ready'),
        writesBlocked: ref('revalidating' in state && state.revalidating),
      }),
      useWatchlist: () => ({
        owner: ref('anonymous' in state ? '' : 'owner'),
        ready: ref(state.ready),
        slugs: ref(new Set(state.saved ? ['film-1'] : [])),
        saving: ref(state.saving),
        error: ref(''),
      }),
    })
    const app = createSSRApp(Button, { slug: 'film-1' })
    app.component('WatchlistIcon', WatchlistIcon)
    const html = await renderToString(app)
    const saved = state.ready && state.saved
    const label = state.unknown
      ? 'Watchlist indisponible pendant la vérification'
      : saved
        ? 'Retirer de la watchlist'
        : 'Ajouter à la watchlist'
    assert.ok(html.includes(`aria-label="${label}"`))
    assert.ok(html.includes(`title="${label}"`))
    assert.match(html, /size-12/)
    assert.match(html, /<svg[^>]*width="24" height="24"/)
    assert.match(html, /focus-visible:outline-2/)
    assert.match(html, /text-ink/)
    const buttonClasses = html.match(/<button[^>]*class="([^"]*)"/)?.[1]
    assert.ok(buttonClasses)
    assert.match(buttonClasses, /bg-surface enabled:hover:bg-subtle/)
    assert.doesNotMatch(buttonClasses, /highlight|color-mix/)
    assert.ok(
      html.includes(`data-watchlist-icon="${saved ? 'remove' : 'add'}"`),
    )
    const blocked =
      state.unknown ||
      state.saving ||
      (!('anonymous' in state) && 'revalidating' in state && state.revalidating)
    assert.ok(html.includes(`aria-busy="${blocked}"`))
    assert.equal(/<button[^>]* disabled(?:\s|=|>)/.test(html), blocked)
    if (state.unknown) assert.doesNotMatch(html, /aria-pressed/)
    else assert.ok(html.includes(`aria-pressed="${saved}"`))
  }
})

test('watchlist navigation and home reuse the plain clock without bookmark regression', async () => {
  for (const path of [
    '../app/components/AccountAreaNavigation.vue',
    '../app/pages/compte/index.vue',
  ]) {
    const Navigation = await component(path, {
      useRoute: () => ({ path: '/compte/watchlist' }),
      useAccountSession: () => ({ session: ref({ state: 'complete' }) }),
      definePageMeta: () => {},
      useHead: () => {},
    })
    const app = createSSRApp(Navigation)
    app.component('WatchlistIcon', WatchlistIcon)
    app.component('NuxtLink', {
      props: ['to', 'prefetch'],
      template: '<a :href="to"><slot /></a>',
    })
    app.component('AccountShell', { template: '<main><slot /></main>' })
    const html = await renderToString(app)
    assert.match(
      html,
      /<a[^>]*href="\/compte\/watchlist"[^>]*>[\s\S]*?data-watchlist-icon="navigation"[\s\S]*?Watchlist/,
    )
    assert.doesNotMatch(html, /data-watchlist-icon="(?:add|remove)"/)
    assert.match(html, /focus-visible:outline/)
    assert.doesNotMatch(await read(path), /Bookmark/)
  }
  assert.doesNotMatch(
    await read('../app/components/WatchlistButton.vue'),
    /Bookmark/,
  )
})
