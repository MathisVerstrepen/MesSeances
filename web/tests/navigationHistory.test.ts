import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const [
  films,
  search,
  planning,
  film,
  cinema,
  adminMatches,
  adminMovies
] = await Promise.all([
  readFile(new URL('../app/pages/films.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/recherche.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/planning.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/film/[slug].vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/cinema/[slug].vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/admin/tmdb-matches.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/components/admin/AdminMoviesGrid.client.vue', import.meta.url), 'utf8')
])

function functionSource(source: string, name: string): string {
  const match = new RegExp(`(?:async\\s+)?function\\s+${name}\\s*\\(`).exec(source)
  assert.ok(match, `missing function ${name}`)
  const start = match.index
  const followingSource = source.slice(start + match[0].length)
  const nextFunction = followingSource.search(/\n(?:async\s+)?function\s+\w+\s*\(/)
  return nextFunction === -1 ? source.slice(start) : source.slice(start, start + match[0].length + nextFunction)
}

function assertRouterMethod(source: string, name: string, method: 'push' | 'replace') {
  const body = functionSource(source, name)
  const opposite = method === 'push' ? 'replace' : 'push'
  assert.match(body, new RegExp(`router\\.${method}\\(`), `${name} must use router.${method}`)
  assert.doesNotMatch(body, new RegExp(`router\\.${opposite}\\(`), `${name} must not use router.${opposite}`)
}

test('catalog search, sorting, and filters replace history while pagination stays push navigation', () => {
  for (const name of ['submitSearch', 'changeSort', 'applyAdvancedFilters', 'clearAdvancedFilters']) {
    assertRouterMethod(films, name, 'replace')
  }
  assert.match(films, /<NuxtLink v-else :to="\{ query: filmQuery\(\{ search: appliedSearch, page: page - 1,[^\n]+/)
  assert.match(films, /<NuxtLink v-else :to="\{ query: filmQuery\(\{ search: appliedSearch, page: page \+ 1,[^\n]+/)
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
  assertRouterMethod(cinema, 'setResultGrouping', 'push')
  assertRouterMethod(cinema, 'setResultLayout', 'push')
  assert.match(cinema, /<NuxtLink\s+:to="\{ query: viewQuery\('showtimes'\) \}"/)
  assert.match(cinema, /<NuxtLink\s+:to="\{ query: viewQuery\('films'\) \}"/)
})

test('TMDB matched search replaces history while pagination and tabs push', () => {
  assertRouterMethod(adminMatches, 'updateMatchedSearch', 'replace')
  for (const name of ['changePage', 'changeRejectedPage', 'changeMatchedPage', 'changeGroupsPage', 'selectTab']) {
    assertRouterMethod(adminMatches, name, 'push')
  }
})

test('admin movie grid separates transient controls from pagination history', () => {
  assert.match(functionSource(adminMovies, 'onSortOrFilterChanged'), /replaceRoute\(next\)/)
  assert.match(functionSource(adminMovies, 'updateSearch'), /replaceRoute\(/)
  assert.match(functionSource(adminMovies, 'updateOverrides'), /replaceRoute\(/)
  assert.match(functionSource(adminMovies, 'onPaginationChanged'), /pushRoute\(/)
  assertRouterMethod(adminMovies, 'replaceRoute', 'replace')
  assertRouterMethod(adminMovies, 'pushRoute', 'push')
})

test('automatic corrections and canonical film slug redirects keep replace semantics', () => {
  assert.match(functionSource(films, 'loadMovies'), /page\.value > lastPage[\s\S]*router\.replace\(\{ query \}\)/)
  assertRouterMethod(films, 'applyRoute', 'replace')
  assertRouterMethod(search, 'applyRoute', 'replace')
  assertRouterMethod(planning, 'applyRoute', 'replace')
  assertRouterMethod(film, 'applyRoute', 'replace')
  assert.match(film, /navigateTo\(\{ path: `\/film\/\$\{encodeURIComponent\([^\n]+\)\}`, query: route\.query \}, \{ redirectCode: 308, replace: true \}\)/)
})
