import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { getFrenchAdminApiError, useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import type { AdminMovieItem, AdminMoviePatchRequest, AdminMoviesQuery, AdminMoviesResponse } from '../app/types/api.ts'
import {
  adminMovieDraftFingerprint,
  adminMovieGridFilterModel,
  adminMovieQueryFromGrid,
  adminMovieRouteQuery,
  buildAdminMoviePatch,
  emptyAdminMovieDraft,
  isAdminMovieFieldOverridden,
  parseAdminMovieRouteQuery,
  stageAdminMovieOverride,
  stageAdminMovieRestore,
  validateAdminMovieDraft
} from '../app/utils/adminMovies.ts'

function movie(overrides: Partial<AdminMovieItem> = {}): AdminMovieItem {
  const metadata = {
    title: 'Le Film',
    runtime_minutes: 100,
    release_date: '2026-08-30',
    genres: ['Drame'],
    overview: 'Synopsis',
    poster_url: 'https://images.example/poster.jpg',
    backdrop_url: null,
    trailer_vf_youtube_key: 'abcdefghijk',
    trailer_vo_youtube_key: null
  }
  return {
    id: '9007199254740993',
    updated_at: '2026-08-30T09:15:00.123456789Z',
    automatic: { ...metadata, genres: [...metadata.genres] },
    values: { ...metadata, genres: [...metadata.genres] },
    overridden_fields: [],
    ...overrides
  }
}

test('canonicalizes all owned route query values and invalid combinations', () => {
  const state = parseAdminMovieRouteQuery({
    q: '  dune  ', runtime_min: '90', runtime_max: '180', release_date_from: '2025-01-01', release_date_to: '2026-12-31',
    genre: '  science-fiction ', override_status: 'overridden', override_field: 'overview', sort: 'updated_at', direction: 'desc', page: '3'
  })
  assert.deepEqual(state, {
    q: 'dune', runtime_min: 90, runtime_max: 180, release_date_from: '2025-01-01', release_date_to: '2026-12-31',
    genre: 'science-fiction', override_status: 'overridden', override_field: 'overview', sort: 'updated_at', direction: 'desc', page: 3
  })
  assert.deepEqual(adminMovieRouteQuery(state), {
    q: 'dune', runtime_min: '90', runtime_max: '180', release_date_from: '2025-01-01', release_date_to: '2026-12-31',
    genre: 'science-fiction', override_status: 'overridden', override_field: 'overview', sort: 'updated_at', direction: 'desc', page: '3'
  })

  const invalid = parseAdminMovieRouteQuery({ runtime_min: '200', runtime_max: '100', release_date_from: '2026-02-30', override_status: 'automatic', override_field: 'title', page: ['2'] })
  assert.equal(invalid.runtime_min, 200)
  assert.equal(invalid.runtime_max, undefined)
  assert.equal(invalid.release_date_from, undefined)
  assert.equal(invalid.override_field, undefined)
  assert.equal(invalid.page, 1)
})

test('translates one AG Grid sort and core filters to strict list API query', () => {
  const state = parseAdminMovieRouteQuery({ q: 'Alien', override_status: 'overridden', override_field: 'genres', page: '4' })
  assert.deepEqual(adminMovieQueryFromGrid(state, {
    startRow: 150,
    endRow: 200,
    sortModel: [{ colId: 'release_date', sort: 'desc' }, { colId: 'title', sort: 'asc' }],
    filterModel: {
      runtime_minutes: { type: 'inRange', filter: 80, filterTo: 140 },
      release_date: { type: 'inRange', dateFrom: '2025-01-01 00:00:00', dateTo: '2026-12-31 00:00:00' },
      genres: { type: 'contains', filter: 'Action' }
    }
  }), {
    limit: 50,
    offset: 150,
    search: 'Alien',
    runtime_min: 80,
    runtime_max: 140,
    release_date_from: '2025-01-01',
    release_date_to: '2026-12-31',
    genre: 'Action',
    override_status: 'overridden',
    override_field: 'genres',
    sort: 'release_date',
    direction: 'desc'
  })
  assert.deepEqual(adminMovieGridFilterModel(parseAdminMovieRouteQuery({ runtime_min: '90', release_date_to: '2026-12-31', genre: 'drame' })), {
    runtime_minutes: { type: 'inRange', filter: 90, filterTo: 2_147_483_647 },
    release_date: { type: 'inRange', dateFrom: '0001-01-01', dateTo: '2026-12-31' },
    genres: { type: 'contains', filter: 'drame' }
  })
})

test('keeps independent drafts and distinguishes equal automatic override, explicit null, and restore', () => {
  const item = movie()
  const first = stageAdminMovieOverride(item, emptyAdminMovieDraft(), 'title', item.automatic.title)
  const second = emptyAdminMovieDraft()
  assert.notEqual(adminMovieDraftFingerprint(first), adminMovieDraftFingerprint(second))
  assert.equal(isAdminMovieFieldOverridden(item, first, 'title'), true)

  const withNull = stageAdminMovieOverride(item, first, 'poster_url', null)
  assert.deepEqual(buildAdminMoviePatch(item, withNull), {
    expected_updated_at: item.updated_at,
    overrides: { title: 'Le Film', poster_url: null }
  })

  const overridden = movie({
    values: { ...item.values, overview: 'Texte manuel' },
    overridden_fields: ['overview']
  })
  const restored = stageAdminMovieRestore(overridden, undefined, 'overview')
  assert.deepEqual(buildAdminMoviePatch(overridden, restored), {
    expected_updated_at: overridden.updated_at,
    restore: ['overview']
  })
  assert.equal(isAdminMovieFieldOverridden(overridden, restored, 'overview'), false)

  const refreshed = { ...item, updated_at: '2026-08-30T10:00:00Z' }
  assert.equal(buildAdminMoviePatch(refreshed, first)?.expected_updated_at, item.updated_at)
})

test('validates field domains and effective trailer collisions before PATCH', () => {
  const item = movie()
  let draft = stageAdminMovieOverride(item, undefined, 'title', '   ')
  assert.equal(validateAdminMovieDraft(item, draft).title, 'Indiquez un titre.')
  assert.equal(buildAdminMoviePatch(item, draft), null)

  draft = stageAdminMovieOverride(item, undefined, 'runtime_minutes', 1.5)
  assert.match(validateAdminMovieDraft(item, draft).runtime_minutes ?? '', /entière/)

  draft = stageAdminMovieOverride(item, undefined, 'poster_url', 'http://example.test/image.jpg')
  assert.equal(validateAdminMovieDraft(item, draft).poster_url, 'Utilisez une URL HTTPS valide.')

  draft = stageAdminMovieOverride(item, undefined, 'trailer_vo_youtube_key', 'abcdefghijk')
  assert.match(validateAdminMovieDraft(item, draft).trailer_vo_youtube_key ?? '', /différentes/)
})

test('uses credentialed GET and PATCH contracts and decimal-string IDs', async () => {
  interface AdminMovieFetchOptions {
    method?: 'PATCH'
    credentials: 'include'
    query?: AdminMoviesQuery
    signal?: AbortSignal
    body?: AdminMoviePatchRequest
  }
  const calls: Array<{ url: string, options: AdminMovieFetchOptions }> = []
  const item = movie()
  const response: AdminMoviesResponse = { items: [item], total: 1, limit: 50, offset: 0 }
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: AdminMovieFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve(options.method === 'PATCH' ? item : response)
    }
  })
  const query: AdminMoviesQuery = { limit: 50, offset: 0, override_status: 'all', sort: 'title', direction: 'asc' }
  const patch: AdminMoviePatchRequest = { expected_updated_at: item.updated_at, overrides: { title: 'Nouveau titre' } }
  const api = useMesSeancesApi()
  await api.adminMovies(query)
  await api.adminUpdateMovie(item.id, patch)
  assert.deepEqual(calls, [
    {
      url: 'http://localhost:8080/api/v1/admin/movies',
      options: { credentials: 'include', query, signal: undefined }
    },
    {
      url: 'http://localhost:8080/api/v1/admin/movies/9007199254740993',
      options: { method: 'PATCH', credentials: 'include', body: patch }
    }
  ])
})

test('maps exact admin movie errors to safe French messages', () => {
  const failure = (code: string) => ({ data: { error: { code } } })
  assert.equal(getFrenchAdminApiError(failure('invalid_admin_movie_query')), 'Filtres de films invalides.')
  assert.equal(getFrenchAdminApiError(failure('invalid_admin_movie_update')), 'Modifications de film invalides.')
  assert.equal(getFrenchAdminApiError(failure('admin_movie_not_found')), 'Film introuvable.')
  assert.equal(getFrenchAdminApiError(failure('admin_movie_conflict')), 'Ce film a changé. La liste a été actualisée.')
  assert.equal(getFrenchAdminApiError(failure('admin_movie_list_failed')), 'Impossible de charger les films.')
  assert.equal(getFrenchAdminApiError(failure('admin_movie_update_failed')), 'Impossible d’enregistrer le film.')
})

test('keeps grid client-only, authenticated, literal, synchronous, and Community-only', async () => {
  const [grid, actions, page, packageJson, lock] = await Promise.all([
    readFile(new URL('../app/components/admin/AdminMoviesGrid.client.vue', import.meta.url), 'utf8'),
    readFile(new URL('../app/components/admin/AdminMoviesActionsCell.vue', import.meta.url), 'utf8'),
    readFile(new URL('../app/pages/admin/movies.vue', import.meta.url), 'utf8'),
    readFile(new URL('../package.json', import.meta.url), 'utf8'),
    readFile(new URL('../package-lock.json', import.meta.url), 'utf8')
  ])
  assert.match(page, /definePageMeta\(\{ middleware: 'admin-auth' \}\)/)
  assert.match(grid, /import AdminMoviesActionsCell from/)
  assert.match(grid, /import AdminMoviesMetadataCell from/)
  assert.match(grid, /InfiniteRowModelModule/)
  assert.match(grid, /:cache-block-size="ADMIN_MOVIE_PAGE_SIZE"/)
  assert.match(grid, /:max-blocks-in-cache="4"/)
  assert.match(grid, /pinned: 'left'/)
  assert.match(grid, /pinned: 'right'/)
  assert.match(grid, /const NARROW_VIEWPORT_QUERY = '\(max-width: 767px\)'/)
  assert.match(grid, /\{ colId: 'title', pinned: narrow \? null : 'left' \}/)
  assert.match(grid, /\{ colId: 'actions', pinned: narrow \? null : 'right' \}/)
  assert.match(grid, /addEventListener\('change', onViewportWidthChange\)/)
  assert.match(grid, /removeEventListener\('change', onViewportWidthChange\)/)
  assert.match(actions, /aria-controls/)
  assert.match(grid, />Restaurer la valeur automatique</)
  assert.doesNotMatch(grid + actions + page, /\bv-html\b/)
  assert.doesNotMatch(grid + actions + page + packageJson + lock, /ag-grid-enterprise|AllEnterpriseModule|MasterDetailModule/)
  const parsedPackage = JSON.parse(packageJson)
  assert.equal(parsedPackage.dependencies['ag-grid-community'], '36.1.0')
  assert.equal(parsedPackage.dependencies['ag-grid-vue3'], '36.1.0')
})
