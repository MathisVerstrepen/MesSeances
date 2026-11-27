import assert from 'node:assert/strict'
import test from 'node:test'
import type { AccountNotification } from '../app/types/notifications.ts'
import {
  formatNotificationTime,
  notificationRuleTheaterCount,
  notificationScopeLabel,
  presentNotification,
  unreadBadge,
} from '../app/utils/notifications.ts'

function notification(
  overrides: Partial<AccountNotification> = {},
): AccountNotification {
  return {
    id: '1',
    kind: 'cinema_activity',
    rule_id: '7',
    created_at: '2026-11-26T10:00:00Z',
    read: false,
    event_count: 1,
    movie_count: 1,
    theater_count: 1,
    movies: [
      {
        slug: 'film-2',
        title: 'Dune',
        poster_url: null,
        updated_at: '2026-11-26T10:00:00Z',
      },
    ],
    theaters: [
      { id: 'ugc-1', slug: 'ugc-1', name: 'UGC Les Halles', city: 'Paris' },
    ],
    ...overrides,
  }
}

test('watchlist notifications link to the film in the first cinema', () => {
  const view = presentNotification(
    notification({ kind: 'watchlist_screening', theater_count: 3 }),
  )
  assert.equal(view.context, 'Watchlist')
  assert.equal(
    view.headline,
    'Dune est programmé à UGC Les Halles et 2 autres cinémas',
  )
  assert.match(view.target, /^\/film\/film-2\?/)
  assert.match(view.target, /ugc-1/)
  const unresolved = presentNotification(
    notification({ kind: 'watchlist_screening', movies: [], theaters: [] }),
  )
  assert.equal(
    unresolved.headline,
    'Un film de votre watchlist est programmé à 1 cinéma',
  )
  assert.equal(unresolved.target, '/compte/watchlist')
})

test('activity notifications summarize films and link to the feed', () => {
  assert.deepEqual(presentNotification(notification()), {
    context: 'Activité des cinémas',
    headline: 'Dune arrive à UGC Les Halles',
    detail: '',
    target: '/compte/activite',
  })
  const many = presentNotification(
    notification({
      movie_count: 5,
      theater_count: 2,
      movies: ['Dune', 'Wicked', 'Nosferatu'].map((title, index) => ({
        slug: `film-${index}`,
        title,
        poster_url: null,
        updated_at: '2026-11-26T10:00:00Z',
      })),
    }),
  )
  assert.equal(
    many.headline,
    '5 nouveaux films à UGC Les Halles et 1 autre cinéma',
  )
  assert.equal(many.detail, 'Dune, Wicked, Nosferatu et 2 autres')
  const pair = presentNotification(
    notification({
      movie_count: 2,
      movies: ['Dune', 'Wicked'].map((title) => ({
        slug: title,
        title,
        poster_url: null,
        updated_at: '2026-11-26T10:00:00Z',
      })),
    }),
  )
  assert.equal(pair.detail, 'Dune et Wicked')
})

test('relative times and badges stay compact', () => {
  const now = new Date('2026-11-26T12:00:00Z')
  assert.equal(
    formatNotificationTime('2026-11-26T11:59:30Z', now),
    'à l’instant',
  )
  assert.equal(
    formatNotificationTime('2026-11-26T11:15:00Z', now),
    'il y a 45 min',
  )
  assert.equal(
    formatNotificationTime('2026-11-26T07:00:00Z', now),
    'il y a 5 h',
  )
  assert.equal(formatNotificationTime('2026-11-25T08:00:00Z', now), 'hier')
  assert.equal(
    formatNotificationTime('2026-11-22T12:00:00Z', now),
    'il y a 4 j',
  )
  assert.equal(formatNotificationTime('2026-10-12T12:00:00Z', now), '12 oct.')
  assert.equal(formatNotificationTime('invalid', now), '')
  assert.equal(unreadBadge(0), '')
  assert.equal(unreadBadge(7), '7')
  assert.equal(unreadBadge(120), '99+')
})

test('rule scopes resolve their current cinema counts', () => {
  assert.equal(
    notificationRuleTheaterCount(
      { theater_scope: 'followed', theater_ids: [] },
      3,
      5,
    ),
    3,
  )
  assert.equal(
    notificationRuleTheaterCount(
      { theater_scope: 'preferred', theater_ids: [] },
      3,
      5,
    ),
    5,
  )
  assert.equal(
    notificationRuleTheaterCount(
      { theater_scope: 'custom', theater_ids: ['a', 'b'] },
      3,
      5,
    ),
    2,
  )
  assert.equal(
    notificationScopeLabel('followed', 1),
    'Mes cinémas suivis (1 cinéma)',
  )
  assert.equal(
    notificationScopeLabel('custom', 2),
    'Sélection personnalisée (2 cinémas)',
  )
})

test('custom cinema search ignores accents and case and is bounded', async () => {
  const { filterNotificationTheaters } = await import(
    '../app/utils/notifications.ts'
  )
  const theaters = [
    { id: 'a', name: 'UGC Ciné Cité Les Halles', city: 'Paris' },
    { id: 'b', name: 'Pathé Wepler', city: 'Paris' },
    { id: 'c', name: 'Le Méliès', city: 'Montreuil' },
  ]
  assert.deepEqual(
    filterNotificationTheaters(theaters, 'cine cite').map((t) => t.id),
    ['a'],
  )
  assert.deepEqual(
    filterNotificationTheaters(theaters, 'MELIES montreuil').map((t) => t.id),
    ['c'],
  )
  assert.deepEqual(
    filterNotificationTheaters(theaters, 'paris', 1).map((t) => t.id),
    ['a'],
  )
  assert.deepEqual(filterNotificationTheaters(theaters, '  '), [])
})
