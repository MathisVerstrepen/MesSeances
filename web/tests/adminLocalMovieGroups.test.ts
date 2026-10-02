import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import * as Vue from 'vue'
import type {
  AdminAddLocalMovieMembersRequest,
  AdminCreateLocalMovieGroupRequest,
  AdminLocalMovieGroup,
  AdminLocalMovieGroupsResponse,
} from '../app/types/api.ts'
import * as catalog from '../app/utils/adminLocalMovieGroups.ts'
import * as tmdb from '../app/utils/adminTmdbMatches.ts'
import * as routeQuery from '../app/utils/routeQuery.ts'

function group(
  id: string,
  ...titles: Array<string | null>
): AdminLocalMovieGroup {
  return {
    local_movie_id: id,
    primary: { source_provider: 'ugc', source_movie_id: '0' },
    metadata_source: null,
    members: titles.map((source_title, index) => ({
      source_provider: 'ugc',
      source_movie_id: String(index),
      source_title,
      available: true,
      source_runtime_minutes: null,
      source_poster_url: null,
    })),
  }
}

function response(
  items: AdminLocalMovieGroup[],
  offset = 0,
): AdminLocalMovieGroupsResponse {
  return { items, limit: 100, offset }
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

test('normalizes accents, case, punctuation, whitespace and ligatures without losing numbers', () => {
  assert.equal(
    catalog.normalizeLocalMovieTitle('  ÉTÉ :  l’Œuvre\t2!  '),
    'ete l oeuvre 2',
  )
  assert.equal(catalog.localMovieTitleSimilarity('Été - 2', 'ETE  2'), 1)
  assert.equal(catalog.localMovieTitleSimilarity('映画 2', '映画 2'), 1)
  assert.equal(catalog.localMovieTitleSimilarity('A', 'a'), 1)
  assert.equal(catalog.localMovieTitleSimilarity('A', 'B'), 0)
})

test('scores approximate titles symmetrically with bounded multiset bigram similarity', () => {
  const similar = catalog.localMovieTitleSimilarity('Alien', 'Aliens')
  assert.ok(similar > catalog.localMovieTitleSimilarity('Alien', 'Barbie'))
  assert.ok(similar > 0 && similar < 1)
  assert.equal(similar, catalog.localMovieTitleSimilarity('Aliens', 'Alien'))
  assert.ok(catalog.localMovieTitleSimilarity('aaaa', 'aa') < 1)
  for (const title of [null, undefined, '', ' ! ']) {
    assert.equal(catalog.localMovieTitleSimilarity(title, title), 0)
    assert.equal(catalog.localMovieTitleSimilarity('Alien', title), 0)
  }
})

test('ranks mean best member matches for every selection, not just the first film or primary', () => {
  const groups = [
    group('one', 'Alien'),
    group('both', 'Barbie', 'Alien'),
    group('none', null),
  ]
  const ranked = catalog.rankLocalMovieGroups(groups, ['Alien', 'Barbie'])
  assert.deepEqual(
    ranked.map(({ group }) => group.local_movie_id),
    ['both', 'one', 'none'],
  )
  assert.equal(ranked[0]!.score, 1)
  assert.equal(
    ranked[1]!.score,
    (1 + catalog.localMovieTitleSimilarity('Barbie', 'Alien')) / 2,
  )
  assert.equal(ranked[2]!.score, 0)
  assert.deepEqual(
    groups.map((item) => item.local_movie_id),
    ['one', 'both', 'none'],
  )
  assert.deepEqual(
    catalog.rankLocalMovieGroups(groups, ['Barbie', 'Alien']),
    ranked,
  )
})

test('orders ties by normalized display title then ID independently of page order', () => {
  const groups = [
    group('z', 'Été'),
    group('a', 'ETE'),
    group('b', 'Barbie'),
    group('empty'),
  ]
  assert.deepEqual(
    catalog
      .rankLocalMovieGroups(groups, [])
      .map(({ group }) => group.local_movie_id),
    ['b', 'empty', 'a', 'z'],
  )
  assert.deepEqual(
    catalog.rankLocalMovieGroups(groups, ['unrelated']),
    catalog.rankLocalMovieGroups([...groups].reverse(), ['unrelated']),
  )
})

test('uses metadata title with primary/member/ID fallback and ranks nullable unavailable titles safely', () => {
  const item = group('fallback', null, 'Alien')
  item.metadata_source = { source_provider: 'ugc', source_movie_id: '1' }
  item.members[1]!.available = false
  assert.equal(catalog.localMovieGroupTitle(item), 'Alien')
  assert.equal(catalog.rankLocalMovieGroups([item], ['Alien'])[0]!.score, 1)
  assert.equal(
    catalog.localMovieGroupTitle(group('untitled', null, '  ')),
    'untitled',
  )
})

test('loads all pages at limit 100, including empty terminal page for exact multiples', async () => {
  for (const length of [0, 1, 100, 201]) {
    const items = Array.from({ length }, (_, index) =>
      group(String(index), `Film ${index}`),
    )
    const calls: number[] = []
    const progress: number[] = []
    const result = await catalog.loadLocalMovieGroupCatalog(
      async (limit, offset) => {
        assert.equal(limit, 100)
        calls.push(offset)
        return response(items.slice(offset, offset + limit), offset)
      },
      () => true,
      (count) => progress.push(count),
    )
    assert.deepEqual(result, items)
    assert.equal(calls.length, Math.floor(length / 100) + 1)
    assert.equal(progress.at(-1), length)
  }
})

test('catalog deduplicates IDs and propagates errors without publishing partial success', async () => {
  let calls = 0
  await assert.rejects(
    catalog.loadLocalMovieGroupCatalog(
      async () => {
        if (++calls === 2) throw new Error('offline')
        return response(
          Array.from({ length: 100 }, () => group('same', 'Alien')),
        )
      },
      () => true,
    ),
    /offline/,
  )
  calls = 0
  const result = await catalog.loadLocalMovieGroupCatalog(
    async () => {
      if (++calls === 1)
        return response(Array.from({ length: 100 }, () => group('same', 'old')))
      return response([group('same', 'new')])
    },
    () => true,
  )
  assert.equal(result?.length, 1)
  assert.equal(result?.[0]?.members[0]?.source_title, 'new')
})

test('cancellation prevents publication, progress and next requests after refresh/unmount', async () => {
  const pending = deferred<AdminLocalMovieGroupsResponse>()
  let current = true
  let calls = 0
  let progress = 0
  const loading = catalog.loadLocalMovieGroupCatalog(
    () => {
      calls += 1
      return pending.promise
    },
    () => current,
    () => {
      progress += 1
    },
  )
  current = false
  pending.resolve(response(Array.from({ length: 100 }, () => group('old'))))
  assert.equal(await loading, null)
  assert.equal(calls, 1)
  assert.equal(progress, 0)
  assert.equal(
    await catalog.loadLocalMovieGroupCatalog(
      () => {
        throw new Error('must not fetch')
      },
      () => false,
    ),
    null,
  )
})

const pageSource = readFile(
  new URL('../app/pages/admin/tmdb-matches.vue', import.meta.url),
  'utf8',
)

// Exercise the real page script using Vue reactivity and mocked Nuxt/API globals.
// No DOM library or dependency addition needed; browser smoke covers the template.
async function mountPage(
  fetchGroups: (
    limit: number,
    offset: number,
  ) => Promise<AdminLocalMovieGroupsResponse>,
) {
  const source = (await pageSource).match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )![1]!
  const script = ts
    .transpileModule(source, {
      compilerOptions: {
        target: ts.ScriptTarget.ES2022,
        module: ts.ModuleKind.ESNext,
      },
    })
    .outputText.replace(/^import[\s\S]*?from ['"][^'"]+['"];?\s*$/gm, '')
    .replace(/export \{\};?/, '')
  let mounted = () => {}
  let unmounted = () => {}
  const calls: Array<{ operation: string; input?: unknown }> = []
  const api = {
    adminLocalMovieGroups: fetchGroups,
    adminPendingMatches: async () => ({ items: [], limit: 20, offset: 0 }),
    adminTMDBMetadataRefreshStatus: async () => ({ job: null }),
    adminApproveMatch: async () => {},
    adminCreateLocalMovieGroup: async (
      input: AdminCreateLocalMovieGroupRequest,
    ) => {
      calls.push({ operation: 'create', input })
    },
    adminAddLocalMovieMembers: async (
      _id: string,
      input: AdminAddLocalMovieMembersRequest,
    ) => {
      calls.push({ operation: 'add', input })
    },
    adminUnmergeLocalMovie: async () => {
      calls.push({ operation: 'unmerge' })
    },
  }
  const state = runInNewContext(
    `(function() { ${script}; return {
    selectedSources, selectedSourceList, primarySourceKey, selectionSidebarOpen, groupCatalog,
    groupCatalogPending, groupCatalogError, groupingError, rankedGroups, anyMutation,
    toggleMergeSelection, closeSelectionSidebar, reopenSelectionSidebar, handleSelectionEscape,
    clearMergeSelection, refreshGroupCatalog, refreshResources, mergeSelectedSources,
    addSelectedSourcesToGroup, unmerge, activeMutation, refreshAfterDecision, approveWithTmdbId,
  }; })()`,
    {
      ...Vue,
      ...catalog,
      ...tmdb,
      ...routeQuery,
      useMesSeancesApi: () => api,
      useRoute: () => ({ query: {} }),
      useRouter: () => ({ replace: async () => {}, push: async () => {} }),
      definePageMeta: () => {},
      useHead: () => {},
      onMounted: (fn: () => void) => {
        mounted = fn
      },
      onBeforeUnmount: (fn: () => void) => {
        unmounted = fn
      },
      getFrenchAdminApiError: () => 'Service indisponible.',
      getApiErrorStatus: (error: { status?: number }) => error.status,
      document: { activeElement: null, getElementById: () => null },
      window: {
        addEventListener: () => {},
        removeEventListener: () => {},
        scrollTo: () => {},
      },
      HTMLElement: class {},
      setTimeout,
      clearTimeout,
    },
  )
  mounted()
  await settle()
  return { state, api, calls, unmount: unmounted }
}

async function settle() {
  for (let index = 0; index < 8; index += 1) await Promise.resolve()
  await Vue.nextTick()
}

function match(id: string, title = 'Alien') {
  return {
    source_provider: 'ugc',
    source_movie_id: id,
    source_title: title,
    status: 'unmatched',
  }
}

test('page loads independent full catalog; selecting, dismissing and reopening preserves multiple selections', async () => {
  const offsets: number[] = []
  const { state, unmount } = await mountPage(async (limit, offset) => {
    if (limit === 20) return response([])
    offsets.push(offset)
    return response(
      offset === 0
        ? Array.from({ length: 100 }, (_, index) =>
            group(String(index), 'Barbie'),
          )
        : [group('off-page', 'Alien')],
    )
  })
  assert.deepEqual(offsets, [0, 100])
  state.toggleMergeSelection(match('1'))
  assert.equal(state.selectionSidebarOpen.value, true)
  assert.equal(state.rankedGroups.value[0].group.local_movie_id, 'off-page')
  state.toggleMergeSelection(match('2'))
  let prevented = false
  state.handleSelectionEscape({
    key: 'Escape',
    preventDefault: () => {
      prevented = true
    },
  })
  assert.equal(prevented, true)
  assert.equal(state.selectionSidebarOpen.value, false)
  assert.equal(state.selectedSourceList.value.length, 2)
  state.reopenSelectionSidebar({ currentTarget: null })
  assert.equal(state.selectionSidebarOpen.value, true)
  state.toggleMergeSelection(match('1'))
  state.toggleMergeSelection(match('2'))
  assert.equal(state.selectionSidebarOpen.value, false)
  unmount()
})

test('page discards stale loads and partial failure, retries, invalidates refresh targets and stops on unmount', async () => {
  const { state, api, unmount } = await mountPage(async () =>
    response([group('old', 'Alien')]),
  )
  const stale = deferred<AdminLocalMovieGroupsResponse>()
  api.adminLocalMovieGroups = () => stale.promise
  const first = state.refreshGroupCatalog()
  assert.equal(state.groupCatalog.value.length, 0)
  assert.equal(state.groupCatalogPending.value, true)
  api.adminLocalMovieGroups = async () => response([group('fresh', 'Alien')])
  await state.refreshGroupCatalog()
  stale.resolve(response([group('stale')]))
  await first
  assert.equal(state.groupCatalog.value[0].local_movie_id, 'fresh')
  api.adminLocalMovieGroups = async () => {
    throw new Error('offline')
  }
  await state.refreshGroupCatalog()
  assert.equal(state.groupCatalogError.value, 'Service indisponible.')
  assert.equal(state.groupCatalogPending.value, false)
  assert.equal(state.groupCatalog.value.length, 0)
  api.adminLocalMovieGroups = async () => response([group('retry')])
  await state.refreshGroupCatalog()
  assert.equal(state.groupCatalogError.value, '')
  const waiting = deferred<AdminLocalMovieGroupsResponse>()
  api.adminLocalMovieGroups = () => waiting.promise
  const refresh = state.refreshGroupCatalog()
  unmount()
  waiting.resolve(response([group('after-unmount')]))
  await refresh
  assert.equal(state.groupCatalog.value.length, 0)
})

test('add/create enforce exclusion, send explicit selection, clear sidebar and refresh catalog after success', async () => {
  let catalogs = 0
  const target = group('target', 'Alien')
  const { state, calls, unmount } = await mountPage(async (limit) => {
    if (limit === 100) catalogs += 1
    return response([target])
  })
  state.toggleMergeSelection(match('1'))
  state.activeMutation.value = 'busy'
  await state.addSelectedSourcesToGroup(target)
  assert.equal(calls.length, 0)
  state.activeMutation.value = ''
  await state.addSelectedSourcesToGroup(group('removed'))
  assert.equal(calls.length, 0)
  await state.addSelectedSourcesToGroup(target)
  assert.equal(calls[0]?.operation, 'add')
  assert.equal(state.selectedSourceList.value.length, 0)
  assert.equal(state.selectionSidebarOpen.value, false)
  assert.equal(catalogs, 2)
  state.toggleMergeSelection(match('1'))
  state.toggleMergeSelection(match('2'))
  await state.mergeSelectedSources()
  assert.equal(calls.length, 1)
  state.primarySourceKey.value = 'ugc:1'
  await state.mergeSelectedSources()
  assert.equal(calls[1]?.operation, 'create')
  assert.equal(state.selectedSourceList.value.length, 0)
  assert.equal(catalogs, 3)
  await state.unmerge(target)
  assert.equal(catalogs, 4)
  unmount()
})

test('failed grouping mutations preserve input and expose errors in sidebar; deleted group triggers refresh', async () => {
  let catalogs = 0
  const target = group('target', 'Alien')
  const { state, api, unmount } = await mountPage(async (limit) => {
    if (limit === 100) catalogs += 1
    return response([target])
  })
  state.toggleMergeSelection(match('1'))
  api.adminAddLocalMovieMembers = async () => {
    throw new Error('offline')
  }
  await state.addSelectedSourcesToGroup(target)
  assert.equal(state.groupingError.value, 'Service indisponible.')
  assert.equal(state.selectedSourceList.value.length, 1)
  assert.equal(state.selectionSidebarOpen.value, true)
  state.toggleMergeSelection(match('2'))
  state.primarySourceKey.value = 'ugc:1'
  api.adminCreateLocalMovieGroup = async () => {
    throw new Error('offline')
  }
  await state.mergeSelectedSources()
  assert.equal(state.groupingError.value, 'Service indisponible.')
  assert.equal(state.primarySourceKey.value, 'ugc:1')
  api.adminAddLocalMovieMembers = async () => {
    throw { status: 404 }
  }
  await state.addSelectedSourcesToGroup(target)
  assert.equal(catalogs, 2)
  assert.match(state.groupingError.value, /n’existe plus/)
  assert.equal(state.selectedSourceList.value.length, 2)
  unmount()
})

test('TMDB approval removes selected source despite held mutation lock and closes only after last source removed', async () => {
  const { state, unmount } = await mountPage(async () => response([]))
  const first = match('1')
  const second = match('2')
  state.toggleMergeSelection(first)
  state.toggleMergeSelection(second)
  state.primarySourceKey.value = 'ugc:1'
  await state.approveWithTmdbId(first, 123, 'manual')
  assert.equal(state.selectedSourceList.value.length, 1)
  assert.equal(state.selectedSourceList.value[0].source_movie_id, '2')
  assert.equal(state.primarySourceKey.value, '')
  assert.equal(state.selectionSidebarOpen.value, true)
  await state.approveWithTmdbId(second, 456, 'candidate')
  assert.equal(state.selectedSourceList.value.length, 0)
  assert.equal(state.selectionSidebarOpen.value, false)
  unmount()
})

test('template moves selection/add actions into named nonmodal sidebar while preserving bottom management pagination', async () => {
  const page = await pageSource
  const sidebar = page.match(/<aside[\s\S]*?<\/aside>/)![0]
  assert.match(sidebar, /aria-labelledby="merge-selection-title"/)
  assert.match(sidebar, /@click="closeSelectionSidebar"/)
  assert.match(sidebar, /@click="addSelectedSourcesToGroup\(group\)"/)
  assert.match(
    sidebar,
    /groupCatalogError[\s\S]*Liste des regroupements incomplète/,
  )
  assert.match(sidebar, /@click="refreshGroupCatalog"/)
  assert.match(sidebar, /groupingError/)
  assert.doesNotMatch(sidebar, /aria-modal|role="dialog"/)
  const management = page.slice(
    page.indexOf('aria-labelledby="local-groups-title"'),
    page.indexOf('<aside'),
  )
  assert.match(management, /@click="unmerge\(group\)"/)
  assert.match(management, /Pagination des regroupements locaux/)
  assert.doesNotMatch(management, /addSelectedSourcesToGroup/)
  assert.equal((page.match(/id="merge-selection-title"/g) ?? []).length, 1)
})
