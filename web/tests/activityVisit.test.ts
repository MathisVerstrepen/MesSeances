import assert from 'node:assert/strict'
import test from 'node:test'
import {
  activityEventID,
  latestActivityEventID,
  visitGuestActivity,
} from '../app/utils/activityVisit.ts'

test('activity visit IDs preserve int64 precision and reject corrupt local data', () => {
  for (const id of ['0', '1', '9007199254740993', '9223372036854775807'])
    assert.equal(activityEventID(id), id)
  for (const id of [null, '', '01', '-1', '1.2', '9223372036854775808', '{}'])
    assert.equal(activityEventID(id), null)
  assert.equal(latestActivityEventID([]), '0')
  assert.equal(
    latestActivityEventID([
      { event_id: '9007199254740992' },
      { event_id: '9007199254740993' },
      { event_id: '1' },
    ]),
    '9007199254740993',
  )
})

test('guest visits retain separate feed markers and never regress across older pages or tabs', () => {
  const values = new Map<string, string>()
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value)
    },
  }
  assert.equal(
    visitGuestActivity(storage, 'theater:ugc-1', '9007199254740993'),
    '0',
  )
  assert.equal(
    visitGuestActivity(storage, 'theater:ugc-1', '9007199254740994'),
    '9007199254740993',
  )
  assert.equal(
    visitGuestActivity(storage, 'theater:ugc-1', '10'),
    '9007199254740994',
  )
  assert.equal(
    visitGuestActivity(storage, 'theater:ugc-1', '0'),
    '9007199254740994',
  )
  assert.equal(visitGuestActivity(storage, 'theater:ugc-2', '20'), '0')
  values.set('messeances:activity-visit:v1:theater:ugc-3', '{invalid}')
  assert.equal(visitGuestActivity(storage, 'theater:ugc-3', '30'), '0')
})
