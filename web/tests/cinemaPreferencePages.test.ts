import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { ref } from 'vue'

async function functionsFromPage(path: string, names: string[]) {
  const source = await readFile(new URL(path, import.meta.url), 'utf8')
  const script = source.match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )![1]!
  const parsed = ts.createSourceFile(
    'page.ts',
    script,
    ts.ScriptTarget.Latest,
    true,
  )
  const functions = parsed.statements.filter(
    (node) =>
      ts.isFunctionDeclaration(node) &&
      node.name &&
      names.includes(node.name.text),
  )
  assert.equal(functions.length, names.length)
  return ts.transpileModule(
    functions.map((node) => node.getFullText(parsed)).join('\n'),
    { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
  ).outputText
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((yes) => {
    resolve = yes
  })
  return { promise, resolve }
}

test('cinema page waits for acknowledgement and does not announce after departure or identity change', async () => {
  const code = await functionsFromPage('../app/pages/cinemas.vue', [
    'applyDraftSelection',
    'reportSaved',
  ])
  for (const outcome of [
    'saved',
    'failed',
    'departed',
    'switched',
    'blocked',
    'empty',
  ]) {
    const acknowledgement = deferred<boolean>()
    let writes = 0
    const bindings = {
      writesBlocked: ref(outcome === 'blocked'),
      isUnmounted: false,
      selectionScopeKey: ref(0),
      statusMessage: ref(''),
      favoriteTheaterIds: ref(['ugc-2']),
      draftFavoriteTheaterIds: ref(['ugc-2']),
      setFavoriteTheaterIds: async () => {
        writes++
        return acknowledgement.promise
      },
    }
    // SAFETY: These functions are extracted from the page and the wrapper returns their explicit signatures.
    const page = new Function(
      ...Object.keys(bindings),
      `${code}\nreturn { applyDraftSelection, depart: () => { isUnmounted = true } }`,
    )(...Object.values(bindings)) as {
      applyDraftSelection: (ids: string[]) => Promise<void>
      depart: () => void
    }
    const pending = page.applyDraftSelection(
      outcome === 'empty' ? [] : ['ugc-1'],
    )
    assert.doesNotMatch(bindings.statusMessage.value, /1 cinéma enregistré/)
    if (outcome === 'departed') page.depart()
    if (outcome === 'switched') bindings.selectionScopeKey.value++
    acknowledgement.resolve(outcome !== 'failed')
    await pending
    assert.equal(writes, outcome === 'blocked' || outcome === 'empty' ? 0 : 1)
    if (outcome === 'saved')
      assert.equal(bindings.statusMessage.value, '1 cinéma enregistré.')
    if (outcome === 'departed' || outcome === 'switched')
      assert.equal(bindings.statusMessage.value, '')
    if (outcome === 'empty')
      assert.match(bindings.statusMessage.value, /restent inchangés/)
    if (outcome === 'failed')
      assert.match(bindings.statusMessage.value, /pas pu être enregistrée/)
  }
})

test('planning clears old result and fences pending request before empty, unresolved and error early returns', async () => {
  const code = await functionsFromPage('../app/pages/planning.vue', [
    'loadTimeline',
  ])
  for (const transition of ['empty', 'unresolved', 'error']) {
    const response = deferred<{ stale: boolean }>()
    const bindings = {
      requestId: 0,
      timeline: ref<{ stale: boolean } | null>({ stale: true }),
      preferences: {
        error: ref<string | null>(null),
        isInitialized: ref(true),
        activeTheaterIds: ref(['ugc-1']),
      },
      errorMessage: ref(''),
      pending: ref(false),
      date: ref('2026-09-24'),
      language: ref('ALL'),
      api: { timeline: () => response.promise },
      getFrenchApiError: () => 'Erreur',
    }
    // SAFETY: The extracted function uses only the injected page bindings.
    const load = new Function(
      ...Object.keys(bindings),
      `${code}\nreturn loadTimeline`,
    )(...Object.values(bindings)) as () => Promise<void>
    const old = load()
    if (transition === 'empty') bindings.preferences.activeTheaterIds.value = []
    if (transition === 'unresolved')
      bindings.preferences.isInitialized.value = false
    if (transition === 'error')
      bindings.preferences.error.value = 'Session indisponible'
    await load()
    response.resolve({ stale: true })
    await old
    assert.equal(bindings.timeline.value, null)
    assert.equal(bindings.pending.value, transition === 'unresolved')
  }
})

test('movie catalog invalidates personalized requests but still permits explicit all-theater browsing', async () => {
  const code = await functionsFromPage('../app/pages/films/index.vue', [
    'loadMovies',
  ])
  const response = deferred<{ total: number }>()
  let requests = 0
  const bindings = {
    requestId: 0,
    pending: ref(false),
    errorMessage: ref(''),
    catalog: ref<{ total: number } | null>(null),
    preferences: {
      error: ref<string | null>(null),
      isInitialized: ref(true),
      favoriteTheaterIds: ref(['ugc-1']),
    },
    appliedFilters: ref({ allTheaters: false }),
    appliedSearch: ref(''),
    sort: ref('title'),
    page: ref(1),
    PAGE_SIZE: 24,
    finishAdvancedApplyNavigation: async () => {},
    serializeMovieCatalogFilters: () => ({}),
    api: {
      movies: () => {
        requests++
        return response.promise
      },
    },
    scrollAfterLoad: false,
    getFrenchApiError: () => 'Erreur',
  }
  // SAFETY: The extracted loader uses only these bindings for a one-page response.
  const load = new Function(
    ...Object.keys(bindings),
    `${code}\nreturn loadMovies`,
  )(...Object.values(bindings)) as () => Promise<void>
  const previous = load()
  bindings.preferences.isInitialized.value = false
  await load()
  response.resolve({ total: 1 })
  await previous
  assert.equal(bindings.catalog.value, null)
  bindings.appliedFilters.value.allTheaters = true
  bindings.preferences.error.value = 'Compte indisponible'
  await load()
  assert.equal(requests, 2)
  assert.equal(bindings.catalog.value?.total, 1)
})
