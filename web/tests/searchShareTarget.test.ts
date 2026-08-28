import assert from 'node:assert/strict'
import test from 'node:test'
import { buildCompleteSearchShareTarget } from '../app/utils/searchShareTarget.ts'
import { withSharedTheaterSelection } from '../app/utils/sharedTheaterSelection.ts'
import { isValidShortLinkTarget } from '../app/utils/shortLinkTarget.ts'

test('builds a deterministic complete target with explicit default values', () => {
  const target = buildCompleteSearchShareTarget({
    theaterIds: ['ugc-25', 'kinepolis_42'],
    date: '2026-08-22',
    startAfter: '18:00',
    finishBefore: '23:30',
    language: 'ALL',
    format: 'ALL',
    includeAds: true,
    bufferAds: 15,
    grouping: 'movie',
    layout: 'lines'
  })

  assert.equal(
    target,
    '/recherche?theaters=ugc-25%2Ckinepolis_42&date=2026-08-22&start_after=18%3A00&finish_before=23%3A30&language=ALL&format=ALL&include_ads=1&buffer_ads=15&grouping=movie&layout=lines'
  )
})

test('preserves non-default filters and presentation in a valid short-link target', () => {
  const target = buildCompleteSearchShareTarget({
    theaterIds: ['ugc-25', 'ugc-26'],
    date: '2026-08-23',
    startAfter: '20:15',
    finishBefore: '01:00',
    language: 'VOSTFR',
    format: 'IMAX',
    includeAds: false,
    bufferAds: 20,
    grouping: 'chronological',
    layout: 'boxes'
  })
  const sharedTarget = withSharedTheaterSelection(target, ['ugc-25', 'ugc-26'])

  assert.equal(
    sharedTarget,
    '/recherche?theaters=ugc-25%2Cugc-26&date=2026-08-23&start_after=20%3A15&finish_before=01%3A00&language=VOSTFR&format=IMAX&include_ads=0&buffer_ads=20&grouping=chronological&layout=boxes&shared_theaters=ugc-25%2Cugc-26'
  )
  assert.equal(isValidShortLinkTarget(sharedTarget!), true)
  assert.equal(new URLSearchParams(sharedTarget!.split('?')[1]).get('theaters'), new URLSearchParams(sharedTarget!.split('?')[1]).get('shared_theaters'))
})
