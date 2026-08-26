import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { hasAdminSyncLog, joinAdminSyncLog } from '../app/utils/adminSyncLog.ts'

test('joins log lines in backend order with one newline', () => {
  const lines = [
    'ts=2026-08-26T07:57:05Z level=info provider=ugc event=provider_started',
    'ts=2026-08-26T07:57:06Z level=error provider=ugc event=provider_failed'
  ]

  assert.equal(hasAdminSyncLog(lines), true)
  assert.equal(joinAdminSyncLog(lines), `${lines[0]}\n${lines[1]}`)
})

test('reports an absent or empty log without fabricating text', () => {
  assert.equal(hasAdminSyncLog(undefined), false)
  assert.equal(joinAdminSyncLog(undefined), '')
  assert.equal(hasAdminSyncLog([]), false)
  assert.equal(joinAdminSyncLog([]), '')
})

test('keeps log text literal', () => {
  const line = '<script>alert("journal")</script>'

  assert.equal(joinAdminSyncLog([line]), line)
})

test('renders the joined log through Vue interpolation without an HTML path', async () => {
  const page = await readFile(new URL('../app/pages/admin/sync.vue', import.meta.url), 'utf8')

  assert.match(page, /<code>\{\{ joinAdminSyncLog\(run\.providers\[provider\]\.log\) \}\}<\/code>/)
  assert.doesNotMatch(page, /\bv-html\b/)
})
