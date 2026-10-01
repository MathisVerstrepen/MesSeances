import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import type { FetchOptions } from 'ofetch'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import {
  type Component,
  computed,
  createSSRApp,
  effectScope,
  h,
  nextTick,
  readonly,
  ref,
  watch,
} from 'vue'
import type { useWatchlist } from '../app/composables/useWatchlist.ts'
import type { useAccountApi } from '../app/composables/useAccountApi.ts'
import type { useAccountSession } from '../app/composables/useAccountSession.ts'
import type { AccountSession } from '../app/types/account.ts'
import type {
  AccountWatchlist,
  ImportedWatchlist,
  SaveWatchlist,
  SaveWatchlistSort,
  SaveWatchlistPreferences,
  WatchlistViewMode,
  WatchlistSortOrder,
  WatchlistSearch,
  CreateWatchlistTag,
  UpdateWatchlistTag,
  DeleteWatchlistTag,
  AssignWatchlistTag,
} from '../app/types/watchlist.ts'
import * as errors from '../app/utils/accountState.ts'
import * as dates from '../app/utils/date.ts'
import * as images from '../app/utils/safeImageUrl.ts'
import * as externalLinks from '../app/utils/movieExternalLinks.ts'
import * as upcoming from '../app/utils/upcomingMovies.ts'
import * as grouping from '../app/utils/watchlistGrouping.ts'
import * as sorting from '../app/utils/watchlistSort.ts'
import * as tagging from '../app/utils/watchlistTags.ts'

const require = createRequire(import.meta.url)

interface AccountApiExports {
  useAccountApi?: typeof useAccountApi
}
const rowSource = await readFile(
  new URL('../app/components/WatchlistMovieRow.vue', import.meta.url),
  'utf8',
)
const { descriptor } = parse(rowSource)
interface RowModule {
  default?: Component
}
const rowModule: RowModule = {}
runInNewContext(
  ts.transpileModule(
    compileScript(descriptor, {
      id: 'WatchlistMovieRow',
      inlineTemplate: true,
    }).content,
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText,
  {
    exports: rowModule,
    computed,
    require: (id: string) => {
      if (id === '~/utils/date') return dates
      if (id === '~/utils/safeImageUrl') return images
      if (id === '~/utils/movieExternalLinks') return externalLinks
      if (id === '~/utils/upcomingMovies') return upcoming
      return require(id)
    },
  },
)
assert.ok(rowModule.default)
const WatchlistMovieRow = rowModule.default

interface RowDateProps {
  slug?: string
  tmdbId?: string
  releaseDate?: string | null
  frenchReleaseDate?: string | null
}

async function renderRow(props: RowDateProps) {
  const app = createSSRApp(WatchlistMovieRow, {
    title: 'Film sauvegardé',
    slug: 'film-1',
    ...props,
  })
  app.component('NuxtLink', {
    props: ['to'],
    setup:
      (props, { slots }) =>
      () =>
        h('a', { href: props.to }, slots.default?.()),
  })
  return renderToString(app)
}

test('compact tag trigger shortens only its name; default desktop trigger and dialog title stay unchanged', async () => {
  const source = await readFile(
    new URL('../app/components/WatchlistTagManager.vue', import.meta.url),
    'utf8',
  )
  const { descriptor } = parse(source)
  const managerModule: RowModule = {}
  runInNewContext(
    ts.transpileModule(
      compileScript(descriptor, {
        id: 'WatchlistTagManager',
        inlineTemplate: true,
      }).content,
      {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2022,
        },
      },
    ).outputText,
    {
      exports: managerModule,
      computed,
      ref,
      watch,
      nextTick,
      useTemplateRef: () => ref(null),
      onMounted: () => {},
      onBeforeUnmount: () => {},
      useWatchlist: () => ({ scopeKey: ref(0) }),
      require: (id: string) =>
        id === '~/utils/watchlistTags' ? tagging : require(id),
    },
  )
  assert.ok(managerModule.default)
  for (const compact of [undefined, true]) {
    const app = createSSRApp(managerModule.default, {
      tags: [],
      ready: true,
      blocked: false,
      compactTrigger: compact,
    })
    app.component('WatchlistTagColorPicker', { render: () => null })
    const html = await renderToString(app)
    assert.match(
      html,
      new RegExp(
        `</svg>\\s*${compact ? 'Tags' : 'Gérer les tags'}\\s*</button>`,
      ),
    )
    assert.match(html, /aria-haspopup="dialog"/)
    assert.match(html, /aria-controls="watchlist-tag-manager"/)
    assert.match(html, /min-h-11/)
    assert.doesNotMatch(html, /<dialog/)
  }
  assert.match(
    source,
    /id="watchlist-tag-manager-title"[\s\S]*?>\s*Gérer les tags\s*<\/h2>/,
  )
})

test('mobile watchlist toolbar keeps compact Configuration then Tags beside Mes films, desktop manager unchanged', async () => {
  const source = await readFile(
    new URL('../app/pages/compte/watchlist.vue', import.meta.url),
    'utf8',
  )
  const toolbar = source.slice(
    source.indexOf('<section aria-labelledby="saved-heading">'),
    source.indexOf(
      '<WatchlistPreferences',
      source.indexOf('<section aria-labelledby="saved-heading">'),
    ),
  )
  assert.match(toolbar, /flex items-center justify-between gap-2 lg:hidden/)
  assert.doesNotMatch(toolbar, /flex-wrap|mt-2/)
  assert.match(toolbar, /id="saved-heading"[\s\S]*?>\s*Mes films\s*<\/h2>/)
  assert.match(toolbar, /class="account-secondary size-12 p-0!"/)
  assert.match(toolbar, /aria-label="Configuration"/)
  assert.match(
    toolbar,
    /<Settings2[^>]+aria-hidden="true"\s*\/>(\s*)<\/button>/,
  )
  assert.match(
    toolbar,
    /ref="configurationTrigger"[\s\S]*?<WatchlistTagManager\s+v-if="!isDesktop"\s+compact-trigger/,
  )
  const desktop = source.match(
    /<WatchlistTagManager\s+v-if="isDesktop"[\s\S]*?\/>/,
  )?.[0]
  assert.ok(desktop)
  assert.doesNotMatch(desktop, /compact-trigger/)
})

test('tag manager renders inline icon-only named actions and separates creation from existing tags', async () => {
  const source = await readFile(
    new URL('../app/components/WatchlistTagManager.vue', import.meta.url),
    'utf8',
  )
  const { descriptor } = parse(
    source.replace('const open = ref(false)', 'const open = ref(true)'),
  )
  const managerModule: RowModule = {}
  runInNewContext(
    ts.transpileModule(
      compileScript(descriptor, {
        id: 'WatchlistTagManager',
        inlineTemplate: true,
      }).content,
      {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2022,
        },
      },
    ).outputText,
    {
      exports: managerModule,
      computed,
      ref,
      watch,
      nextTick,
      useTemplateRef: () => ref(null),
      onMounted: () => {},
      onBeforeUnmount: () => {},
      useWatchlist: () => ({ scopeKey: ref(0), error: ref('') }),
      require: (id: string) =>
        id === '~/utils/watchlistTags' ? tagging : require(id),
    },
  )
  assert.ok(managerModule.default)
  const name = 'À revoir au cinéma avec tous les amis'
  for (const blocked of [false, true]) {
    const app = createSSRApp(managerModule.default, {
      tags: [{ id: '1', name, color: 'blue' }],
      ready: true,
      blocked,
    })
    app.component('WatchlistTagColorPicker', { render: () => null })
    const html = await renderToString(app)
    assert.match(
      html,
      /<ul class="mt-4 divide-y divide-ink\/20 border-t border-ink\/20 pt-2">/,
    )
    assert.match(
      html,
      /<div class="flex items-center gap-2"><div class="min-w-0 flex-1">/,
    )
    assert.doesNotMatch(html, /basis-full|sm:basis-auto/)
    for (const label of ['Modifier', 'Supprimer']) {
      const button = html.match(
        new RegExp(
          `<button[^>]*aria-label="${label} ${name}"[^>]*>[\\s\\S]*?<\\/button>`,
        ),
      )?.[0]
      assert.ok(button)
      assert.match(button, /size-11 shrink-0/)
      assert.match(button, /hover:bg-subtle/)
      assert.match(button, /focus-visible:outline-2/)
      assert.match(button, /aria-expanded="false"/)
      assert.match(button, /<svg[^>]*aria-hidden="true"[^>]*focusable="false"/)
      assert.match(button, /width="20" height="20"/)
      assert.doesNotMatch(button.replace(/<[^>]*>/g, ''), /\S/)
      assert.equal(/ disabled(?:=""|(?=[\s>]))/.test(button), blocked)
    }
  }
})

test('saved movie row renders a full French theatrical date in semantic time, not the general year', async () => {
  const html = await renderRow({
    frenchReleaseDate: '1998-10-14',
    releaseDate: '1997-07-24',
  })
  assert.match(html, /<time datetime="1998-10-14"[^>]*>14 octobre 1998<\/time>/)
  assert.doesNotMatch(html, /1997/)
  assert.match(html, /href="\/film\/film-1"/)
  assert.match(html, /Film sauvegardé/)
  assert.match(html, /aria-hidden="true"/)
})

test('external title opens validated TMDB detail in a protected new tab; slug links retain priority', async () => {
  for (const tmdbId of ['1', '999', '9007199254740991']) {
    const html = await renderRow({ slug: undefined, tmdbId })
    assert.match(
      html,
      new RegExp(`href="https://www.themoviedb.org/movie/${tmdbId}"`),
    )
    assert.match(html, /target="_blank"/)
    assert.match(html, /rel="noopener noreferrer"/)
    assert.match(html, /referrerpolicy="no-referrer"/)
    assert.match(html, />Film sauvegardé<\/a>/)
    assert.doesNotMatch(html, /href="\/film\//)
  }
  const local = await renderRow({ tmdbId: '999' })
  assert.match(local, /href="\/film\/film-1"/)
  assert.doesNotMatch(local, /themoviedb|target="_blank"|rel="noopener/)
})

test('invalid or absent external identity renders plain title without a link', async () => {
  for (const tmdbId of [
    undefined,
    '',
    '0',
    '-1',
    '1.5',
    '01',
    '+1',
    '1e3',
    ' 999',
    '999 ',
    '999\n',
    'NaN',
    'Infinity',
    '9007199254740992',
    '999/other',
    '999?query=private',
    'https://example.com',
    'javascript:alert(1)',
  ]) {
    const html = await renderRow({ slug: undefined, tmdbId })
    assert.match(html, /<p class="break-words font-bold">Film sauvegardé<\/p>/)
    assert.doesNotMatch(html, /<a\b|target="_blank"/)
  }
})

test('saved row omits missing, negative and invalid evidence without date placeholder or inferred year', async () => {
  for (const frenchReleaseDate of [
    undefined,
    null,
    '',
    '1998',
    '1998-02-30',
    '2025-02-29',
    '1998-13-14',
    '1998-10-14T00:00:00Z',
    'invalid',
  ]) {
    const html = await renderRow({ frenchReleaseDate })
    assert.doesNotMatch(html, /<time|text-muted|1998|2025|Invalid Date/)
    assert.match(html, /Film sauvegardé/)
  }
  assert.match(
    await renderRow({ frenchReleaseDate: '2000-02-29' }),
    />29 février 2000<\/time>/,
  )
})

test('search rows retain general-date years and saved loop receives no fallback release date', async () => {
  const html = await renderRow({ releaseDate: '1997-07-24' })
  assert.match(html, /<p[^>]*>1997<\/p>/)
  assert.doesNotMatch(html, /<time|juillet/)
  const page = await readFile(
    new URL('../app/pages/compte/watchlist.vue', import.meta.url),
    'utf8',
  )
  const rows = [...page.matchAll(/<WatchlistMovieRow\b[^>]*>/g)].map(
    (match) => match[0],
  )
  assert.equal(rows.length, 3)
  assert.match(rows[1]!, /:tmdb-id="movie.tmdb_id"/)
  for (const row of [rows[0]!, rows[2]!]) assert.doesNotMatch(row, /tmdb-id/)
  for (const row of rows.slice(0, 2)) {
    assert.match(row, /:release-date="movie.release_date"/)
    assert.doesNotMatch(row, /french-release-date/)
  }
  assert.match(rows[2]!, /:french-release-date="movie.french_release_date"/)
  assert.doesNotMatch(rows[2]!, /\s:release-date=/)
})

function session(username = 'alice'): AccountSession {
  return {
    enabled: true,
    state: 'complete',
    account: {
      username,
      email: `${username}@example.test`,
      has_password: true,
      google_linked: false,
    },
  }
}
function value(
  revision = '1',
  slugs = ['film-1'],
  username = 'alice',
  sortOrder: WatchlistSortOrder = 'added_desc',
): AccountWatchlist {
  return {
    username,
    revision,
    sort_order: sortOrder,
    view_mode: 'list',
    filter_tag_id: null,
    tags: [],
    items: slugs.map((slug) => ({
      slug,
      title: slug,
      release_date: '1997-07-24',
      french_release_date: '1998-10-14',
      added_at: '2026-09-01T00:00:00Z',
      tag_ids: [],
    })),
    external_search_available: true,
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
async function settle() {
  await nextTick()
  await new Promise<void>((resolve) => setImmediate(resolve))
}

async function fixture(client = true) {
  const states = new Map()
  const messages: string[] = []
  const app = {
    _accountChannel: {
      postMessage: (message: string) => messages.push(message),
    },
  }
  const posts: SaveWatchlist[] = []
  const sortPosts: SaveWatchlistSort[] = []
  const preferencePosts: SaveWatchlistPreferences[] = []
  const tagPosts: {
    action: string
    input:
      | CreateWatchlistTag
      | UpdateWatchlistTag
      | DeleteWatchlistTag
      | AssignWatchlistTag
  }[] = []
  let tagWrite = async (
    _input:
      | CreateWatchlistTag
      | UpdateWatchlistTag
      | DeleteWatchlistTag
      | AssignWatchlistTag,
  ) => response
  function tag(
    action: string,
    input:
      | CreateWatchlistTag
      | UpdateWatchlistTag
      | DeleteWatchlistTag
      | AssignWatchlistTag,
  ) {
    tagPosts.push({ action, input })
    return tagWrite(input)
  }
  const scopes: ReturnType<typeof effectScope>[] = []
  let response = value()
  let admitted = session()
  let sessionRead = async () => admitted
  let gets = 0
  let read = async () => response
  let write = async (input: SaveWatchlist) => {
    response = {
      ...response,
      revision: String(BigInt(input.expected_revision) + 1n),
      username: input.expected_username,
      items: value('0', input.saved === 'true' ? [input.movie_slug] : []).items,
    }
    return response
  }
  let sort = async (input: SaveWatchlistSort) => {
    response = {
      ...response,
      revision: String(BigInt(input.expected_revision) + 1n),
      sort_order: input.sort_order,
    }
    return response
  }
  let preferences = async (input: SaveWatchlistPreferences) => {
    response = {
      ...response,
      revision: String(BigInt(input.expected_revision) + 1n),
      view_mode: input.view_mode,
      filter_tag_id: input.filter_tag_id,
    }
    return response
  }
  let search = async (): Promise<WatchlistSearch> => ({
    username: 'alice',
    catalog: [],
    external: [{ tmdb_id: '12', title: 'External' }],
    external_status: 'ready',
    catalog_has_more: false,
  })
  let imported = async (): Promise<ImportedWatchlist> => ({
    watchlist: {
      ...response,
      revision: '2',
      items: value('2', ['film-12']).items,
    },
    movie_slug: 'film-12',
  })
  const context = {
    ref,
    computed,
    readonly,
    watch,
    AbortController,
    effectScope: () => {
      const scope = effectScope(true)
      scopes.push(scope)
      return scope
    },
    useState: <T>(key: string, init: () => T) => {
      if (!states.has(key)) states.set(key, ref(init()))
      return states.get(key)
    },
    useNuxtApp: () => app,
    useAccountApi: () => ({
      session: () => sessionRead(),
      watchlist: async () => {
        gets++
        return read()
      },
      saveWatchlist: (input: SaveWatchlist) => {
        posts.push(input)
        return write(input)
      },
      saveWatchlistSort: (input: SaveWatchlistSort) => {
        sortPosts.push(input)
        return sort(input)
      },
      saveWatchlistPreferences: (input: SaveWatchlistPreferences) => {
        preferencePosts.push(input)
        return preferences(input)
      },
      searchWatchlist: () => search(),
      importWatchlist: () => imported(),
      createWatchlistTag: (input: CreateWatchlistTag) => tag('create', input),
      updateWatchlistTag: (input: UpdateWatchlistTag) => tag('update', input),
      deleteWatchlistTag: (input: DeleteWatchlistTag) => tag('delete', input),
      assignWatchlistTag: (input: AssignWatchlistTag) => tag('assign', input),
    }),
    require: () => errors,
  }
  async function compile<T>(name: string, extra = {}): Promise<T> {
    const source = await readFile(
      new URL(`../app/composables/${name}.ts`, import.meta.url),
      'utf8',
    )
    const exports = {}
    runInNewContext(
      ts.transpileModule(
        source.replaceAll('import.meta.client', String(client)),
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      { ...context, ...extra, exports },
    )
    // SAFETY: Export type matches the compiled module supplied by each caller.
    return exports as T
  }
  const account = (
    await compile<{ useAccountSession: typeof useAccountSession }>(
      'useAccountSession',
    )
  ).useAccountSession()
  const module = await compile<{ useWatchlist: typeof useWatchlist }>(
    'useWatchlist',
    { useAccountSession: () => account },
  )
  const list = module.useWatchlist()
  list.startSynchronization()
  return {
    list,
    account,
    posts,
    sortPosts,
    preferencePosts,
    setPreferences: (fn: typeof preferences) => {
      preferences = fn
    },
    tagPosts,
    setTagWrite: (fn: typeof tagWrite) => {
      tagWrite = fn
    },
    messages,
    states,
    another: () => module.useWatchlist(),
    get gets() {
      return gets
    },
    setRead: (fn: typeof read) => {
      read = fn
    },
    setSessionRead: (fn: typeof sessionRead) => {
      sessionRead = fn
    },
    setWrite: (fn: typeof write) => {
      write = fn
    },
    setSort: (fn: typeof sort) => {
      sort = fn
    },
    setSearch: (fn: typeof search) => {
      search = fn
    },
    setImport: (fn: typeof imported) => {
      imported = fn
    },
    setResponse: (next: AccountWatchlist) => {
      response = next
    },
    admit: (next = session()) => {
      admitted = next
      account.accept(next)
    },
    stop: () => scopes.forEach((scope) => scope.stop()),
  }
}

test('watchlist is app scoped, client-only, absent from serialized state and guest storage', async () => {
  const f = await fixture(false)
  try {
    f.admit()
    await settle()
    assert.equal(f.gets, 0)
    assert.equal(f.another(), f.list)
    assert.equal(f.list.ready.value, false)
    assert.equal(await f.list.save('film-1', true), false)
    assert.ok([...f.states.keys()].every((key) => !key.includes('watchlist')))
    const source = await readFile(
      new URL('../app/composables/useWatchlist.ts', import.meta.url),
      'utf8',
    )
    assert.doesNotMatch(
      source,
      /useState|useAsyncData|localStorage|sessionStorage/,
    )
  } finally {
    f.stop()
  }
})

test('preference transport converts null only at strict typed request boundary', async () => {
  const source = await readFile(
    new URL('../app/composables/useAccountApi.ts', import.meta.url),
    'utf8',
  )
  const calls: { url: string; options: FetchOptions<'json'> }[] = []
  const exports: AccountApiExports = {}
  runInNewContext(
    ts.transpileModule(source.replaceAll('import.meta.server', 'false'), {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    }).outputText,
    {
      exports,
      useRuntimeConfig: () => ({}),
      useState: () => ref(false),
      require: (id: string) =>
        id === 'ofetch'
          ? {
              ofetch: async (url: string, options: FetchOptions<'json'>) => {
                calls.push({ url, options })
                return value()
              },
            }
          : errors,
    },
  )
  assert.ok(exports.useAccountApi)
  for (const tagId of [null, '9007199254740993']) {
    await exports.useAccountApi().saveWatchlistPreferences({
      expected_username: 'alice',
      expected_revision: '7',
      view_mode: 'tags',
      filter_tag_id: tagId,
    })
    const call = calls.at(-1)!
    assert.equal(call.url, '/api/v1/account/watchlist/preferences')
    assert.deepEqual(JSON.parse(JSON.stringify(call.options.body)), {
      expected_username: 'alice',
      expected_revision: '7',
      view_mode: 'tags',
      filter_tag_id: tagId ?? '',
    })
    assert.equal(call.options.retry, false)
    assert.equal(call.options.cache, 'no-store')
    assert.equal(call.options.credentials, 'same-origin')
  }
})

test('page composes preference pair from one committed snapshot, never initial/default POST or optimistic controls', async () => {
  const f = await pageFixture()
  try {
    assert.equal(f.preferencePosts.length, 0)
    const next = {
      ...value('7'),
      view_mode: 'tags' as const,
      filter_tag_id: '9007199254740993',
      tags: [{ id: '9007199254740993', name: 'Privé', color: 'blue' as const }],
    }
    f.setResponse(next)
    await f.account.revalidate()
    f.page.openTagEditor.value = 'tag-9007199254740993:film-1'
    const pending = deferred<AccountWatchlist>()
    f.setPreferences(() => pending.promise)
    const select = new TestSelect('')
    const write = f.page.changeFilter({ target: select })
    assert.equal(select.value, '9007199254740993')
    assert.equal(f.page.selectedTag.value, '9007199254740993')
    assert.equal(f.page.displayMode.value, 'tags')
    assert.equal(f.page.openTagEditor.value, 'tag-9007199254740993:film-1')
    assert.equal(f.list.writesBlocked.value, true)
    assert.deepEqual(
      { ...f.preferencePosts[0] },
      {
        expected_username: 'alice',
        expected_revision: '7',
        view_mode: 'tags',
        filter_tag_id: null,
      },
    )
    const committed = { ...next, revision: '8', filter_tag_id: null }
    f.setResponse(committed)
    pending.resolve(committed)
    await write
    assert.equal(f.page.openTagEditor.value, '')
    f.setPreferences(async (input) => ({
      ...committed,
      revision: '9',
      view_mode: input.view_mode,
    }))
    await f.page.changeDisplay('list', { currentTarget: new TestSelect('') })
    assert.deepEqual(
      { ...f.preferencePosts[1] },
      {
        expected_username: 'alice',
        expected_revision: '8',
        view_mode: 'list',
        filter_tag_id: null,
      },
    )
    await f.page.changeDisplay('list', { currentTarget: new TestSelect('') })
    await f.page.changeFilter({ target: new TestSelect('') })
    f.page.clearPageSearch()
    assert.equal(f.preferencePosts.length, 2)
    assert.equal(f.page.displayMode.value, 'list')
    assert.ok(f.messages.every((message) => message === 'watchlist-changed'))
    assert.doesNotMatch(
      JSON.stringify([...f.states].map(([key, state]) => [key, state.value])),
      /view_mode|filter_tag_id|9007199254740993/,
    )
  } finally {
    f.stop()
  }
})

for (const operation of [
  'sort',
  'membership',
  'import',
  'create',
  'update',
  'delete',
  'assign',
  'preferences',
] as const) {
  test(`preferences serialize in both directions with ${operation}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      let pending = deferred<AccountWatchlist>()
      f.setPreferences(() => pending.promise)
      f.setSort(() => pending.promise)
      f.setWrite(() => pending.promise)
      f.setTagWrite(() => pending.promise)
      f.setImport(async () => ({
        watchlist: await pending.promise,
        movie_slug: 'film-1',
      }))
      const other = () =>
        operation === 'sort'
          ? f.list.saveSort('title_asc')
          : operation === 'membership'
            ? f.list.save('film-1', true)
            : operation === 'import'
              ? f.list.importMovie('12')
              : operation === 'create'
                ? f.list.createTag('Tag', 'blue')
                : operation === 'update'
                  ? f.list.updateTag('1', 'Tag', 'red')
                  : operation === 'delete'
                    ? f.list.deleteTag('1')
                    : operation === 'assign'
                      ? f.list.assignTag('film-1', '1', true)
                      : f.list.savePreferences('list', null)
      const first = f.list.savePreferences('tags', '1')
      assert.equal(await other(), false)
      pending.resolve({ ...value('2'), view_mode: 'tags', filter_tag_id: '1' })
      await first
      pending = deferred<AccountWatchlist>()
      const second = other()
      assert.equal(await f.list.savePreferences('list', null), false)
      pending.resolve({ ...value('3'), view_mode: 'tags', filter_tag_id: '1' })
      await second
      assert.equal(f.list.viewMode.value, 'tags')
      assert.equal(f.list.filterTagId.value, '1')
    } finally {
      f.stop()
    }
  })
}

for (const transition of [
  'logout',
  'owner',
  'same-owner',
  'pagehide',
  'offline',
] as const) {
  test(`late preference response cannot resurrect private pair after ${transition}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      const pending = deferred<AccountWatchlist>()
      f.setPreferences(() => pending.promise)
      const write = f.list.savePreferences('tags', '1')
      f.setResponse(value('0', [], transition === 'owner' ? 'bob' : 'alice'))
      if (transition === 'owner' || transition === 'same-owner')
        f.admit(session(transition === 'owner' ? 'bob' : 'alice'))
      else f.account.clear()
      await settle()
      pending.resolve({ ...value('99'), view_mode: 'tags', filter_tag_id: '1' })
      assert.equal(await write, false)
      assert.notEqual(f.list.viewMode.value, 'tags')
      assert.notEqual(f.list.filterTagId.value, '1')
      assert.deepEqual(f.messages, [])
    } finally {
      f.stop()
    }
  })
}

for (const committed of [false, true]) {
  test(`preference ${committed ? 'post-commit timeout' : 'stale CAS'} reads back without replay and gates failed reconciliation`, async () => {
    const f = await pageFixture()
    try {
      const remote = {
        ...value('2'),
        view_mode: 'tags' as const,
        filter_tag_id: '1',
      }
      f.setPreferences(async () => {
        f.setResponse(remote)
        throw new errors.AccountApiError(
          committed ? 0 : 409,
          committed ? '' : 'watchlist_changed',
        )
      })
      const select = new TestSelect('1')
      const write = f.page.changeFilter({ target: select })
      assert.equal(select.value, '')
      await write
      assert.equal(f.page.displayMode.value, 'tags')
      assert.equal(f.page.selectedTag.value, '1')
      assert.ok(f.list.error.value)
      assert.equal(f.preferencePosts.length, 1)
      f.setRead(async () => {
        throw new errors.AccountApiError()
      })
      await f.page.changeDisplay('list', { currentTarget: new TestSelect('') })
      assert.equal(f.list.ready.value, false)
      const rejected = new TestSelect('')
      await f.page.changeFilter({ target: rejected })
      assert.equal(rejected.value, '1')
      assert.equal(
        f.page.selectedTag.value,
        '1',
        'unavailable tags are not deletion authority',
      )
      assert.equal(f.preferencePosts.length, 2)
      f.setRead(async () => remote)
      await f.list.retry()
      f.setRead(async () => value('1'))
      await f.account.revalidate()
      assert.equal(
        f.page.selectedTag.value,
        '1',
        'lower revisions never revert pair',
      )
      assert.equal(f.preferencePosts.length, 2)
    } finally {
      f.stop()
    }
  })
}

test('revalidation waits for preference writer then atomically applies remote pair and deletion without POST', async () => {
  const f = await pageFixture()
  try {
    const pending = deferred<AccountWatchlist>()
    f.setPreferences(() => pending.promise)
    const write = f.list.savePreferences('tags', '1')
    const refresh = f.account.revalidate()
    await settle()
    assert.equal(f.gets, 1)
    f.setResponse({
      ...value('3', [], 'alice', 'title_desc'),
      view_mode: 'tags',
      filter_tag_id: '2',
    })
    pending.resolve({ ...value('2'), view_mode: 'tags', filter_tag_id: '1' })
    await write
    await refresh
    assert.equal(f.page.selectedTag.value, '2')
    assert.equal(f.page.displayMode.value, 'tags')
    f.setResponse({
      ...value('4', [], 'alice', 'title_desc'),
      view_mode: 'tags',
    })
    await f.account.revalidate()
    assert.equal(f.page.selectedTag.value, '')
    assert.equal(f.page.displayMode.value, 'tags')
    assert.equal(f.list.sortOrder.value, 'title_desc')
    assert.equal(f.preferencePosts.length, 1)
  } finally {
    f.stop()
  }
})

for (const control of ['filter', 'mode'] as const) {
  for (const transition of [
    'same-control',
    'owner',
    'departure',
    'unmount',
    'interaction',
    'moved-focus',
    'disabled',
    'hidden',
  ] as const) {
    test(`${control} focus recovery respects ${transition}`, async () => {
      const f = await pageFixture()
      try {
        const pending = deferred<AccountWatchlist>()
        f.setPreferences(() => pending.promise)
        const element = new TestSelect('1')
        f.pageDocument.activeElement = element
        const write =
          control === 'filter'
            ? f.page.changeFilter({ target: element })
            : f.page.changeDisplay('tags', { currentTarget: element })
        f.pageDocument.activeElement = f.pageDocument.body
        if (transition === 'owner') {
          f.setResponse(value('0', [], 'bob'))
          f.admit(session('bob'))
          await settle()
        }
        if (transition === 'departure') f.page.clearPageSearch()
        if (transition === 'unmount') element.isConnected = false
        if (transition === 'interaction') f.page.interactWithTags()
        if (transition === 'moved-focus') f.pageDocument.activeElement = {}
        if (transition === 'disabled') element.disabled = true
        if (transition === 'hidden') element.visible = false
        pending.resolve({
          ...value('2'),
          view_mode: 'tags',
          filter_tag_id: '1',
        })
        await write
        assert.equal(element.focusCalls, transition === 'same-control' ? 1 : 0)
      } finally {
        f.stop()
      }
    })
  }
}

test('four tag operations use shared CAS and committed snapshot without touching movie search', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.list.query.value = 'External'
    await f.list.search()
    const search = JSON.stringify(f.list.searchResults.value)
    let revision = 1
    f.setTagWrite(async () => ({
      ...value(String(++revision)),
      tags: [{ id: '9007199254740993', name: 'Soirée', color: 'blue' }],
    }))
    assert.equal(await f.list.createTag('Soirée', 'blue'), true)
    assert.equal(
      await f.list.updateTag('9007199254740993', 'Amis', 'rose'),
      true,
    )
    assert.equal(
      await f.list.assignTag('film-1', '9007199254740993', true),
      true,
    )
    assert.equal(
      await f.list.assignTag('film-1', '9007199254740993', false),
      true,
    )
    assert.equal(await f.list.deleteTag('9007199254740993'), true)
    assert.deepEqual(
      f.tagPosts.map(({ action, input }) => [action, input.expected_revision]),
      [
        ['create', '1'],
        ['update', '2'],
        ['assign', '3'],
        ['assign', '4'],
        ['delete', '5'],
      ],
    )
    assert.ok(
      f.tagPosts.every(({ input }) => input.expected_username === 'alice'),
    )
    assert.deepEqual(JSON.parse(JSON.stringify(f.tagPosts.slice(0, 2))), [
      {
        action: 'create',
        input: {
          expected_username: 'alice',
          expected_revision: '1',
          name: 'Soirée',
          color: 'blue',
        },
      },
      {
        action: 'update',
        input: {
          expected_username: 'alice',
          expected_revision: '2',
          tag_id: '9007199254740993',
          name: 'Amis',
          color: 'rose',
        },
      },
    ])
    assert.equal(JSON.stringify(f.list.searchResults.value), search)
    assert.equal(f.list.query.value, 'External')
    assert.ok(f.messages.every((message) => message === 'watchlist-changed'))
    assert.doesNotMatch(
      JSON.stringify([...f.states].map(([key, state]) => [key, state.value])),
      /Soirée|9007199254740993|tag_ids/,
    )
  } finally {
    f.stop()
  }
})

test('tag writes serialize sort, membership, import and other tags; revalidation waits', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const pending = deferred<AccountWatchlist>()
    f.setTagWrite(() => pending.promise)
    const save = f.list.createTag('Amis', 'neutral')
    assert.equal(f.list.tags.value.length, 0)
    assert.equal(await f.list.saveSort('title_asc'), false)
    assert.equal(await f.list.save('film-2', true), false)
    assert.equal(await f.list.importMovie('12'), false)
    assert.equal(await f.list.createTag('Autre', 'red'), false)
    const refresh = f.account.revalidate()
    await settle()
    assert.equal(f.gets, 1)
    const next: AccountWatchlist = {
      ...value('2'),
      tags: [{ id: '1', name: 'Amis', color: 'neutral' }],
    }
    f.setResponse(next)
    pending.resolve(next)
    await save
    await refresh
    assert.equal(f.list.tags.value[0]?.name, 'Amis')
    assert.equal(f.tagPosts.length, 1)
    for (const operation of ['sort', 'membership', 'import'] as const) {
      const held = deferred<AccountWatchlist>()
      f.setSort(() => held.promise)
      f.setWrite(() => held.promise)
      f.setImport(async () => ({
        watchlist: await held.promise,
        movie_slug: 'film-1',
      }))
      const write =
        operation === 'sort'
          ? f.list.saveSort('title_asc')
          : operation === 'membership'
            ? f.list.save('film-1', true)
            : f.list.importMovie('12')
      assert.equal(await f.list.assignTag('film-1', '1', true), false)
      held.resolve(next)
      await write
    }
  } finally {
    f.stop()
  }
})

for (const transition of ['logout', 'owner', 'same-owner'] as const) {
  test(`late tag response cannot resurrect private state after ${transition}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      const pending = deferred<AccountWatchlist>()
      f.setTagWrite(() => pending.promise)
      const save = f.list.createTag('Private tag', 'violet')
      f.setResponse(value('0', [], transition === 'owner' ? 'bob' : 'alice'))
      if (transition === 'logout') f.account.clear()
      else f.admit(session(transition === 'owner' ? 'bob' : 'alice'))
      await settle()
      pending.resolve({
        ...value('99'),
        tags: [{ id: '1', name: 'Private tag', color: 'violet' }],
      })
      assert.equal(await save, false)
      assert.equal(f.list.tags.value.length, 0)
      assert.deepEqual(f.messages, [])
    } finally {
      f.stop()
    }
  })
}

test('tag timeout after commit reconciles without replay; failed readback blocks all writers', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const next: AccountWatchlist = {
      ...value('2'),
      tags: [{ id: '1', name: 'Amis', color: 'teal' }],
    }
    f.setTagWrite(async () => {
      f.setResponse(next)
      throw new errors.AccountApiError()
    })
    assert.equal(await f.list.createTag('Amis', 'teal'), false)
    assert.equal(f.list.tags.value[0]?.name, 'Amis')
    assert.equal(f.tagPosts.length, 1)
    f.setRead(async () => {
      throw new errors.AccountApiError()
    })
    await f.list.deleteTag('1')
    assert.equal(f.list.ready.value, false)
    assert.equal(await f.list.createTag('New', 'neutral'), false)
    f.setRead(async () => next)
    await f.list.retry()
    assert.equal(f.list.ready.value, true)
    assert.equal(f.tagPosts.length, 2)
    f.setRead(async () => ({ ...value('1'), tags: [] }))
    await f.account.revalidate()
    assert.equal(f.list.tags.value.length, 1)
    f.setRead(async () => ({
      ...next,
      tags: [{ id: '1', name: 'Renommé', color: 'rose' }],
    }))
    await f.account.revalidate()
    assert.equal(f.list.tags.value[0]?.name, 'Renommé')
    assert.equal(f.list.tags.value[0]?.color, 'rose')
  } finally {
    f.stop()
  }
})

test('committed filter combines all sorts, keeps renamed ID, follows deletion snapshot and survives page cleanup', async () => {
  const f = await pageFixture()
  try {
    const next = value('2', ['film-1', 'film-2', 'film-3'])
    next.tags = [
      { id: '1', name: 'Amis', color: 'neutral' },
      { id: '2', name: 'Vide', color: 'blue' },
    ]
    next.items[0]!.tag_ids = ['1']
    next.items[2]!.tag_ids = ['1']
    next.filter_tag_id = '1'
    f.setResponse(next)
    await f.account.revalidate()
    const gets = f.gets
    for (const option of sorting.watchlistSortOptions) {
      const changed = { ...next, sort_order: option.value }
      f.setResponse(changed)
      await f.account.revalidate()
      assert.deepEqual(
        f.page.sortedItems.value.map((item) => item.slug),
        sorting
          .sortWatchlistItems(
            changed.items.filter((item) => item.tag_ids.includes('1')),
            option.value,
          )
          .map((item) => item.slug),
      )
    }
    assert.equal(f.gets, gets + 6)
    assert.equal(f.tagPosts.length, 0)
    f.setResponse({
      ...next,
      tags: [
        { id: '1', name: 'Renommé', color: 'green' },
        { id: '2', name: 'Vide', color: 'blue' },
      ],
    })
    await f.account.revalidate()
    assert.equal(f.page.selectedTag.value, '1')
    f.setResponse({ ...next, filter_tag_id: '2' })
    await f.account.revalidate()
    assert.equal(f.page.sortedItems.value.length, 0)
    f.setResponse({ ...next, tags: [], filter_tag_id: null })
    await f.account.revalidate()
    assert.equal(f.page.selectedTag.value, '')
    f.setResponse(next)
    await f.account.revalidate()
    f.page.openTagEditor.value = 'film-1'
    f.page.clearPageSearch()
    assert.equal(f.page.selectedTag.value, '1')
    assert.equal(f.page.openTagEditor.value, '')
  } finally {
    f.stop()
  }
})

test('revalidation acquires, replaces and removes French evidence at equal membership revision in private memory only', async () => {
  const f = await fixture()
  try {
    const initial = value()
    delete initial.items[0]!.french_release_date
    f.setResponse(initial)
    f.admit()
    await settle()
    assert.equal(f.list.items.value[0]?.french_release_date, undefined)
    for (const date of ['1998-10-14', '1998-10-07', undefined]) {
      const updated = value()
      if (date) updated.items[0]!.french_release_date = date
      else delete updated.items[0]!.french_release_date
      f.setResponse(updated)
      await f.account.revalidate()
      assert.equal(f.list.ready.value, true)
      assert.equal(f.list.items.value[0]?.french_release_date, date)
      assert.equal(f.list.items.value[0]?.release_date, '1997-07-24')
      assert.equal(f.list.items.value[0]?.added_at, initial.items[0]?.added_at)
      assert.deepEqual([...f.list.slugs.value], ['film-1'])
      assert.doesNotMatch(
        JSON.stringify([...f.states].map(([key, state]) => [key, state.value])),
        /french_release_date|1998-10|film-1|added_at/,
      )
    }
    assert.equal(f.gets, 4)
    assert.equal(f.posts.length, 0)
    assert.deepEqual(f.messages, [])
    // Metadata refresh keeps membership CAS unchanged for the next real write.
    await f.list.save('film-2', true)
    assert.equal(f.posts[0]?.expected_revision, '1')
    assert.deepEqual(f.messages, ['watchlist-changed'])
    f.account.clear()
    assert.equal(f.list.items.value.length, 0)
  } finally {
    f.stop()
  }
})

for (const transition of ['logout', 'owner'] as const) {
  test(`late French evidence revalidation is discarded after ${transition}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      const pending = deferred<AccountWatchlist>()
      f.setRead(() => pending.promise)
      const refresh = f.account.revalidate()
      await settle()
      f.setRead(async () => value('1', [], 'bob'))
      if (transition === 'logout') f.account.clear()
      else f.admit(session('bob'))
      await settle()
      pending.resolve(value())
      await refresh
      await settle()
      assert.equal(f.list.items.value.length, 0)
      assert.equal(f.list.owner.value, transition === 'logout' ? '' : 'bob')
      assert.deepEqual(f.messages, [])
    } finally {
      f.stop()
    }
  })
}

test('committed snapshots alone change saved status and writes are serialized', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const pending = deferred<AccountWatchlist>()
    f.setWrite(() => pending.promise)
    const save = f.list.save('film-2', true)
    assert.equal(f.list.saving.value, true)
    assert.equal(f.list.slugs.value.has('film-2'), false)
    assert.equal(await f.list.save('film-3', true), false)
    pending.resolve(value('2', ['film-1', 'film-2']))
    assert.equal(await save, true)
    assert.equal(f.list.slugs.value.has('film-2'), true)
    assert.deepEqual(f.messages, ['watchlist-changed'])
    assert.equal(f.posts.length, 1)
    assert.equal(f.posts[0]?.saved, 'true')
  } finally {
    f.stop()
  }
})

for (const transition of ['logout', 'owner', 'same-owner'] as const) {
  test(`late searches cannot resurrect state after ${transition}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      const pending = deferred<WatchlistSearch>()
      f.setSearch(() => pending.promise)
      f.list.query.value = 'Private query'
      const search = f.list.search()
      f.setResponse(value('3', [], transition === 'owner' ? 'bob' : 'alice'))
      if (transition === 'logout') f.account.clear()
      else f.admit(session(transition === 'owner' ? 'bob' : 'alice'))
      await settle()
      pending.resolve({
        username: 'alice',
        catalog: [],
        external: [],
        external_status: 'ready',
        catalog_has_more: false,
      })
      await search
      assert.equal(f.list.query.value, '')
      assert.equal(f.list.searchResults.value, null)
      assert.deepEqual([...f.list.items.value], [])
      assert.equal(f.list.searching.value, false)
    } finally {
      f.stop()
    }
  })

  test(`late initial reads cannot resurrect state after ${transition}`, async () => {
    const f = await fixture()
    try {
      const pending = deferred<AccountWatchlist>()
      f.setRead(() => pending.promise)
      f.admit()
      await settle()
      assert.equal(f.list.loading.value, true)
      f.setRead(async () =>
        value('3', [], transition === 'owner' ? 'bob' : 'alice'),
      )
      if (transition === 'logout') f.account.clear()
      else f.admit(session(transition === 'owner' ? 'bob' : 'alice'))
      await settle()
      pending.resolve(value('99', ['private-old-film']))
      await settle()
      assert.deepEqual([...f.list.items.value], [])
      assert.equal(f.list.loading.value, false)
      assert.equal(f.list.ready.value, transition !== 'logout')
    } finally {
      f.stop()
    }
  })
}

test('late mutations are discarded across owners and do not unblock a newer write', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const old = deferred<AccountWatchlist>()
    f.setWrite(() => old.promise)
    const first = f.list.save('film-2', true)
    f.setResponse(value('0', [], 'bob'))
    f.admit(session('bob'))
    await settle()
    const next = deferred<AccountWatchlist>()
    f.setWrite(() => next.promise)
    const second = f.list.save('film-3', true)
    old.resolve(value('2', ['film-2']))
    await first
    assert.equal(f.list.saving.value, true)
    assert.deepEqual([...f.list.items.value], [])
    next.resolve(value('1', ['film-3'], 'bob'))
    await second
    assert.deepEqual([...f.list.slugs.value], ['film-3'])
  } finally {
    f.stop()
  }
})

test('conflicts and ambiguous writes reconcile without replay, including committed timeout', async () => {
  for (const code of [
    new errors.AccountApiError(409, 'watchlist_changed'),
    new errors.AccountApiError(),
  ]) {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      f.setWrite(async () => {
        f.setResponse(value('2', ['film-2']))
        throw code
      })
      assert.equal(await f.list.save('film-2', true), false)
      assert.deepEqual([...f.list.slugs.value], ['film-2'])
      assert.equal(f.posts.length, 1)
      assert.equal(f.list.ready.value, true)
      assert.ok(f.list.error.value)
    } finally {
      f.stop()
    }
  }
})

test('failed reconciliation gates results and writes until explicit retry succeeds', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.setWrite(async () => {
      throw new errors.AccountApiError()
    })
    f.setRead(async () => {
      throw new errors.AccountApiError(503, 'watchlist_unavailable')
    })
    await f.list.save('film-2', true)
    assert.equal(f.list.ready.value, false)
    assert.equal(await f.list.save('film-2', true), false)
    f.setRead(async () => value('2', ['film-2']))
    await f.list.retry()
    assert.equal(f.list.ready.value, true)
    assert.equal(f.posts.length, 1)
  } finally {
    f.stop()
  }
})

test('same-owner held session and watchlist reads retain display readiness but reject every write', async () => {
  const f = await pageFixture()
  try {
    const next = {
      ...value('7', ['film-1'], 'alice', 'title_desc'),
      view_mode: 'tags' as const,
      filter_tag_id: '1',
      tags: [{ id: '1', name: 'Amis', color: 'blue' as const }],
    }
    next.items[0]!.tag_ids = ['1']
    f.setResponse(next)
    await f.account.revalidate()
    const rows = f.list.items.value
    const heldSession = deferred<AccountSession>()
    const heldRead = deferred<AccountWatchlist>()
    f.setSessionRead(() => heldSession.promise)
    f.setRead(() => heldRead.promise)
    const refresh = f.account.revalidate()
    const assertHeld = async () => {
      assert.equal(f.list.ready.value, true)
      assert.equal(f.list.items.value, rows)
      assert.equal(f.page.selectedTag.value, '1')
      assert.equal(f.page.displayMode.value, 'tags')
      assert.equal(f.list.sortOrder.value, 'title_desc')
      assert.equal(f.list.writesBlocked.value, true)
      assert.equal(await f.list.save('film-1', false), false)
      assert.equal(await f.list.saveSort('added_desc'), false)
      assert.equal(await f.list.savePreferences('list', null), false)
      assert.equal(await f.list.createTag('Autre', 'red'), false)
      assert.equal(await f.list.updateTag('1', 'Autre', 'red'), false)
      assert.equal(await f.list.deleteTag('1'), false)
      assert.equal(await f.list.assignTag('film-1', '1', false), false)
      assert.equal(await f.list.importMovie('12'), false)
      await f.list.search()
      assert.equal(
        f.posts.length +
          f.sortPosts.length +
          f.preferencePosts.length +
          f.tagPosts.length,
        0,
      )
    }
    await settle()
    await assertHeld()
    heldSession.resolve(session())
    await settle()
    await assertHeld()
    heldRead.resolve(next)
    await refresh
    assert.equal(f.list.ready.value, true)
    assert.equal(f.list.writesBlocked.value, false)
    assert.deepEqual(f.messages, [])
  } finally {
    f.stop()
  }
})

test('focus revalidation waits for writes and reads committed state under new session revision', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const pending = deferred<AccountWatchlist>()
    f.setWrite(() => pending.promise)
    const save = f.list.save('film-2', true)
    const refresh = f.account.revalidate()
    await settle()
    assert.equal(f.list.ready.value, true)
    assert.equal(f.list.writesBlocked.value, true)
    f.setResponse(value('2', ['film-2']))
    pending.resolve(value('2', ['film-2']))
    await save
    await refresh
    assert.deepEqual([...f.list.slugs.value], ['film-2'])
    assert.equal(f.list.writesBlocked.value, false)
  } finally {
    f.stop()
  }
})

test('external import replaces candidate with canonical link only after committed success', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.list.query.value = 'External'
    await f.list.search()
    const pending = deferred<ImportedWatchlist>()
    f.setImport(() => pending.promise)
    const save = f.list.importMovie('12')
    assert.equal(f.list.searchResults.value?.catalog.length, 0)
    pending.resolve({
      watchlist: value('2', ['film-12']),
      movie_slug: 'film-12',
    })
    assert.equal(await save, 'film-12')
    assert.equal(f.list.searchResults.value?.catalog[0]?.slug, 'film-12')
    assert.equal(f.list.searchResults.value?.external.length, 0)
  } finally {
    f.stop()
  }
})

test('route departure clears query and fences pending search/import result replacement', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const pending = deferred<WatchlistSearch>()
    f.setSearch(() => pending.promise)
    f.list.query.value = 'External'
    const search = f.list.search()
    f.list.clearSearch()
    pending.resolve({
      username: 'alice',
      catalog: [],
      external: [{ tmdb_id: '12', title: 'External' }],
      external_status: 'ready',
      catalog_has_more: false,
    })
    await search
    assert.equal(f.list.searchResults.value, null)
    assert.equal(f.list.query.value, '')
  } finally {
    f.stop()
  }
})

test('recoverable search failure preserves query; unavailable provider preserves local results', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.list.query.value = 'Private query'
    f.setSearch(async () => {
      throw new errors.AccountApiError(503)
    })
    await f.list.search()
    assert.equal(f.list.query.value, 'Private query')
    assert.ok(f.list.searchError.value)
    f.setSearch(async () => ({
      username: 'alice',
      catalog: [{ slug: 'film-1', title: 'Local' }],
      external: [],
      external_status: 'unavailable',
      catalog_has_more: false,
    }))
    await f.list.search()
    assert.equal(f.list.searchResults.value?.catalog.length, 1)
    assert.equal(f.list.searchResults.value?.external_status, 'unavailable')
  } finally {
    f.stop()
  }
})

test('dismissal keeps query but fences pending results without ending a newer search', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const old = deferred<WatchlistSearch>()
    const newer = deferred<WatchlistSearch>()
    f.setSearch(() => old.promise)
    f.list.query.value = 'Private query'
    const first = f.list.search()
    f.list.dismissSearch()
    assert.equal(f.list.query.value, 'Private query')
    assert.equal(f.list.searching.value, false)
    f.setSearch(() => newer.promise)
    const second = f.list.search()
    old.resolve({
      username: 'alice',
      catalog: [],
      external: [],
      external_status: 'ready',
      catalog_has_more: false,
    })
    await first
    assert.equal(f.list.searchResults.value, null)
    assert.equal(f.list.searching.value, true)
    newer.resolve({
      username: 'alice',
      catalog: [{ slug: 'film-2', title: 'New' }],
      external: [],
      external_status: 'ready',
      catalog_has_more: false,
    })
    await second
    assert.equal(f.list.searchResults.value?.catalog[0]?.slug, 'film-2')
  } finally {
    f.stop()
  }
})

class TestSelect {
  value: string
  isConnected = true
  disabled = false
  visible = true
  focusCalls = 0
  constructor(value: string) {
    this.value = value
  }
  focus() {
    this.focusCalls++
  }
  checkVisibility() {
    return this.visible
  }
}

interface PageInteractions {
  selectedTag: ReturnType<typeof computed<string>>
  displayMode: ReturnType<typeof computed<WatchlistViewMode | undefined>>
  changeFilter: (event: { target: unknown }) => Promise<void> | undefined
  changeDisplay: (
    mode: WatchlistViewMode,
    event: { currentTarget: unknown },
  ) => Promise<void> | undefined
  clearFilter: (event: { currentTarget: unknown }) => Promise<void> | undefined
  interactWithTags: () => void
  savedSections: ReturnType<typeof computed<grouping.WatchlistGroup[]>>
  openTagEditor: ReturnType<typeof ref<string>>
  assignTag: (
    slug: string,
    tagId: string,
    assigned: boolean,
    input: TestSelect,
  ) => Promise<void>
  sortedItems: ReturnType<typeof computed<AccountWatchlist['items']>>
  changeSort: (event: { target: unknown }) => Promise<void>
  panelOpen: ReturnType<typeof ref<boolean>>
  activeTab: ReturnType<typeof ref<string>>
  panelHeight: ReturnType<typeof ref<number>>
  panelTop: ReturnType<typeof ref<number>>
  panelBottom: ReturnType<typeof ref<number>>
  positionPanel: () => void
  submitSearch: () => Promise<void>
  openSearch: () => Promise<void>
  openConfiguration: () => Promise<void>
  closeConfiguration: (restoreFocus?: boolean) => void
  configurationOpen: ReturnType<typeof ref<boolean>>
  isDesktop: ReturnType<typeof ref<boolean>>
  breakpointChanged: () => void
  selectTab: (tab: string) => void
  tabKeydown: (event: { key: string; preventDefault: () => void }) => void
  addMovie: (movie: { slug: string } | { tmdb_id: string }) => Promise<void>
  dismiss: (restoreFocus?: boolean) => void
  clearPageSearch: () => void
  removalTarget: ReturnType<typeof ref<{ slug: string; title: string } | null>>
  removing: ReturnType<typeof ref<boolean>>
  removalAttempted: ReturnType<typeof ref<boolean>>
  openRemoval: (
    movie: { slug: string; title: string },
    event: { currentTarget: unknown },
  ) => Promise<void>
  closeRemoval: (restoreFocus?: boolean) => void
  confirmRemoval: () => Promise<void>
}

interface PageDocument {
  activeElement: unknown
  body: { style: { overflow: string } }
  removeEventListener: () => void
  addEventListener: () => void
  querySelector: () => TestSelect | null
}

// Exercise the page's interaction handlers with the real fenced composable.
async function pageFixture() {
  const f = await fixture()
  f.admit()
  await settle()
  const source = await readFile(
    new URL('../app/pages/compte/watchlist.vue', import.meta.url),
    'utf8',
  )
  const elements = new Map<string, ReturnType<typeof ref>>()
  let focused = ''
  const scope = effectScope()
  const callbacks: (() => void)[] = []
  const mounted: (() => void)[] = []
  const viewport = {
    height: 844,
    offsetTop: 0,
    addEventListener: () => {},
    removeEventListener: () => {},
  }
  const media = {
    matches: true,
    addEventListener: () => {},
    removeEventListener: () => {},
  }
  const pageDocument: PageDocument = {
    activeElement: null,
    body: { style: { overflow: 'auto' } },
    removeEventListener: () => {},
    addEventListener: () => {},
    querySelector: () => null,
  }
  // SAFETY: The transpiled page below exports exactly this interaction contract before use.
  const exports = {} as PageInteractions
  const script = source.match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )?.[1]
  assert.ok(script)
  scope.run(() =>
    runInNewContext(
      ts.transpileModule(
        `${script}\nexport { selectedTag, displayMode, changeFilter, changeDisplay, clearFilter, interactWithTags, savedSections, openTagEditor, assignTag, sortedItems, changeSort, panelOpen, activeTab, panelHeight, panelTop, panelBottom, positionPanel, openSearch, openConfiguration, closeConfiguration, configurationOpen, isDesktop, breakpointChanged, submitSearch, selectTab, tabKeydown, addMovie, dismiss, clearPageSearch, removalTarget, removing, removalAttempted, openRemoval, closeRemoval, confirmRemoval }`,
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      {
        exports,
        require: (id: string) =>
          id === '~/utils/watchlistSort'
            ? sorting
            : id === '~/utils/watchlistTags'
              ? tagging
              : id === '~/utils/watchlistGrouping'
                ? grouping
                : errors,
        computed,
        HTMLSelectElement: TestSelect,
        HTMLButtonElement: TestSelect,
        ref,
        watch,
        nextTick,
        useWatchlist: () => f.list,
        useAccountSession: () => f.account,
        useHead: () => {},
        definePageMeta: () => {},
        onMounted: (fn: () => void) => mounted.push(fn),
        onBeforeUnmount: (fn: () => void) => callbacks.push(fn),
        onBeforeRouteLeave: () => {},
        useTemplateRef: (name: string) => {
          const element = ref({
            isConnected: true,
            disabled: false,
            checkVisibility: () => true,
            showModal: () => {},
            close: () => {},
            focusFilter: () => {
              focused = 'tagFilter'
            },
            focus: () => {
              focused = name
            },
            scrollTop: 50,
            getBoundingClientRect: () => ({ top: 500 }),
          })
          elements.set(name, element)
          return element
        },
        window: {
          innerHeight: 844,
          visualViewport: viewport,
          addEventListener: () => {},
          removeEventListener: () => {},
          matchMedia: () => media,
        },
        document: pageDocument,
      },
    ),
  )
  mounted.forEach((fn) => fn())
  return {
    ...f,
    page: exports,
    pageDocument,
    get gets() {
      return f.gets
    },
    elements,
    media,
    viewport,
    get focused() {
      return focused
    },
    stop() {
      callbacks.forEach((fn) => fn())
      scope.stop()
      f.stop()
    },
  }
}

test('watchlist-only cross opens guarded confirmation; cancellation never mutates', async () => {
  const f = await pageFixture()
  try {
    const source = await readFile(
      new URL('../app/pages/compte/watchlist.vue', import.meta.url),
      'utf8',
    )
    assert.match(
      source,
      /<button\s[^>]*data-watchlist-remove[^>]*class="[^"]*size-11[^"]*\bself-center\b[^"]*"[^>]*aria-label="Retirer de la watchlist"[^>]*>\s*<X :size="20"/,
    )
    assert.doesNotMatch(source, /<WatchlistButton/)
    assert.match(source, /@cancel.prevent="closeRemoval\(\)"/)
    const opener = new TestSelect('')
    f.page.openTagEditor.value = 'list:film-1'
    await f.page.openSearch()
    await f.page.openRemoval(
      { slug: 'film-1', title: 'Titre privé' },
      { currentTarget: opener },
    )
    assert.equal(f.page.panelOpen.value, false)
    assert.equal(f.page.openTagEditor.value, '')
    assert.equal(f.focused, 'removalCancel')
    assert.equal(f.pageDocument.body.style.overflow, 'hidden')
    assert.equal(f.posts.length, 0)
    f.page.closeRemoval()
    await settle()
    assert.equal(opener.focusCalls, 1)
    assert.equal(f.page.removalTarget.value, null)
    assert.equal(f.pageDocument.body.style.overflow, 'auto')
    assert.equal(f.posts.length, 0)
  } finally {
    f.stop()
  }
})

test('one explicit confirmation dispatches existing removal once and restores surviving focus', async () => {
  const f = await pageFixture()
  try {
    const opener = new TestSelect('')
    await f.page.openRemoval(
      { slug: 'film-1', title: 'Titre privé' },
      { currentTarget: opener },
    )
    const pending = deferred<AccountWatchlist>()
    f.setWrite(() => pending.promise)
    const save = f.page.confirmRemoval()
    await f.page.confirmRemoval()
    assert.equal(f.posts.length, 1)
    assert.equal(f.posts[0]?.saved, 'false')
    assert.equal(f.page.removing.value, true)
    opener.isConnected = false
    const surviving = new TestSelect('')
    f.pageDocument.querySelector = () => surviving
    pending.resolve(value('2', []))
    await save
    await settle()
    assert.equal(f.page.removalTarget.value, null)
    assert.equal(surviving.focusCalls, 1)
    assert.equal(f.pageDocument.body.style.overflow, 'auto')
  } finally {
    f.stop()
  }
})

for (const committed of [false, true]) {
  test(`uncertain removal readback ${committed ? 'removed' : 'retained'} target never replays; fresh intent required`, async () => {
    const f = await pageFixture()
    try {
      const opener = new TestSelect('')
      await f.page.openRemoval(
        { slug: 'film-1', title: 'Titre privé' },
        { currentTarget: opener },
      )
      f.setWrite(async () => {
        f.setResponse(value('2', committed ? [] : ['film-1']))
        throw new errors.AccountApiError()
      })
      await f.page.confirmRemoval()
      assert.ok(f.page.removalTarget.value)
      assert.ok(f.list.error.value)
      assert.equal(f.page.removalAttempted.value, true)
      assert.equal(f.focused, 'removalCancel')
      await f.page.confirmRemoval()
      await f.list.retry()
      await f.page.confirmRemoval()
      assert.equal(f.posts.length, 1)
      f.page.closeRemoval()
      await settle()
      await f.page.openRemoval(
        { slug: 'film-1', title: 'Titre privé' },
        { currentTarget: opener },
      )
      if (committed) assert.equal(f.page.removalTarget.value, null)
      else {
        assert.equal(f.page.removalAttempted.value, false)
        f.setWrite(async () => value('3', []))
        await f.page.confirmRemoval()
        assert.equal(f.posts.length, 2)
        await settle()
        assert.equal(f.page.removalTarget.value, null)
      }
    } finally {
      f.stop()
    }
  })
}

test('external target disappearance cancels confirmation without dispatch; disconnected opener uses add fallback', async () => {
  const f = await pageFixture()
  try {
    const opener = new TestSelect('')
    await f.page.openRemoval(
      { slug: 'film-1', title: 'Titre privé' },
      { currentTarget: opener },
    )
    opener.isConnected = false
    f.setResponse(value('2', []))
    await f.account.revalidate()
    await settle()
    assert.equal(f.page.removalTarget.value, null)
    await f.page.confirmRemoval()
    assert.equal(f.posts.length, 0)
    assert.equal(f.focused, 'addTrigger')
  } finally {
    f.stop()
  }
})

for (const boundary of [
  'owner',
  'departure',
  'unmount',
  'cancel-and-reopen',
] as const) {
  test(`late removal completion cannot resurrect/refocus at ${boundary} boundary`, async () => {
    const f = await pageFixture()
    const opener = new TestSelect('')
    try {
      await f.page.openRemoval(
        { slug: 'film-1', title: 'Titre privé' },
        { currentTarget: opener },
      )
      const pending = deferred<AccountWatchlist>()
      f.setWrite(() => pending.promise)
      const save = f.page.confirmRemoval()
      if (boundary === 'owner') f.admit('bob')
      else if (boundary === 'unmount') f.stop()
      else f.page.clearPageSearch()
      assert.equal(f.page.removalTarget.value, null)
      assert.equal(f.pageDocument.body.style.overflow, 'auto')
      pending.resolve(value('2', []))
      await save
      await settle()
      assert.equal(f.page.removalTarget.value, null)
      assert.equal(opener.focusCalls, 0)
      if (boundary === 'cancel-and-reopen') {
        f.setResponse(value('3', ['film-2']))
        await f.account.revalidate()
        await f.page.openRemoval(
          { slug: 'film-2', title: 'Nouveau titre' },
          { currentTarget: opener },
        )
        assert.equal(f.page.removalTarget.value?.title, 'Nouveau titre')
      }
    } finally {
      if (boundary !== 'unmount') f.stop()
    }
  })
}

test('grouped page keeps one rendered picker identity and closes it on view/filter changes', async () => {
  const f = await pageFixture()
  try {
    const snapshot = value('2')
    snapshot.tags = [
      { id: '1', name: 'Amis', color: 'blue' },
      { id: '2', name: 'Famille', color: 'rose' },
    ]
    snapshot.items[0]!.tag_ids = ['1', '2']
    f.setResponse(snapshot)
    await f.account.revalidate()
    assert.equal(f.page.displayMode.value, 'list')
    await f.page.changeDisplay('tags', { currentTarget: new TestSelect('') })
    await nextTick()
    assert.deepEqual(
      f.page.savedSections.value.map((section) => section.id),
      ['tag-1', 'tag-2'],
    )
    f.page.openTagEditor.value = 'tag-1:film-1'
    await f.page.changeFilter({ target: new TestSelect('2') })
    await nextTick()
    assert.equal(f.page.openTagEditor.value, '')
    assert.deepEqual(
      f.page.savedSections.value.map((section) => section.id),
      ['tag-2'],
    )
    f.page.openTagEditor.value = 'tag-2:film-1'
    await f.page.changeDisplay('list', { currentTarget: new TestSelect('') })
    await nextTick()
    assert.equal(f.page.openTagEditor.value, '')
    assert.equal(f.page.selectedTag.value, '2')
    assert.equal(f.tagPosts.length, 0)
    assert.equal(f.sortPosts.length, 0)
  } finally {
    f.stop()
  }
})

for (const transition of [
  'removed-instance',
  'retained-instance',
  'view',
  'filter',
  'owner',
  'departure',
  'moved-focus',
] as const) {
  test(`grouped assignment restores focus only within valid rendered instance: ${transition}`, async () => {
    const f = await pageFixture()
    try {
      const snapshot = value('2')
      snapshot.tags = [
        { id: '1', name: 'Amis', color: 'blue' },
        { id: '2', name: 'Famille', color: 'rose' },
      ]
      snapshot.items[0]!.tag_ids = ['1', '2']
      snapshot.view_mode = 'tags'
      f.setResponse(snapshot)
      await f.account.revalidate()
      await nextTick()
      f.page.openTagEditor.value = 'tag-1:film-1'
      const input = new TestSelect('1')
      f.pageDocument.activeElement = input
      const pending = deferred<AccountWatchlist>()
      f.setTagWrite(() => pending.promise)
      const removing = transition !== 'retained-instance'
      const write = f.page.assignTag(
        'film-1',
        removing ? '1' : '2',
        false,
        input,
      )
      // Native disabled controls lose focus; the DOM unmounts only the removed copy.
      f.pageDocument.activeElement = f.pageDocument.body
      input.isConnected = !removing
      if (transition === 'view' || transition === 'filter')
        f.page.interactWithTags()
      if (transition === 'owner') f.admit(session('bob'))
      if (transition === 'departure') f.page.clearPageSearch()
      if (transition === 'moved-focus') f.pageDocument.activeElement = {}
      await nextTick()
      const committed = {
        ...snapshot,
        revision: '3',
        items: snapshot.items.map((item) => ({
          ...item,
          tag_ids: [removing ? '2' : '1'],
        })),
      }
      if (transition === 'view') committed.view_mode = 'list'
      if (transition === 'filter') committed.filter_tag_id = '2'
      pending.resolve(committed)
      await write
      assert.equal(f.tagPosts.length, 1)
      if (transition === 'removed-instance') {
        assert.equal(f.page.openTagEditor.value, '')
        assert.equal(f.focused, 'tagFilter')
        assert.deepEqual(
          f.page.savedSections.value.map((section) => section.id),
          ['tag-2'],
        )
      } else if (transition === 'retained-instance') {
        assert.equal(f.page.openTagEditor.value, 'tag-1:film-1')
        assert.equal(input.focusCalls, 1)
        assert.equal(f.focused, '')
      } else {
        assert.equal(input.focusCalls, 0)
        assert.equal(f.focused, '')
        assert.equal(f.page.openTagEditor.value, '')
      }
      if (transition === 'departure')
        assert.equal(f.page.displayMode.value, 'tags')
    } finally {
      f.stop()
    }
  })
}

test('configuration is mobile-only, saves immediately, restores trigger and closes on breakpoint', async () => {
  const f = await pageFixture()
  try {
    await f.page.openConfiguration()
    assert.equal(f.page.configurationOpen.value, false)
    f.media.matches = false
    f.page.breakpointChanged()
    await f.page.openConfiguration()
    assert.equal(f.page.configurationOpen.value, true)
    assert.equal(f.focused, 'configurationClose')
    assert.equal(f.pageDocument.body.style.overflow, 'hidden')
    await f.page.changeDisplay('tags', { currentTarget: new TestSelect('') })
    assert.equal(f.page.displayMode.value, 'tags')
    assert.equal(f.page.configurationOpen.value, true)
    f.page.closeConfiguration()
    await nextTick()
    assert.equal(f.focused, 'configurationTrigger')
    assert.equal(f.pageDocument.body.style.overflow, 'auto')
    await f.page.openConfiguration()
    f.media.matches = true
    f.page.breakpointChanged()
    await nextTick()
    assert.equal(f.page.configurationOpen.value, false)
    assert.equal(f.pageDocument.body.style.overflow, 'auto')
    assert.equal(f.focused, 'configurationClose')
    assert.equal(f.page.displayMode.value, 'tags')
  } finally {
    f.stop()
  }
})

for (const overlay of ['search', 'configuration'] as const) {
  for (const boundary of ['owner', 'departure', 'unmount'] as const) {
    test(`${overlay} opening and focus restoration fenced at ${boundary} boundary`, async () => {
      const f = await pageFixture()
      try {
        f.media.matches = false
        f.page.breakpointChanged()
        const opening =
          overlay === 'search'
            ? f.page.openSearch()
            : f.page.openConfiguration()
        if (boundary === 'owner') f.admit(session('bob'))
        else if (boundary === 'departure') f.page.clearPageSearch()
        else f.stop()
        await opening
        assert.equal(f.page.panelOpen.value, false)
        assert.equal(f.page.configurationOpen.value, false)
        assert.equal(f.pageDocument.body.style.overflow, 'auto')
        assert.equal(f.focused, '')
      } finally {
        if (boundary !== 'unmount') f.stop()
      }
    })
  }
}

test('header search dialog opens before submit, locks scroll and supports roving tabs', async () => {
  const f = await pageFixture()
  try {
    f.list.query.value = 'External'
    assert.equal(f.page.panelOpen.value, false)
    assert.equal(f.list.searchResults.value, null)
    await f.page.submitSearch()
    assert.equal(f.page.panelOpen.value, false)
    await f.page.openSearch()
    assert.equal(f.focused, 'searchInput')
    assert.equal(f.pageDocument.body.style.overflow, 'hidden')
    await f.page.submitSearch()
    assert.equal(f.page.panelOpen.value, true)
    assert.equal(f.focused, 'catalogTab')
    assert.equal(f.page.panelHeight.value, 844)
    f.viewport.height = 320
    f.viewport.offsetTop = 60
    f.page.positionPanel()
    assert.equal(f.page.panelHeight.value, 320)
    assert.equal(f.page.panelTop.value, 60)
    assert.equal(f.page.panelBottom.value, 464)
    for (const [key, tab] of [
      ['ArrowRight', 'external'],
      ['ArrowRight', 'catalog'],
      ['End', 'external'],
      ['Home', 'catalog'],
      ['ArrowLeft', 'external'],
    ]) {
      let prevented = false
      f.page.tabKeydown({
        key: key!,
        preventDefault: () => {
          prevented = true
        },
      })
      assert.equal(prevented, true)
      assert.equal(f.page.activeTab.value, tab)
      assert.equal(f.focused, `${tab}Tab`)
      assert.equal(f.elements.get('resultsScroll')?.value.scrollTop, 0)
    }
    f.page.dismiss(true)
    await nextTick()
    assert.equal(f.page.panelOpen.value, false)
    assert.equal(f.focused, 'addTrigger')
    assert.equal(f.pageDocument.body.style.overflow, 'auto')
    assert.equal(f.list.query.value, 'External')
  } finally {
    f.stop()
  }
})

for (const imported of [false, true]) {
  test(`${imported ? 'external import' : 'catalog save'} dismisses only on confirmed success`, async () => {
    const f = await pageFixture()
    try {
      f.list.query.value = 'External'
      await f.page.openSearch()
      await f.page.submitSearch()
      const candidate = imported ? { tmdb_id: '12' } : { slug: 'film-1' }
      const failed = async () => {
        throw new errors.AccountApiError()
      }
      if (imported) f.setImport(failed)
      else f.setWrite(failed)
      await f.page.addMovie(candidate)
      assert.equal(f.page.panelOpen.value, true)
      assert.equal(f.list.query.value, 'External')
      assert.ok(f.list.searchResults.value)
      assert.ok(f.list.error.value)
      if (imported)
        f.setImport(async () => ({
          watchlist: value('2', ['film-12']),
          movie_slug: 'film-12',
        }))
      else f.setWrite(async () => value('2'))
      await f.page.addMovie(candidate)
      assert.equal(f.page.panelOpen.value, false)
      assert.equal(f.list.query.value, '')
      assert.equal(f.list.searchResults.value, null)
      assert.equal(f.focused, 'addTrigger')
      if (!imported) assert.ok(f.posts.every((post) => post.saved === 'true'))
    } finally {
      f.stop()
    }
  })
}

test('late successful add cannot dismiss a newer search; owner invalidation closes panel', async () => {
  const f = await pageFixture()
  try {
    f.list.query.value = 'External'
    await f.page.openSearch()
    await f.page.submitSearch()
    const pending = deferred<AccountWatchlist>()
    f.setWrite(() => pending.promise)
    const add = f.page.addMovie({ slug: 'film-1' })
    f.page.dismiss()
    f.list.query.value = 'Another search draft'
    pending.resolve(value('2'))
    await add
    assert.equal(f.list.query.value, 'Another search draft')
    assert.equal(f.focused, 'catalogTab')
    await f.page.openSearch()
    await f.page.submitSearch()
    f.account.clear()
    assert.equal(f.page.panelOpen.value, false)
    assert.equal(f.list.query.value, '')
    assert.equal(f.list.searchResults.value, null)
  } finally {
    f.stop()
  }
})

test('uncertain committed add preserves search despite read-back membership; unmount fences completion', async () => {
  const f = await pageFixture()
  try {
    f.list.query.value = 'External'
    await f.page.openSearch()
    await f.page.submitSearch()
    f.setWrite(async () => {
      f.setResponse(value('2', ['film-2']))
      throw new errors.AccountApiError()
    })
    await f.page.addMovie({ slug: 'film-2' })
    assert.equal(f.list.slugs.value.has('film-2'), true)
    assert.equal(f.page.panelOpen.value, true)
    assert.equal(f.list.query.value, 'External')
    assert.ok(f.list.searchResults.value)
    assert.ok(f.list.error.value)
    const pending = deferred<AccountWatchlist>()
    f.setWrite(() => pending.promise)
    const add = f.page.addMovie({ slug: 'film-3' })
    f.page.clearPageSearch()
    f.list.query.value = 'New page draft'
    pending.resolve(value('3', ['film-3']))
    await add
    assert.equal(f.page.panelOpen.value, false)
    assert.equal(f.list.query.value, 'New page draft')
    assert.equal(f.focused, 'catalogTab')
  } finally {
    f.stop()
  }
})

test('owner mismatch fails closed and anonymous login never queues a save', async () => {
  const f = await fixture()
  try {
    f.admit({ enabled: true, state: 'anonymous', account: null })
    await settle()
    assert.equal(await f.list.save('film-1', true), false)
    f.setResponse(value('1', ['film-2'], 'bob'))
    f.admit()
    await settle()
    assert.equal(f.account.status.value, 'error')
    assert.equal(f.list.items.value.length, 0)
    assert.equal(f.posts.length, 0)
  } finally {
    f.stop()
  }
})

test('six committed sort choices share owner/revision CAS and preserve search results', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.list.query.value = 'External'
    await f.list.search()
    const results = JSON.stringify(f.list.searchResults.value)
    for (const [index, option] of sorting.watchlistSortOptions.entries()) {
      assert.equal(await f.list.saveSort(option.value), true)
      assert.equal(f.list.sortOrder.value, option.value)
      assert.deepEqual(
        { ...f.sortPosts[index] },
        {
          expected_username: 'alice',
          expected_revision: String(index + 1),
          sort_order: option.value,
        },
      )
    }
    assert.equal(JSON.stringify(f.list.searchResults.value), results)
    assert.equal(f.list.query.value, 'External')
    assert.equal(f.posts.length, 0)
    assert.equal(f.messages.length, 6)
    assert.ok(f.messages.every((message) => message === 'watchlist-changed'))
    assert.doesNotMatch(
      JSON.stringify([...f.states].map(([key, state]) => [key, state.value])),
      /sort_order|release_asc/,
    )
  } finally {
    f.stop()
  }
})

test('sort serializes membership/import both ways with no optimistic selection or rows', async () => {
  const f = await pageFixture()
  try {
    const next = value('2', ['film-2', 'film-1'], 'alice', 'title_desc')
    const pending = deferred<AccountWatchlist>()
    f.setSort(() => pending.promise)
    const select = new TestSelect('title_desc')
    f.page.changeSort({ target: select })
    assert.equal(select.value, 'added_desc')
    assert.equal(f.list.sortOrder.value, 'added_desc')
    assert.equal(f.list.writesBlocked.value, true)
    assert.deepEqual(
      f.page.sortedItems.value.map((item) => item.slug),
      ['film-1'],
    )
    assert.equal(await f.list.save('film-3', true), false)
    assert.equal(await f.list.importMovie('12'), false)
    assert.equal(await f.list.saveSort('title_asc'), false)
    pending.resolve(next)
    await settle()
    assert.equal(f.list.sortOrder.value, 'title_desc')
    assert.deepEqual(
      f.page.sortedItems.value.map((item) => item.slug),
      ['film-2', 'film-1'],
    )
    for (const operation of ['membership', 'import'] as const) {
      const held = deferred<AccountWatchlist>()
      f.setWrite(() => held.promise)
      f.setImport(async () => ({
        watchlist: await held.promise,
        movie_slug: 'film-12',
      }))
      const write =
        operation === 'membership'
          ? f.list.save('film-12', true)
          : f.list.importMovie('12')
      assert.equal(await f.list.saveSort('title_asc'), false)
      held.resolve(value('3', ['film-12'], 'alice', 'title_desc'))
      await write
      assert.equal(f.list.sortOrder.value, 'title_desc')
    }
    assert.equal(f.sortPosts.length, 1)
  } finally {
    f.stop()
  }
})

for (const transition of ['logout', 'owner', 'same-owner'] as const) {
  test(`late sort result is discarded after ${transition}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      const pending = deferred<AccountWatchlist>()
      f.setSort(() => pending.promise)
      const save = f.list.saveSort('title_desc')
      f.setResponse(
        value('3', [], transition === 'owner' ? 'bob' : 'alice', 'release_asc'),
      )
      if (transition === 'logout') f.account.clear()
      else f.admit(session(transition === 'owner' ? 'bob' : 'alice'))
      await settle()
      pending.resolve(value('99', ['old-private-film'], 'alice', 'title_desc'))
      assert.equal(await save, false)
      assert.equal(
        f.list.sortOrder.value,
        transition === 'logout' ? undefined : 'release_asc',
      )
      assert.equal(f.list.items.value.length, 0)
      assert.deepEqual(f.messages, [])
    } finally {
      f.stop()
    }
  })
}

test('older snapshots cannot replace selection but equal-revision French evidence reorders saved projection', async () => {
  const f = await pageFixture()
  try {
    const initial = value('5', ['film-1', 'film-2'], 'alice', 'release_asc')
    delete initial.items[0]!.french_release_date
    f.setResponse(initial)
    await f.account.revalidate()
    assert.deepEqual(
      f.page.sortedItems.value.map((item) => item.slug),
      ['film-2', 'film-1'],
    )
    for (const [date, expected] of [
      ['1997-01-01', ['film-1', 'film-2']],
      ['1999-01-01', ['film-2', 'film-1']],
      [undefined, ['film-2', 'film-1']],
    ] as const) {
      const updated = structuredClone(initial)
      updated.items[0]!.french_release_date = date
      f.setResponse(updated)
      await f.account.revalidate()
      assert.deepEqual(
        f.page.sortedItems.value.map((item) => item.slug),
        expected,
      )
    }
    f.setResponse(value('4', [], 'alice', 'title_desc'))
    await f.account.revalidate()
    assert.equal(f.list.sortOrder.value, 'release_asc')
    assert.equal(f.list.items.value.length, 2)
    assert.equal(f.sortPosts.length, 0)
  } finally {
    f.stop()
  }
})

for (const committed of [false, true]) {
  test(`sort failure restores native control and reads authoritative ${committed ? 'committed' : 'unchanged'} choice without replay`, async () => {
    const f = await pageFixture()
    try {
      f.setSort(async () => {
        if (committed)
          f.setResponse(value('2', ['film-1'], 'alice', 'title_desc'))
        throw new errors.AccountApiError(
          committed ? 0 : 409,
          committed ? '' : 'watchlist_changed',
        )
      })
      const select = new TestSelect('title_desc')
      f.page.changeSort({ target: select })
      assert.equal(select.value, 'added_desc')
      await settle()
      assert.equal(
        f.list.sortOrder.value,
        committed ? 'title_desc' : 'added_desc',
      )
      assert.equal(f.list.ready.value, true)
      assert.ok(f.list.error.value)
      assert.equal(f.sortPosts.length, 1)
      assert.equal(f.gets, 2)
    } finally {
      f.stop()
    }
  })
}

test('sort reconciliation failure gates writes until explicit retry, with no replay', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.setSort(async () => {
      throw new errors.AccountApiError()
    })
    f.setRead(async () => {
      throw new errors.AccountApiError(503, 'watchlist_unavailable')
    })
    await f.list.saveSort('title_desc')
    assert.equal(f.list.ready.value, false)
    assert.equal(await f.list.saveSort('title_desc'), false)
    f.setRead(async () => value('2', ['film-1'], 'alice', 'release_desc'))
    await f.list.retry()
    assert.equal(f.list.sortOrder.value, 'release_desc')
    assert.equal(f.list.ready.value, true)
    assert.equal(f.sortPosts.length, 1)
  } finally {
    f.stop()
  }
})

test('focus revalidation waits for sort and applies other-session authoritative preference', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const pending = deferred<AccountWatchlist>()
    f.setSort(() => pending.promise)
    const save = f.list.saveSort('title_desc')
    const refresh = f.account.revalidate()
    await settle()
    assert.equal(f.gets, 1)
    assert.equal(f.list.writesBlocked.value, true)
    f.setResponse(value('3', ['film-1'], 'alice', 'release_desc'))
    pending.resolve(value('2', ['film-1'], 'alice', 'title_desc'))
    await save
    await refresh
    assert.equal(f.list.sortOrder.value, 'release_desc')
    assert.equal(f.list.writesBlocked.value, false)
  } finally {
    f.stop()
  }
})

test('sort response owner mismatch clears private state instead of applying preference', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    f.setSort(async () => value('2', [], 'bob', 'title_desc'))
    assert.equal(await f.list.saveSort('title_desc'), false)
    assert.equal(f.list.sortOrder.value, undefined)
    assert.equal(f.list.items.value.length, 0)
    assert.equal(f.account.status.value, 'error')
  } finally {
    f.stop()
  }
})

for (const transition of [
  'same-control',
  'owner',
  'unmount',
  'moved-focus',
] as const) {
  test(`native sort focus restoration respects ${transition}`, async () => {
    const f = await pageFixture()
    try {
      const pending = deferred<AccountWatchlist>()
      f.setSort(() => pending.promise)
      const select = new TestSelect('title_desc')
      f.pageDocument.activeElement = select
      const change = f.page.changeSort({ target: select })
      assert.equal(select.value, 'added_desc')
      // Browser moves focus to body when the select becomes disabled.
      f.pageDocument.activeElement = f.pageDocument.body
      if (transition === 'owner') {
        f.setResponse(value('0', [], 'bob'))
        f.admit(session('bob'))
        await settle()
      } else if (transition === 'unmount') select.isConnected = false
      else if (transition === 'moved-focus') f.pageDocument.activeElement = {}
      pending.resolve(value('2', ['film-1'], 'alice', 'title_desc'))
      await change
      assert.equal(select.focusCalls, transition === 'same-control' ? 1 : 0)
      assert.equal(
        select.value,
        'added_desc',
        'completion never writes native value imperatively',
      )
    } finally {
      f.stop()
    }
  })
}
