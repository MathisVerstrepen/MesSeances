import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { getFrenchActivityApiError } from '../app/composables/useMesSeancesApi.ts'
import type { TheaterActivityItem } from '../app/types/api.ts'
import {
  activityFullDate,
  activityHistoryDate,
  activityObservationDay,
  activityShortDate,
  activityShowtimesTarget,
  activityTypeLabel,
  appendActivityItems,
  groupActivityItems,
} from '../app/utils/cinemaActivity.ts'
import { cinemaMovieTarget } from '../app/utils/cinemaMovieTarget.ts'
import { mergeOwnedQuery } from '../app/utils/routeQuery.ts'

const [cinema, activity, api, types] = await Promise.all([
  readFile(new URL('../app/pages/cinema/[slug].vue', import.meta.url), 'utf8'),
  readFile(
    new URL('../app/components/CinemaActivity.vue', import.meta.url),
    'utf8',
  ),
  readFile(
    new URL('../app/composables/useMesSeancesApi.ts', import.meta.url),
    'utf8',
  ),
  readFile(new URL('../app/types/api.ts', import.meta.url), 'utf8'),
])

function item(
  eventId: string,
  timestamp = '2026-10-01T22:30:00.123456Z',
  overrides: Partial<TheaterActivityItem> = {},
): TheaterActivityItem {
  return {
    event_id: eventId,
    type: 'added_to_program',
    detected_at: timestamp,
    first_screening_date: '2026-10-09',
    previous_program_end_date: null,
    movie: {
      slug: 'film-42',
      title: 'Une programmation',
      poster_url: null,
      updated_at: timestamp,
    },
    has_upcoming_showtimes: false,
    next_showtime_date: null,
    ...overrides,
  }
}

test('activity observation days use Paris calendar midnight, not UTC or cinema service day', () => {
  assert.equal(
    activityObservationDay('2026-10-01T21:59:59.999999Z'),
    '2026-10-01',
  )
  assert.equal(
    activityObservationDay('2026-10-01T22:00:00.000001Z'),
    '2026-10-02',
  )
  assert.equal(activityObservationDay('2026-12-31T23:00:00Z'), '2027-01-01')
  assert.equal(activityObservationDay('2026-03-29T22:01:00Z'), '2026-03-30')
  assert.equal(activityObservationDay('2026-10-25T23:01:00Z'), '2026-10-26')
  assert.equal(activityObservationDay('invalid'), '')
})

test('activity full dates keep year for historic observation and announcement dates', () => {
  assert.equal(activityFullDate('2027-01-01'), 'vendredi 1 janvier 2027')
  assert.equal(activityFullDate('2026-10-09'), 'vendredi 9 octobre 2026')
  assert.equal(activityFullDate('2026-02-30'), '2026-02-30')
})

test('activity concise dates retain the year, omit weekdays and use French month names', () => {
  assert.equal(activityShortDate('2026-10-09'), '9 oct. 2026')
  assert.equal(activityShortDate('2026-12-16'), '16 déc. 2026')
  assert.equal(activityShortDate('2027-01-01'), '1 janv. 2027')
  assert.equal(activityShortDate('2024-02-29'), '29 févr. 2024')
  assert.equal(activityHistoryDate('2026-09-28'), '28 septembre 2026')
  assert.equal(activityHistoryDate('2027-01-01'), '1 janvier 2027')
  for (const date of [
    '',
    'invalid',
    '2026-02-30',
    '2026-13-01',
    '2026-10-09T12:00:00Z',
  ]) {
    assert.equal(activityShortDate(date), date)
    assert.equal(activityHistoryDate(date), date)
  }
})

test('append merges same-day pages and deduplicates decimal string IDs without numeric conversion', () => {
  const first = [item('9223372036854775807'), item('9007199254740993')]
  const second = [
    item('9007199254740993'),
    item('9007199254740992'),
    item('1', '2026-10-01T20:00:00Z'),
  ]
  const items = appendActivityItems(first, second)
  assert.deepEqual(
    items.map((entry) => entry.event_id),
    ['9223372036854775807', '9007199254740993', '9007199254740992', '1'],
  )
  assert.equal(items[1], first[1])
  const groups = groupActivityItems(items)
  assert.deepEqual(
    groups.map((group) => [group.day, group.items.length]),
    [
      ['2026-10-02', 3],
      ['2026-10-01', 1],
    ],
  )
  assert.equal(first.length, 2)
  assert.equal(second.length, 3)
  assert.deepEqual(appendActivityItems([], [first[0]!, first[0]!]), [first[0]])
  assert.deepEqual(groupActivityItems([]), [])
})

test('activity labels and previous programming date distinguish returns from additions', () => {
  assert.equal(
    activityTypeLabel('added_to_program'),
    'Ajout à la programmation',
  )
  assert.equal(activityTypeLabel('return_to_program'), 'Retour à l’affiche')
  assert.match(
    activity,
    /item\.type === 'return_to_program' && item\.previous_program_end_date/,
  )
  assert.match(activity, /Première séance annoncée ·/)
  assert.match(activity, /Programmation précédente · jusqu’au/)
  assert.match(activity, /activityShortDate\(item\.first_screening_date\)/)
  assert.match(activity, /activityShortDate\(item\.previous_program_end_date\)/)
  assert.doesNotMatch(activity, /release_date|french_release_date/)
})

test('activity uses compact explicit badges, a narrower date column and quiet day separators', () => {
  assert.match(activity, /inline-flex border px-2 py-px align-top text-xs/)
  assert.match(
    activity,
    /'border-accent\/40 bg-accent-soft text-accent' : 'border-ink\/30 text-ink'/,
  )
  assert.match(activity, /lg:grid-cols-\[180px_minmax\(0,1fr\)\]/)
  assert.match(activity, /border-b border-ink\/15/)
  assert.match(activity, /<ul class="space-y-6">/)
  assert.doesNotMatch(activity, /capitalize|border-l border-ink/)
  assert.match(
    activity,
    /class="inline-flex min-h-11 items-center text-sm font-bold underline/,
  )
  assert.doesNotMatch(activity, /class="mt-3 inline-flex/)
})

test('activity date rail omits desktop weekdays while preserving full mobile dates in one time element', () => {
  assert.equal(activityHistoryDate('2026-10-01'), '1 octobre 2026')
  assert.equal(activityFullDate('2026-10-01'), 'jeudi 1 octobre 2026')
  assert.match(
    activity,
    /<time :datetime="group\.day">\s*<span class="lg:hidden">{{ activityFullDate\(group\.day\) }}<\/span>\s*<span class="hidden lg:inline">{{\s*activityHistoryDate\(group\.day\)\s*}}<\/span>\s*<\/time>/,
  )
  assert.match(activity, /<h3 class="mb-4 text-base font-bold lg:mb-0">/)
})

test('activity titles align with poster tops while retaining touch targets and long-title wrapping', () => {
  assert.match(activity, /class="flex items-start gap-4"/)
  assert.match(activity, /<div class="min-w-0 flex-1">/)
  assert.match(activity, /<h4 class="editorial-heading break-words">/)
  assert.match(
    activity,
    /class="flex min-h-11 min-w-11 w-fit max-w-full items-start[^"]*"\s*>\s*<span class="min-w-0">{{ item\.movie\.title }}<\/span>/,
  )
})

test('history coverage stays available in a native keyboard and touch disclosure', () => {
  assert.match(activity, /<details v-if="historyStart"/)
  assert.match(
    activity,
    /<summary\s+class="min-h-11 w-fit max-w-full cursor-pointer/,
  )
  assert.match(activity, /activityHistoryDate\(historyStart\)/)
  assert.match(
    activity,
    /<p[^>]*>\s*Les programmations antérieures ne sont pas reconstituées\.\s*<\/p>\s*<\/details>/,
  )
  assert.doesNotMatch(activity, /role="tooltip"|title="Les programmations/)
})

test('film targets preserve cinema; session CTA uses current next date and schedule anchor only', () => {
  const ended = item('1')
  assert.equal(activityShowtimesTarget(ended, 'ugc-25'), null)
  assert.equal(
    activityShowtimesTarget(
      { ...ended, next_showtime_date: '2026-11-01' },
      'ugc-25',
    ),
    null,
  )
  assert.equal(
    activityShowtimesTarget(
      { ...ended, has_upcoming_showtimes: true },
      'ugc-25',
    ),
    null,
  )
  assert.equal(
    activityShowtimesTarget(
      { ...ended, has_upcoming_showtimes: true, next_showtime_date: 'invalid' },
      'ugc-25',
    ),
    null,
  )
  const upcoming = {
    ...ended,
    has_upcoming_showtimes: true,
    next_showtime_date: '2026-11-01',
  }
  const target = new URL(
    activityShowtimesTarget(upcoming, 'ugc-25')!,
    'https://messeances.fr',
  )
  assert.equal(target.pathname, '/film/film-42')
  assert.equal(target.searchParams.get('shared_theaters'), 'ugc-25')
  assert.equal(target.searchParams.get('date'), '2026-11-01')
  assert.equal(target.hash, '#schedule-heading')
  assert.equal(
    new URL(
      cinemaMovieTarget(ended.movie.slug, 'ugc-25'),
      target,
    ).searchParams.has('date'),
    false,
  )
  assert.match(
    activity,
    /v-if="activityShowtimesTarget\(item, response\.theater\.id\)"/,
  )
  assert.doesNotMatch(activity, /selectedDate|route\.query|Date\.now/)
})

test('activity tab retains owned-query navigation and existing cinema SEO/share/showtimes/Films', () => {
  assert.match(
    cinema,
    /enumQueryValue\(singularQueryValue\(route\.query\.view\), \[\s*'films',\s*'activity',?\s*\]\)\s*\?\?\s*'showtimes'/,
  )
  assert.match(
    cinema,
    /function viewQuery\(view: 'showtimes' \| 'films' \| 'activity'\)/,
  )
  assert.match(cinema, /view: view === 'showtimes' \? undefined : view/)
  const query = mergeOwnedQuery(
    {
      view: 'films',
      q: 'film',
      sort: 'title',
      date: '2026-11-01',
      layout: 'boxes',
      grouping: 'chronological',
      shared_theaters: ['ugc-25'],
      other: 'keep',
    },
    ['view', 'q', 'sort'],
    { view: 'activity', q: undefined, sort: undefined },
  )
  assert.deepEqual(query, {
    view: 'activity',
    date: '2026-11-01',
    layout: 'boxes',
    grouping: 'chronological',
    shared_theaters: ['ugc-25'],
    other: 'keep',
  })
  assert.match(cinema, /grid-cols-3/)
  for (const view of ['showtimes', 'films', 'activity']) {
    assert.match(cinema, new RegExp(`id="cinema-${view}-heading"`))
    assert.match(
      cinema,
      new RegExp(
        `:aria-current="currentView === '${view}' \\? 'page' : undefined"`,
      ),
    )
  }
  assert.match(cinema, /:aria-labelledby="`cinema-\$\{currentView\}-heading`"/)
  assert.equal((cinema.match(/<ShareButton\b/g) ?? []).length, 1)
  assert.match(
    cinema,
    /Object\.keys\(route\.query\)\.length === 0\s*\? 'index,follow'\s*: 'noindex,follow'/,
  )
  assert.match(
    cinema,
    /link: \[\{ rel: 'canonical', href: canonicalUrl\.value \}\]/,
  )
  assert.match(cinema, /'@type': 'MovieTheater'/)
  assert.match(cinema, /'@type': 'BreadcrumbList'/)
  assert.match(cinema, /for \(const showtime of current\.showtimes\)/)
  assert.match(cinema, /'@type': 'ScreeningEvent'/)
  assert.doesNotMatch(
    activity,
    /useSeoMeta|useHead|ScreeningEvent|ShareButton|subscribe|notification/i,
  )
})

test('first activity fetch is active-branch-only, SSR/hydration keyed by cinema rather than display/date', () => {
  assert.match(
    cinema,
    /<CinemaActivity\s+v-else-if="currentView === 'activity'"\s+:key="slug"\s+:slug="slug"/,
  )
  assert.doesNotMatch(cinema, /api\.theaterActivity/)
  assert.match(activity, /const activitySlug = props\.slug/)
  assert.match(
    activity,
    /await useAsyncData\(\s*`cinema-activity:\$\{activitySlug\}`/,
  )
  assert.match(activity, /\{ lazy: true \}/)
  assert.match(activity, /const PAGE_SIZE = 20/)
  assert.match(
    api,
    /theaterActivity\([\s\S]*?encodeURIComponent\(slug\)\}\/activity`,\s*\{ query: queryValues\(query\), signal, retry: false \}/,
  )
  assert.match(types, /event_id: string/)
  assert.match(types, /history_started_at: string \| null/)
  assert.match(types, /return_minimum_break_days: 28/)
})

test('activity cancels/invalidate pending first and more requests on slug changes and unmount', () => {
  assert.ok(
    activity.indexOf('onScopeDispose(') <
      activity.indexOf('await useAsyncData('),
  )
  assert.match(
    activity,
    /onScopeDispose\(\(\) => \{\s*disposed = true\s*invalidateRequests\(\)/,
  )
  assert.match(
    activity,
    /watch\(\(\) => props\.slug, invalidateRequests, \{ flush: 'sync' \}\)/,
  )
  assert.match(
    activity,
    /requestId\+\+\s*initialController\?\.abort\(\)\s*moreController\?\.abort\(\)/,
  )
  assert.match(
    activity,
    /!disposed && props\.slug === activitySlug && id === requestId/,
  )
  assert.match(
    activity,
    /AbortSignal\.any\(\[signal, initialController\.signal\]\)/,
  )
  assert.match(
    activity,
    /if \(!isCurrentRequest\(id\) \|\| response\.value\?\.next_cursor !== cursor\) return/,
  )
  assert.match(
    activity,
    /if \(disposed \|\| props\.slug !== activitySlug \|\| !state\) return/,
  )
})

test('activity handles four states, baseline notice and safe pagination with retained rows/cursor on error', () => {
  assert.match(activity, /:aria-busy="firstPending \|\| morePending"/)
  assert.match(activity, /v-if="firstPending"/)
  assert.match(activity, /Chargement de l’activité…/)
  assert.match(activity, /motion-safe:animate-pulse/)
  assert.match(activity, /v-else-if="firstError \|\| !response"/)
  assert.match(activity, /Impossible de charger l’activité/)
  assert.match(activity, /@click="retryInitial"/)
  assert.match(activity, /Historique suivi depuis le/)
  assert.match(
    activity,
    /Les programmations antérieures ne sont pas reconstituées/,
  )
  assert.match(activity, /v-if="!response\.coverage\.history_started_at"/)
  assert.match(activity, /Historique en cours d’initialisation/)
  assert.match(activity, /Aucune nouvelle programmation détectée/)
  assert.match(activity, /:key="item\.event_id"/)
  assert.match(activity, /<PosterImage/)
  assert.match(activity, /v-if="response\.next_cursor"/)
  assert.match(activity, /:disabled="morePending"/)
  assert.match(activity, /'Réessayer' : 'Afficher plus'/)
  assert.match(activity, /\{ limit: PAGE_SIZE, cursor \}/)
  assert.match(
    activity,
    /items: appendActivityItems\(response\.value\.items, page\.items\)/,
  )
  assert.match(
    activity,
    /catch \(cause\) \{\s*if \(isCurrentRequest\(id\)\) moreError\.value = getFrenchActivityApiError\(cause\)\s*\}/,
  )
})

test('activity error mapping is French, actionable and does not render raw API/transport details', () => {
  for (const code of [
    'history_busy',
    'history_query_timeout',
    'history_unavailable',
    'not_found',
    'invalid_query',
    'internal_error',
  ]) {
    const message = getFrenchActivityApiError({
      data: { error: { code, message: 'secret SQL details' } },
    })
    assert.doesNotMatch(message, /secret|SQL/)
    assert.match(message, /[Rr]éessaye[rz]|liste des cinémas|Actualisez/)
  }
  assert.match(
    getFrenchActivityApiError(new Error('secret')),
    /Vérifiez votre connexion/,
  )
  assert.match(getFrenchActivityApiError({ status: 429 }), /occupé/)
  assert.match(getFrenchActivityApiError({ status: 503 }), /indisponible/)
})
