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
    layout: 'lines',
    selectedShowtimeKeys: []
  })

  assert.equal(
    target,
    '/recherche?theaters=ugc-25%2Ckinepolis_42&date=2026-08-22&start_after=18%3A00&finish_before=23%3A30&language=ALL&format=ALL&include_ads=1&buffer_ads=15&grouping=movie&layout=lines'
  )
})

test('omits an empty normalized screening selection', () => {
  const target = buildCompleteSearchShareTarget({
    theaterIds: ['ugc-25'],
    date: '2026-08-22',
    startAfter: '18:00',
    finishBefore: '23:30',
    language: 'ALL',
    format: 'ALL',
    includeAds: true,
    bufferAds: 15,
    grouping: 'movie',
    layout: 'lines',
    selectedShowtimeKeys: ['', '']
  })

  assert.equal(new URL(target, 'https://messeances.fr').searchParams.has('selected'), false)
})

test('preserves non-default filters and presentation with compact selections', () => {
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
    layout: 'boxes',
    selectedShowtimeKeys: ['ugc:ugc-showing-900', 'ugc:ugc-showing-12', 'ugc:ugc-showing-900']
  })
  const sharedTarget = withSharedTheaterSelection(target, ['ugc-25', 'ugc-26'])

  assert.equal(
    sharedTarget,
    '/recherche?theaters=ugc-25%2Cugc-26&date=2026-08-23&start_after=20%3A15&finish_before=01%3A00&language=VOSTFR&format=IMAX&include_ads=0&buffer_ads=20&grouping=chronological&layout=boxes&selected=u12%2Cu900&shared_theaters=ugc-25%2Cugc-26'
  )
  assert.equal(isValidShortLinkTarget(sharedTarget!), true)
})

test('builds the reported realistic compact selection as a valid short-link target', () => {
  const target = buildCompleteSearchShareTarget({
    theaterIds: ['cgr-W8010', 'pathe-cinema-pathe-lievin', 'cgr-P0798', 'cgr-P1016'],
    date: '2026-08-30',
    startAfter: '13:15',
    finishBefore: '22:45',
    language: 'ALL',
    format: 'ALL',
    includeAds: true,
    bufferAds: 15,
    grouping: 'chronological',
    layout: 'boxes',
    selectedShowtimeKeys: [
      'pathe:pathe-showing-V3001S170227',
      'cgr:cgr-showing-P1016-57435c8260ab73a85f6cd30038f21572df9b71a18c68467bef03566bdc5d36f2',
      'cgr:cgr-showing-P0798-eb8c701bf9eb902f738cb7a32ed14cb55b9e2b42e0fc346ac79d9cf11d171bbc',
      'cgr:cgr-showing-P1016-252b96e16cf563c832f2ffc5c35d55f318d15e4398708bc748fbb88482c0f052'
    ]
  })
  const sharedTarget = withSharedTheaterSelection(target, ['cgr-W8010', 'pathe-cinema-pathe-lievin', 'cgr-P0798', 'cgr-P1016'])

  assert.equal(
    sharedTarget,
    '/recherche?theaters=cgr-W8010%2Cpathe-cinema-pathe-lievin%2Ccgr-P0798%2Ccgr-P1016&date=2026-08-30&start_after=13%3A15&finish_before=22%3A45&language=ALL&format=ALL&include_ads=1&buffer_ads=15&grouping=chronological&layout=boxes&selected=cP0798-64xwG_nrkC9zjLejLtFMtVueK0Lg_DRqx52c8R0XG7w%2CcP1016-JSuW4Wz1Y8gy8v_Fw11V8xjRXkOYcIvHSPu4hILA8FI%2CcP1016-V0NcgmCrc6hfbNMAOPIVct-bcaGMaEZ77wNWa9xdNvI%2CpV3001S170227&shared_theaters=cgr-W8010%2Cpathe-cinema-pathe-lievin%2Ccgr-P0798%2Ccgr-P1016'
  )
  assert.equal(isValidShortLinkTarget(sharedTarget!), true)
  assert.equal(new URLSearchParams(sharedTarget!.split('?')[1]).get('theaters'), new URLSearchParams(sharedTarget!.split('?')[1]).get('shared_theaters'))
})
