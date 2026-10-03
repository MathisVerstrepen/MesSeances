import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { AccountApiError } from '../app/utils/accountState.ts'
import {
  activityShowtimesTarget,
  appendActivityItems,
  groupActivityItems,
} from '../app/utils/cinemaActivity.ts'
import {
  deferred,
  feed,
  fixture,
  session,
  settle,
  snapshot,
} from './helpers/cinemaFollowsHarness.ts'

test('generic activity helpers preserve attribution and server order, deduplicate only event identity', () => {
  const items = feed('1', ['100', '99']).items
  const all = appendActivityItems(items, [
    items[1]!,
    ...feed('1', ['98']).items,
  ])
  assert.deepEqual(
    all.map((item) => item.event_id),
    ['100', '99', '98'],
  )
  assert.equal(groupActivityItems(all)[0]!.items[1]!.theater.id, 'cinema-b')
  assert.match(
    activityShowtimesTarget(items[1]!, items[1]!.theater.id)!,
    /shared_theaters=cinema-b&date=2026-10-03#schedule-heading/,
  )
})

test('feed begins only after client mount; no shared, SSR, URL, storage or private payload state', async () => {
  const f = await fixture(false)
  try {
    f.admit()
    await settle()
    await f.mountActivity()
    assert.equal(f.cursors.length, 0)
    for (const name of [
      'useAccountActivity.ts',
      '../pages/compte/activite.vue',
    ]) {
      const source = await readFile(
        new URL(`../app/composables/${name}`, import.meta.url),
        'utf8',
      )
      assert.doesNotMatch(
        source,
        /useState|useAsyncData|localStorage|sessionStorage|router\.replace|router\.push/,
      )
    }
  } finally {
    f.stop()
  }
})

test('continuation serialized, ordinary failure keeps rows and cursor; retry appends by event ID', async () => {
  const f = await fixture()
  try {
    f.admit()
    f.setActivity(async () => feed('0', ['100', '99'], 'page-2'))
    await settle()
    const page = await f.mountActivity()
    const held = deferred<ReturnType<typeof feed>>()
    f.setActivity(() => held.promise)
    const pending = page.loadMore()
    await page.loadMore()
    assert.equal(f.cursors.length, 2)
    held.reject(new AccountApiError(503))
    await pending
    assert.deepEqual(
      page.response.value?.items.map((item) => item.event_id),
      ['100', '99'],
    )
    assert.equal(page.response.value?.next_cursor, 'page-2')
    assert.ok(page.moreError.value)
    f.setActivity(async () => feed('0', ['99', '98']))
    await page.loadMore()
    assert.deepEqual(
      page.response.value?.items.map((item) => item.event_id),
      ['100', '99', '98'],
    )
    assert.deepEqual(f.cursors, [undefined, 'page-2', 'page-2'])
  } finally {
    f.stop()
  }
})

test('409 continuation and newer known follows discard walk and request page one', async () => {
  const f = await fixture()
  try {
    f.admit()
    f.setActivity(async () => feed('0', ['100'], 'old'))
    await settle()
    const page = await f.mountActivity()
    f.setActivity(async (cursor) => {
      if (cursor) throw new AccountApiError(409, 'theater_follows_changed')
      return feed('1', ['200'], 'new')
    })
    await page.loadMore()
    assert.deepEqual(
      page.response.value?.items.map((item) => item.event_id),
      ['200'],
    )
    f.setSnapshot(snapshot('2', ['cinema-b']))
    f.setActivity(async () => feed('2', ['300']))
    await f.account.revalidate()
    await settle()
    assert.deepEqual(
      page.response.value?.items.map((item) => item.event_id),
      ['300'],
    )
    assert.equal(f.posts.length, 0)
    assert.equal(f.cursors.at(-1), undefined)
  } finally {
    f.stop()
  }
})

for (const transition of [
  'logout',
  'owner',
  'departure',
  'unmount',
  'offline',
  'pagehide',
  'expiry',
]) {
  for (const outcome of ['success', 'error']) {
    test(`late feed ${outcome} ignored after ${transition}`, async () => {
      const f = await fixture()
      try {
        f.admit()
        await settle()
        const held = deferred<ReturnType<typeof feed>>()
        f.setActivity(() => held.promise)
        const page = await f.mountActivity()
        if (transition === 'departure') f.depart()
        else if (transition === 'unmount') f.stop()
        else if (transition === 'owner') {
          f.setActivity(async () => feed('0', [], null, 'bob'))
          f.setSnapshot(snapshot('0', [], 'bob'))
          f.admit(session('bob'))
        } else f.account.clear()
        await settle()
        if (outcome === 'success') held.resolve(feed('0', ['private-old']))
        else held.reject(new AccountApiError(503))
        await settle()
        assert.equal(
          page.response.value?.items.some(
            (item) => item.event_id === 'private-old',
          ) ?? false,
          false,
        )
        assert.equal(page.error.value, '')
      } finally {
        f.stop()
      }
    })
  }
}

test('route departure unregisters page revalidation and same-route arrival resumes fresh first page', async () => {
  const f = await fixture()
  try {
    f.admit()
    f.setActivity(async () => feed('0', ['100']))
    await settle()
    const page = await f.mountActivity()
    f.depart()
    assert.equal(page.response.value, null)
    const count = f.cursors.length
    await f.account.revalidate()
    assert.equal(f.cursors.length, count)
    f.arrivals.forEach((fn) => fn())
    await settle()
    assert.equal(page.response.value?.items[0]?.event_id, '100')
  } finally {
    f.stop()
  }
})

test('page preserves empty, retry and originating-theater links without partial-history disclosure or footer actions', async () => {
  const source = await readFile(
    new URL('../app/pages/compte/activite.vue', import.meta.url),
    'utf8',
  )
  for (const text of [
    'Aucun cinéma suivi',
    'Explorer les cinémas',
    'Historique en cours d’initialisation',
    'Aucune nouvelle programmation détectée.',
    'Afficher plus',
    'Réessayer',
    'motion-safe:animate-pulse',
    'role="alert"',
    'item.theater.id',
    '?view=activity',
  ])
    assert.ok(source.includes(text), text)
  assert.doesNotMatch(
    source,
    /<details|<summary|Historique partiel|point de départ silencieux|programmations antérieures/,
  )
  assert.match(source, /<AccountShell[^>]*hide-explore[^>]*hide-logout/)
  const admission = await readFile(
    new URL('../app/middleware/account-auth.ts', import.meta.url),
    'utf8',
  )
  assert.match(admission, /'\/compte\/activite'/)
})

test('cinema attribution is top aligned with decorative existing provider logo and exact public activity badges', async () => {
  const source = await readFile(
    new URL('../app/pages/compte/activite.vue', import.meta.url),
    'utf8',
  )
  const publicActivity = await readFile(
    new URL('../app/components/CinemaActivity.vue', import.meta.url),
    'utf8',
  )
  const cinemaLink = source.match(
    /<NuxtLink\s+:to="`\/cinema\/[\s\S]*?<\/NuxtLink>/,
  )?.[0]
  assert.ok(cinemaLink)
  assert.match(cinemaLink, /:aria-label="item.theater.name"/)
  assert.match(cinemaLink, /items-start[^"\n]*leading-5/)
  assert.doesNotMatch(cinemaLink, /items-center/)
  assert.match(
    cinemaLink,
    /<TheaterName\s+:name="item.theater.name"\s+:provider="item.theater.provider"\s+decorative/,
  )
  assert.match(cinemaLink, /min-w-0 break-words \[overflow-wrap:anywhere\]/)
  assert.doesNotMatch(source, /<BrandLogo|providerBrands|\.webp|\.svg/)
  const badge = /<span\s+class="([^"]+)"\s+:class="([^"]+)"/
  const privateBadge = source.match(badge)
  const publicBadge = publicActivity.match(badge)
  assert.ok(privateBadge)
  assert.ok(publicBadge)
  assert.deepEqual(privateBadge.slice(1), publicBadge.slice(1))
  assert.equal(
    privateBadge[1],
    'inline-flex border px-2 py-px align-top text-xs font-medium leading-4',
  )
  assert.equal(
    privateBadge[2],
    "item.type === 'return_to_program' ? 'border-accent/40 bg-accent-soft text-accent' : 'border-ink/30 text-ink'",
  )
})

test('newer follows arriving before first page cannot leave blank stale walk', async () => {
  const f = await fixture()
  try {
    f.setSnapshot(snapshot('2', ['cinema-b']))
    f.admit()
    await settle()
    f.setActivity(async () => feed('1', ['old']))
    const page = await f.mountActivity()
    assert.equal(page.response.value, null)
    assert.equal(page.loading.value, false)
    assert.match(page.error.value, /cinémas suivis ont changé/)
    f.setActivity(async () => feed('2', ['new']))
    await page.refresh()
    assert.equal(page.response.value?.items[0]?.event_id, 'new')
  } finally {
    f.stop()
  }
})

test('movie title has small heading margins while links retain compact natural heights', async () => {
  const source = await readFile(
    new URL('../app/pages/compte/activite.vue', import.meta.url),
    'utf8',
  )
  const cinemaLink = source.match(
    /<NuxtLink\s+:to="`\/cinema\/[\s\S]*?<\/NuxtLink>/,
  )?.[0]
  const title = source.match(
    /<h3 class="editorial-heading my-1 break-words">[\s\S]*?<\/h3>/,
  )?.[0]
  assert.ok(cinemaLink)
  assert.ok(title)
  const titleLink = title.match(/<NuxtLink[\s\S]*?<\/NuxtLink\s*>/)?.[0]
  assert.ok(titleLink)
  assert.match(
    cinemaLink,
    /class="flex w-fit max-w-full items-start text-sm font-semibold leading-5/,
  )
  assert.match(
    title,
    /class="block w-fit max-w-full hover:underline underline-offset-4"/,
  )
  for (const link of [cinemaLink, titleLink])
    assert.doesNotMatch(
      link,
      /\b(?:min-h-|h-\d|py-|pt-|pb-|my-|mt-|mb-|inline-flex)/,
    )
  assert.match(title, /cinemaMovieTarget\(item.movie.slug, item.theater.id\)/)
})
