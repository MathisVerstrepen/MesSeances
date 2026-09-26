import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import * as icons from '@lucide/vue'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import * as vue from 'vue'
import type { AccountSession } from '../app/types/account.ts'
import * as accountState from '../app/utils/accountState.ts'
import * as accountPrivacy from '../shared/accountPrivacy.ts'

interface HeaderModule {
  default?: vue.Component
}

const source = await readFile(
  new URL('../app/components/AppHeader.vue', import.meta.url),
  'utf8',
)
const { descriptor } = parse(source)
const compiled = ts.transpileModule(
  compileScript(descriptor, { id: 'AppHeader', inlineTemplate: true }).content,
  {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  },
).outputText

async function render(
  options: {
    count?: number
    cities?: string[]
    initialized?: boolean
    loading?: boolean
    path?: string
    session?: AccountSession
  } = {},
) {
  const exports: HeaderModule = {}
  runInNewContext(compiled, {
    exports,
    require: (name: string) => {
      if (name === 'vue') return vue
      if (name === '@lucide/vue') return icons
      if (name === '~/utils/accountState') return accountState
      if (name === '~~/shared/accountPrivacy') return accountPrivacy
      throw new Error(`Unexpected import: ${name}`)
    },
    ...vue,
    useRoute: () => ({ path: options.path ?? '/' }),
    useAccountSession: () => ({ session: vue.ref(options.session ?? null) }),
    useCinemaPreferences: () => ({
      favoriteTheaterIds: vue.ref(
        Array.from({ length: options.count ?? 4 }, (_, index) => `${index}`),
      ),
      favoriteTheaters: vue.ref(
        (options.cities ?? ['Lille', 'Lille', 'Lomme', 'Lomme']).map(
          (city) => ({
            city,
            city_slug: city.toLowerCase(),
          }),
        ),
      ),
      isInitialized: vue.ref(options.initialized ?? true),
      isLoading: vue.ref(options.loading ?? false),
      initialize: async () => {},
    }),
  })
  assert.ok(exports.default)
  const app = vue.createSSRApp(exports.default)
  app.component(
    'NuxtLink',
    vue.defineComponent({
      props: { to: String, prefetch: Boolean },
      setup:
        (props, { slots }) =>
        () =>
          vue.h('a', { href: props.to }, slots.default?.()),
    }),
  )
  return renderToString(app)
}

function link(html: string, href: string) {
  const result = html
    .match(/<a\b[^]*?<\/a>/g)
    ?.find((value) => value.includes(`href="${href}"`))
  assert.ok(result, `Missing link: ${href}`)
  return result
}

test('cinema summary shows an untruncated mobile count and keeps full desktop and accessible descriptions', async () => {
  for (const fixture of [
    {
      count: 4,
      cities: ['Lille', 'Lille', 'Lomme', 'Lomme'],
      mobile: '4 cinémas',
      full: '4 cinémas · 2 villes',
    },
    { count: 1, cities: ['Lille'], mobile: '1 cinéma', full: 'Lille · 1' },
    { count: 0, cities: [], mobile: '0 cinémas', full: 'Mes cinémas' },
    {
      count: 0,
      cities: [],
      initialized: false,
      loading: true,
      mobile: 'Chargement…',
      full: 'Chargement…',
    },
    {
      count: 0,
      cities: [],
      initialized: false,
      loading: false,
      mobile: '0 cinémas',
      full: 'Mes cinémas',
    },
    {
      count: 4,
      cities: ['Lille', 'Lomme'],
      loading: true,
      mobile: '4 cinémas',
      full: '4 cinémas · 2 villes',
    },
  ]) {
    const cinema = link(await render(fixture), '/cinemas')
    assert.ok(
      cinema.includes(`aria-label="Gérer mes cinémas, ${fixture.full}"`),
    )
    assert.ok(
      cinema.includes(
        `<span class="whitespace-nowrap sm:hidden">${fixture.mobile}</span>`,
      ),
    )
    assert.ok(
      cinema.includes(
        `<span class="hidden max-w-44 truncate sm:inline">${fixture.full}</span>`,
      ),
    )
    const openingTag = cinema.slice(0, cinema.indexOf('>'))
    assert.doesNotMatch(
      openingTag,
      /border-l-2|border-ink|\bpl-\d|truncate|max-w-/,
    )
    assert.match(openingTag, /shrink-0/)
    assert.match(openingTag, /gap-1\.5/)
  }
})

test('mobile nav uses legible short labels, retaining full search name, destinations and touch targets', async () => {
  const html = await render({ path: '/recherche' })
  const search = link(html, '/recherche')
  assert.match(search, /aria-label="Trouver une séance"/)
  assert.match(search, /<span class="sm:hidden">Séances<\/span>/)
  assert.match(
    search,
    /<span class="hidden sm:inline">Trouver une séance<\/span>/,
  )
  assert.match(search, /aria-current="page"/)
  assert.match(search, /bg-ink text-white/)
  assert.match(html, /grid-cols-4/)
  for (const href of ['/planning', '/recherche', '/films', '/connexion']) {
    const navigationLink = link(html, href)
    assert.match(navigationLink, /text-\[10px\]/)
    assert.match(navigationLink, /lg:text-xs/)
    assert.match(navigationLink, /min-h-14/)
    assert.match(navigationLink, /lg:min-h-\[4\.5rem\]/)
    assert.doesNotMatch(navigationLink, /text-\[(?:9|11)px\]/)
    if (href !== '/recherche')
      assert.doesNotMatch(navigationLink, /aria-current/)
  }
  assert.match(link(html, '/planning'), /<span>Planning<\/span>/)
  assert.match(link(html, '/films'), /<span>Films<\/span>/)
  assert.match(link(html, '/connexion'), /<span>Connexion<\/span>/)
  const brand = link(html, '/')
  assert.match(brand, /shrink-0/)
  assert.match(brand, /aria-label="MesSeances, accueil"/)
  assert.match(brand, /MesSeances<span/)
  for (const classes of brand.match(/class="[^"]*"/g) ?? [])
    assert.doesNotMatch(classes, /\bhidden\b|truncate/)
})

test('header keeps account labels and selected links unchanged on account and cinema routes', async () => {
  const anonymous = await render({ path: '/inscription' })
  assert.match(link(anonymous, '/connexion'), /aria-current="page"/)
  assert.match(link(anonymous, '/connexion'), /<span>Connexion<\/span>/)
  const signedIn = await render({
    path: '/compte',
    session: {
      enabled: true,
      state: 'complete',
      account: {
        email: 'header@example.test',
        username: 'header',
        has_password: true,
        google_linked: false,
      },
    },
  })
  assert.match(link(signedIn, '/compte'), /aria-current="page"/)
  assert.match(link(signedIn, '/compte'), /<span>Mon compte<\/span>/)
  assert.match(link(signedIn, '/compte'), /text-\[10px\]/)
  assert.match(link(signedIn, '/compte'), /lg:text-xs/)
  assert.match(
    link(await render({ path: '/cinemas' }), '/cinemas'),
    /aria-current="page"/,
  )
  assert.match(
    link(await render({ path: '/film/example' }), '/films'),
    /aria-current="page"/,
  )
})
