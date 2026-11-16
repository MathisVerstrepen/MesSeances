import assert from 'node:assert/strict'
import test from 'node:test'
import { AccountApiError } from '../app/utils/accountState.ts'
import {
  deferred,
  feed,
  fixture,
  settle,
} from './helpers/cinemaFollowsHarness.ts'

function manualFeed(ids = ['9007199254740993', '9007199254740992']) {
  const value = feed('0', ids, 'older-page')
  value.reading.manual_reading = true
  return value
}

test('manual badge toggles only its activity, preserves precise IDs, and publishes committed state', async () => {
  const f = await fixture()
  try {
    f.admit()
    f.setActivity(async () => manualFeed())
    await settle()
    const page = await f.mountActivity()
    assert.equal(page.response.value?.reading.manual_reading, true)
    await page.toggleConsulted('9007199254740993')
    assert.equal(page.response.value?.items[0]?.consulted, true)
    assert.equal(page.response.value?.items[1]?.consulted, false)
    await page.toggleConsulted('9007199254740993')
    assert.equal(page.response.value?.items[0]?.consulted, false)
    assert.deepEqual(
      f.readingPosts.map((post) => ({ ...post.input })),
      [
        {
          expected_username: 'alice',
          event_id: '9007199254740993',
          consulted: 'true',
        },
        {
          expected_username: 'alice',
          event_id: '9007199254740993',
          consulted: 'false',
        },
      ],
    )
    assert.deepEqual(f.messages, ['activity-visited', 'activity-visited'])
    assert.equal(page.response.value?.next_cursor, 'older-page')
  } finally {
    f.stop()
  }
})

test('mode changes commit after saving; explicit bulk uses the loaded boundary and updates older rows', async () => {
  const f = await fixture()
  try {
    f.admit()
    f.setActivity(async () =>
      feed('0', ['9007199254740992', '9007199254740993']),
    )
    await settle()
    const page = await f.mountActivity()
    await page.toggleConsulted('9007199254740993')
    assert.equal(f.readingPosts.length, 0)
    await page.saveReadingMode(true)
    assert.equal(page.response.value?.reading.manual_reading, true)
    await page.markAllConsulted()
    assert.deepEqual(
      { ...f.readingPosts[1]?.input },
      {
        expected_username: 'alice',
        last_event_id: '9007199254740993',
      },
    )
    assert.ok(page.response.value?.items.every((item) => item.consulted))
    await page.saveReadingMode(false)
    assert.equal(page.response.value?.reading.manual_reading, false)
  } finally {
    f.stop()
  }
})

test('reading writes serialize and navigation discards a late private result', async () => {
  const f = await fixture()
  try {
    f.admit()
    f.setActivity(async () => manualFeed(['10', '9']))
    await settle()
    const page = await f.mountActivity()
    const held = deferred<{
      username: string
      event_id: string
      consulted: boolean
    }>()
    f.setSaveRead(() => held.promise)
    const saving = page.toggleConsulted('10')
    assert.equal(page.readingPending.value, '10')
    assert.equal(page.response.value?.items[0]?.consulted, false)
    await page.toggleConsulted('9')
    assert.equal(f.readingPosts.length, 1)
    f.depart()
    held.resolve({ username: 'alice', event_id: '10', consulted: true })
    await saving
    assert.equal(page.response.value, null)
    assert.equal(page.readingPending.value, '')
    assert.deepEqual(f.messages, [])
  } finally {
    f.stop()
  }
})

test('focus reconciles every loaded badge and mode without replacing the walk or continuation cursor', async () => {
  const f = await fixture()
  try {
    f.admit()
    f.setActivity(async (cursor) =>
      cursor ? manualFeed(['9']) : manualFeed(['10']),
    )
    await settle()
    const page = await f.mountActivity()
    await page.loadMore()
    const before = page.response.value
    const queried: string[] = []
    f.setReading(async (ids) => {
      queried.push(...ids)
      return {
        reading: {
          username: 'alice',
          manual_reading: true,
          last_read_event_id: '0',
        },
        items: ids.map((event_id) => ({
          username: 'alice',
          event_id,
          consulted: event_id === '9',
        })),
      }
    })
    await f.account.revalidate()
    assert.equal(page.response.value, before)
    assert.deepEqual(queried, ['10', '9'])
    assert.equal(page.response.value?.items[0]?.consulted, false)
    assert.equal(page.response.value?.items[1]?.consulted, true)
    assert.equal(page.response.value?.next_cursor, 'older-page')
  } finally {
    f.stop()
  }
})

test('a lost mutation response reconciles committed state without replaying the write', async () => {
  const f = await fixture()
  try {
    f.admit()
    f.setActivity(async () => manualFeed(['10']))
    f.setReading(async (ids) => ({
      reading: {
        username: 'alice',
        manual_reading: true,
        last_read_event_id: '0',
      },
      items: ids.map((event_id) => ({
        username: 'alice',
        event_id,
        consulted: true,
      })),
    }))
    await settle()
    const page = await f.mountActivity()
    f.setSaveRead(async () => {
      throw new AccountApiError(503)
    })
    await page.toggleConsulted('10')
    await settle()
    assert.match(page.readingError.value, /Impossible de mémoriser/)
    assert.equal(page.response.value?.items[0]?.consulted, true)
    assert.equal(f.readingPosts.length, 1)
    assert.equal(page.readingPending.value, '')
  } finally {
    f.stop()
  }
})
