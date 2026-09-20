import assert from 'node:assert/strict'
import test from 'node:test'
import {
  parseShowtimeSelection,
  serializeShowtimeSelection,
  showtimeSelectionQueryValues,
} from '../app/utils/showtimeResults.ts'
import { buildCompleteSearchShareTarget } from '../app/utils/searchShareTarget.ts'
import { withSharedTheaterSelection } from '../app/utils/sharedTheaterSelection.ts'
import { isValidShortLinkTarget } from '../app/utils/shortLinkTarget.ts'

const platforms = ['cineoffice', 'ticketingcine', 'webediamovies']
const hash = 'a'.repeat(64)
const keys = platforms.map(
  (platform) => `cinewest:cinewest-showing-${platform}-${hash}`,
)

test('Cinewest selection tokens preserve all platform namespaces and canonical SHA-256 bits', () => {
  const encoded = Buffer.from(hash, 'hex').toString('base64url')
  assert.equal(
    serializeShowtimeSelection([...keys, keys[0]!].reverse()),
    platforms.map((platform) => `w${platform}-${encoded}`).join(','),
  )
  assert.deepEqual(
    parseShowtimeSelection(serializeShowtimeSelection(keys)),
    keys,
  )
  for (const bad of [
    `wcineoffice-${encoded}=`,
    `wcineoffice-${encoded.slice(0, -1)}r`,
    `wcineoffice-${encoded}\n`,
    `wunknown-${encoded}`,
    `wcineoffice-${encoded.slice(1)}`,
    `wcineoffice-${encoded}A`,
    `wcineoffice-${hash}`,
  ]) {
    assert.deepEqual(parseShowtimeSelection(bad), [], bad)
  }
  for (const bad of [
    keys[0] + '\n',
    keys[0]!.replace(hash, hash.toUpperCase()),
    keys[0]!.replace('cineoffice', 'unknown'),
    keys[0]!.slice(0, -1),
  ]) {
    assert.equal(serializeShowtimeSelection([bad]), undefined, bad)
  }
  assert.equal(showtimeSelectionQueryValues(keys, true).selected_only, '1')
})

test('complete Cinewest shared search round-trips theaters and selected-only sessions across all platforms', () => {
  const theaterIds = [
    'cinewest-cineoffice-royanlelido',
    'cinewest-ticketingcine-EMS0042',
    'cinewest-webediamovies-W8400',
  ]
  const target = buildCompleteSearchShareTarget({
    theaterIds,
    date: '2027-06-27',
    startAfter: '18:00',
    finishBefore: '23:30',
    language: 'ALL',
    format: 'ALL',
    includeAds: false,
    bufferAds: 15,
    grouping: 'chronological',
    layout: 'boxes',
    selectedShowtimeKeys: keys,
    selectedOnly: true,
  })
  const shared = withSharedTheaterSelection(target, theaterIds)!
  assert.equal(isValidShortLinkTarget(shared), true)
  const query = new URL(shared, 'https://messeances.fr').searchParams
  assert.equal(query.get('shared_theaters'), theaterIds.join(','))
  assert.equal(query.get('selected_only'), '1')
  assert.deepEqual(parseShowtimeSelection(query.get('selected')!), keys)
  assert.equal(
    serializeShowtimeSelection(parseShowtimeSelection(query.get('selected')!)),
    query.get('selected'),
  )
})
