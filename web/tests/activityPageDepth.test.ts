import assert from 'node:assert/strict'
import test from 'node:test'
import { activityPageDepth } from '../app/utils/activityPageDepth.ts'

function historyFixture(saved: History['state'] = undefined) {
  const history = {
    state: {
      position: 7,
      current: '/compte/activite',
      activityPages: saved,
      scroll: { top: 123, left: 0 },
    },
    replaceState(state: History['state']) {
      this.state = state
    },
  }
  return history
}

test('depth is a positive page count, with SSR and malformed history defaulting to one', () => {
  assert.equal(activityPageDepth().target, 1)
  for (const saved of [
    undefined,
    null,
    0,
    -1,
    1.2,
    '3',
    Infinity,
    Number.MAX_SAFE_INTEGER + 1,
  ]) {
    const history = historyFixture(saved)
    assert.equal(activityPageDepth(history).target, 1)
  }
  assert.equal(activityPageDepth(historyFixture(3)).target, 3)
})

test('records only the count, preserving router state and refusing writes after navigation', () => {
  const history = historyFixture()
  const depth = activityPageDepth(history)
  depth.record(4)
  assert.deepEqual(history.state, {
    position: 7,
    current: '/compte/activite',
    activityPages: 4,
    scroll: { top: 123, left: 0 },
  })
  history.state.position++
  depth.record(5)
  assert.equal(history.state.activityPages, 4)
  history.state.position--
  history.state.current = '/film/example'
  depth.record(6)
  assert.equal(history.state.activityPages, 4)
})
