import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, reactive, type ComputedRef } from 'vue'
import type { LocationQuery } from 'vue-router'
import { mergeOwnedQuery, singularQueryValue } from '../app/utils/routeQuery.ts'

const [
  films,
  search,
  planning,
  film,
  cinema,
  city,
  catalogPagination,
  adminMatches,
  adminMovies,
] = await Promise.all([
  readFile(new URL('../app/pages/films/index.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/recherche.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/planning.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/film/[slug].vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/cinema/[slug].vue', import.meta.url), 'utf8'),
  readFile(
    new URL('../app/pages/ville/[slug]/cinemas.vue', import.meta.url),
    'utf8',
  ),
  readFile(
    new URL('../app/components/MovieCatalogPagination.vue', import.meta.url),
    'utf8',
  ),
  readFile(
    new URL('../app/pages/admin/tmdb-matches.vue', import.meta.url),
    'utf8',
  ),
  readFile(
    new URL(
      '../app/components/admin/AdminMoviesGrid.client.vue',
      import.meta.url,
    ),
    'utf8',
  ),
])

function functionSource(source: string, name: string): string {
  const match = new RegExp(`(?:async\\s+)?function\\s+${name}\\s*\\(`).exec(
    source,
  )
  assert.ok(match, `missing function ${name}`)
  const start = match.index
  const followingSource = source.slice(start + match[0].length)
  const nextFunction = followingSource.search(
    /\n(?:async\s+)?function\s+\w+\s*\(/,
  )
  return nextFunction === -1
    ? source.slice(start)
    : source.slice(start, start + match[0].length + nextFunction)
}

function assertRouterMethod(
  source: string,
  name: string,
  method: 'push' | 'replace',
) {
  const body = functionSource(source, name)
  const opposite = method === 'push' ? 'replace' : 'push'
  assert.match(
    body,
    new RegExp(`router\\.${method}\\(`),
    `${name} must use router.${method}`,
  )
  assert.doesNotMatch(
    body,
    new RegExp(`router\\.${opposite}\\(`),
    `${name} must not use router.${opposite}`,
  )
}

test('catalog search, sorting, and filters replace history while pagination stays push navigation', () => {
  for (const name of [
    'submitSearch',
    'changeSort',
    'applyAdvancedFilters',
    'clearAdvancedFilters',
  ]) {
    assertRouterMethod(films, name, 'replace')
  }
  assert.match(
    films,
    /:previous-to="page > 1 \? \{ query: filmQuery\(\{ search: appliedSearch, page: page - 1,/,
  )
  assert.match(
    films,
    /:next-to="page < totalPages \? \{ query: filmQuery\(\{ search: appliedSearch, page: page \+ 1,/,
  )
  assert.match(catalogPagination, /<NuxtLink\s+v-else\s+:to="previousTo"/)
  assert.match(catalogPagination, /<NuxtLink\s+v-else\s+:to="nextTo"/)
})

test('city catalog search and sorting replace history while pagination stays push navigation', () => {
  for (const name of ['submitSearch', 'changeSort'])
    assertRouterMethod(city, name, 'replace')
  assert.match(
    city,
    /:previous-to="page > 1 \? \{ query: cityCatalogQuery\(appliedSearch, sort, page - 1\) \}/,
  )
  assert.match(
    city,
    /:next-to="page < totalPages \? \{ query: cityCatalogQuery\(appliedSearch, sort, page \+ 1\) \}/,
  )
})

test('search state and selections replace history while result display tabs push', () => {
  assertRouterMethod(search, 'setShowtimeSelection', 'replace')
  assertRouterMethod(search, 'submitSearch', 'replace')
  assertRouterMethod(search, 'setResultGrouping', 'push')
  assertRouterMethod(search, 'setResultLayout', 'push')
})

test('planning and film date, filter, toggle, and sort changes replace history', () => {
  assertRouterMethod(planning, 'updateTimelineQuery', 'replace')
  assertRouterMethod(film, 'updateFilmQuery', 'replace')
})

test('cinema date changes replace history while grouping, layout, and view tabs push', () => {
  assertRouterMethod(cinema, 'selectDate', 'replace')
  assertRouterMethod(cinema, 'submitFilmSearch', 'replace')
  assertRouterMethod(cinema, 'changeFilmSort', 'replace')
  assertRouterMethod(cinema, 'setResultGrouping', 'push')
  assertRouterMethod(cinema, 'setResultLayout', 'push')
  assert.match(
    cinema,
    /<NuxtLink\s+:to="\{ query: viewQuery\('showtimes'\) \}"/,
  )
  assert.match(cinema, /<NuxtLink\s+:to="\{ query: viewQuery\('films'\) \}"/)
  assert.match(cinema, /<NuxtLink\s+:to="\{ query: viewQuery\('activity'\) \}"/)
  assert.match(
    functionSource(cinema, 'viewQuery'),
    /mergeOwnedQuery\(route\.query, FILMS_QUERY_KEYS/,
  )
})

// Execute the cinema page's display-query boundary with actual Vue reactivity.
function cinemaDisplay(query: LocationQuery) {
  const script = cinema.match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )![1]!
  const parsed = ts.createSourceFile(
    'cinema.ts',
    script,
    ts.ScriptTarget.Latest,
    true,
  )
  const names = new Set([
    'DISPLAY_QUERY_KEYS',
    'FILMS_QUERY_KEYS',
    'resultGrouping',
    'resultLayout',
    'viewQuery',
    'selectDate',
    'setResultGrouping',
    'setResultLayout',
  ])
  const statements = parsed.statements.filter((statement) => {
    if (ts.isFunctionDeclaration(statement))
      return names.has(statement.name?.text ?? '')
    return (
      ts.isVariableStatement(statement) &&
      statement.declarationList.declarations.some(
        (declaration) =>
          ts.isIdentifier(declaration.name) && names.has(declaration.name.text),
      )
    )
  })
  assert.equal(statements.length, names.size)
  const compiled = ts.transpileModule(
    statements.map((statement) => statement.getFullText(parsed)).join('\n'),
    {
      compilerOptions: { target: ts.ScriptTarget.ESNext },
    },
  ).outputText
  const route = reactive({ query })
  const navigate = async ({ query: next }: { query: LocationQuery }) => {
    route.query = next
  }
  const bindings = {
    computed,
    route,
    router: { replace: navigate, push: navigate },
    mergeOwnedQuery,
    singularQueryValue,
    todayInParis: () => '2026-10-10',
  }
  // SAFETY: The selected page declarations and explicit return expose exactly these reactive query bindings.
  const display = new Function(
    ...Object.keys(bindings),
    `${compiled}\nreturn { resultLayout, viewQuery, selectDate, setResultGrouping, setResultLayout }`,
  )(...Object.values(bindings)) as {
    resultLayout: ComputedRef<string>
    viewQuery: (view: string) => LocationQuery
    selectDate: (date: string) => void
    setResultGrouping: (grouping: string) => Promise<void>
    setResultLayout: (layout: string) => Promise<void>
  }
  return { route, ...display }
}

test('cinema defaults to boxes except for a singular explicit lines query', () => {
  for (const layout of [
    undefined,
    null,
    '',
    'invalid',
    'boxes',
    ['lines'],
    ['lines', 'boxes'],
  ]) {
    assert.equal(cinemaDisplay({ layout }).resultLayout.value, 'boxes')
  }
  assert.equal(cinemaDisplay({ layout: 'lines' }).resultLayout.value, 'lines')
})

test('cinema keeps explicit lines across date, grouping and tabs; boxes removes layout only', async () => {
  const preserved = {
    q: 'keep',
    shared_theaters: ['ugc-25', 'ugc-26'],
    other: 'keep',
  }
  const display = cinemaDisplay({ ...preserved, date: '2026-10-11' })
  await display.setResultLayout('lines')
  assert.deepEqual(display.route.query, {
    ...preserved,
    date: '2026-10-11',
    layout: 'lines',
  })
  display.selectDate('2026-10-12')
  await display.setResultGrouping('chronological')
  assert.deepEqual(display.route.query, {
    ...preserved,
    date: '2026-10-12',
    layout: 'lines',
    grouping: 'chronological',
  })
  for (const view of ['films', 'activity', 'showtimes']) {
    display.route.query = display.viewQuery(view)
    assert.equal(display.route.query.layout, 'lines')
    assert.equal(display.route.query.grouping, 'chronological')
    assert.equal(display.route.query.other, 'keep')
    assert.deepEqual(
      display.route.query.shared_theaters,
      preserved.shared_theaters,
    )
    assert.equal(display.resultLayout.value, 'lines')
  }
  await display.setResultLayout('boxes')
  assert.deepEqual(display.route.query, {
    date: '2026-10-12',
    grouping: 'chronological',
    shared_theaters: preserved.shared_theaters,
    other: 'keep',
  })
  display.selectDate('2026-10-10')
  assert.equal(display.route.query.layout, undefined)
  assert.equal(display.route.query.date, undefined)
  assert.equal(display.resultLayout.value, 'boxes')
})

test('TMDB matched search replaces history while pagination and tabs push', () => {
  assertRouterMethod(adminMatches, 'updateMatchedSearch', 'replace')
  for (const name of [
    'changePage',
    'changeRejectedPage',
    'changeMatchedPage',
    'changeGroupsPage',
    'selectTab',
  ]) {
    assertRouterMethod(adminMatches, name, 'push')
  }
})

test('admin movie grid separates transient controls from pagination history', () => {
  assert.match(
    functionSource(adminMovies, 'onSortOrFilterChanged'),
    /replaceRoute\(next\)/,
  )
  assert.match(functionSource(adminMovies, 'updateSearch'), /replaceRoute\(/)
  assert.match(functionSource(adminMovies, 'updateOverrides'), /replaceRoute\(/)
  assert.match(
    functionSource(adminMovies, 'onPaginationChanged'),
    /pushRoute\(/,
  )
  assertRouterMethod(adminMovies, 'replaceRoute', 'replace')
  assertRouterMethod(adminMovies, 'pushRoute', 'push')
})

test('automatic corrections and canonical film slug redirects keep replace semantics', () => {
  assert.match(
    functionSource(films, 'loadMovies'),
    /page\.value > lastPage[\s\S]*router\.replace\(\{ query \}\)/,
  )
  assert.match(
    functionSource(city, 'loadCatalog'),
    /page\.value > lastPage[\s\S]*router\.replace\(\{ query \}\)/,
  )
  assertRouterMethod(films, 'applyRoute', 'replace')
  assertRouterMethod(search, 'applyRoute', 'replace')
  assertRouterMethod(planning, 'applyRoute', 'replace')
  assertRouterMethod(film, 'applyRoute', 'replace')
  assert.match(
    film,
    /navigateTo\(\s*\{\s*path: `\/film\/\$\{encodeURIComponent\([^`]+\)\}`,\s*query: route\.query,?\s*\},\s*\{ redirectCode: 308, replace: true \},?\s*\)/,
  )
})
