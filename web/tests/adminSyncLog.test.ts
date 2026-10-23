import assert from 'node:assert/strict'
import test from 'node:test'
import { hasAdminSyncLog, joinAdminSyncLog } from '../app/utils/adminSyncLog.ts'
import { renderPageTemplate } from './helpers/renderVue.ts'

test('joins log lines in backend order with one newline', () => {
  const lines = [
    'ts=2026-08-26T07:57:05Z level=info provider=ugc event=provider_started',
    'ts=2026-08-26T07:57:06Z level=error provider=ugc event=provider_failed',
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

test('actual sync page escapes hostile operational log HTML', async () => {
  const html = await renderPageTemplate('pages/admin/sync.vue', {
    errorMessage: '',
    initialPending: false,
    targets: [],
    activeJob: null,
    history: [
      {
        id: 'run',
        state: 'failed',
        target: 'all',
        providers: {
          ugc: {
            state: 'failed',
            log: [
              '<script>alert("journal")</script>',
              '<img src=x onerror=alert(1)>',
              'event=provider_failed category=transport detail="GET https://www.ocine.fr/maubeuge/seances/; attempt 4/4; proxyconnect tcp: connection refused"',
            ],
          },
        },
      },
    ],
    requestedProviders: () => ['ugc'],
    providerIcon: () => 'span',
    providerIconClass: () => '',
    providerLabels: {},
    providerStateLabels: {},
    stateLabels: {},
    targetLabels: {},
    triggerLabels: {},
    formatDateTime: () => '',
    formatDuration: () => '',
    formatNewShowtimes: () => '',
    X: 'span',
    hasAdminSyncLog,
    joinAdminSyncLog,
  })
  assert.match(
    html,
    /<code>&lt;script&gt;alert\(&quot;journal&quot;\)&lt;\/script&gt;\n&lt;img src=x onerror=alert\(1\)&gt;\nevent=provider_failed/,
  )
  assert.match(
    html,
    /detail=&quot;GET https:\/\/www\.ocine\.fr\/maubeuge\/seances\/; attempt 4\/4; proxyconnect tcp: connection refused&quot;<\/code>/,
  )
  assert.doesNotMatch(html, /<script\b|<img\b/)
})
