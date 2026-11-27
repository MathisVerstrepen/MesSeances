import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'

interface PushPayload {
  title?: string
  body?: string
  url?: string | number
  tag?: string
}
interface PushData {
  json: () => PushPayload
}
interface WorkerEvent {
  data?: PushData | null
  notification?: { close: () => void; data: { url: unknown } }
  waitUntil: (pending: Promise<unknown>) => void
}
type Listener = (event: WorkerEvent) => void
interface Shown {
  title: string
  options: { body: string; tag?: string; data: { url: string } }
}

async function worker(windows: { url: string; focused: string[] }[] = []) {
  const listeners: Record<string, Listener> = {}
  const shown: Shown[] = []
  const opened: string[] = []
  runInNewContext(
    await readFile(new URL('../public/sw-push.js', import.meta.url), 'utf8'),
    {
      URL,
      String,
      self: {
        location: { origin: 'https://messeances.fr' },
        addEventListener: (name: string, listener: Listener) => {
          listeners[name] = listener
        },
        registration: {
          showNotification: async (
            title: string,
            options: Shown['options'],
          ) => {
            shown.push({ title, options })
          },
        },
        clients: {
          matchAll: async () =>
            windows.map((client) => ({
              url: client.url,
              focus: async () => ({
                navigate: async (target: string) => client.focused.push(target),
              }),
            })),
          openWindow: async (target: string) => opened.push(target),
        },
      },
    },
  )
  const dispatch = async (
    name: string,
    event: Omit<WorkerEvent, 'waitUntil'>,
  ) => {
    let pending: Promise<unknown> = Promise.resolve()
    listeners[name]!({
      ...event,
      waitUntil: (p: Promise<unknown>) => (pending = p),
    })
    await pending
  }
  return { dispatch, shown, opened }
}

test('push links stay on the application origin', async () => {
  const w = await worker()
  for (const url of [
    '/film/film-2',
    '/\\evil.example/x',
    '//evil.example',
    'https://evil.example',
    '/compte/activite?x=1',
    42,
  ])
    await w.dispatch('push', {
      data: { json: () => ({ title: 'T', body: 'B', url, tag: 'rule-1' }) },
    })
  assert.deepEqual(
    w.shown.map((item) => item.options.data.url),
    [
      '/film/film-2',
      '/compte/notifications',
      '/compte/notifications',
      '/compte/notifications',
      '/compte/activite?x=1',
      '/compte/notifications',
    ],
  )
  assert.equal(w.shown[0]!.options.tag, 'rule-1')
})

test('invalid payloads show a generic notification', async () => {
  const w = await worker()
  await w.dispatch('push', {
    data: {
      json: () => {
        throw new Error('not json')
      },
    },
  })
  await w.dispatch('push', { data: null })
  assert.deepEqual(
    w.shown.map((item) => [item.title, item.options.tag]),
    [
      ['MesSeances', undefined],
      ['MesSeances', undefined],
    ],
  )
})

test('clicks reuse an open window or open a new one', async () => {
  const focused: string[] = []
  const existing = { url: 'https://messeances.fr/films', focused }
  const w = await worker([
    { url: 'https://other.example/', focused: [] },
    existing,
  ])
  const notification = { close() {}, data: { url: '/compte/activite' } }
  await w.dispatch('notificationclick', { notification })
  assert.deepEqual(existing.focused, ['https://messeances.fr/compte/activite'])
  assert.deepEqual(w.opened, [])
  const fresh = await worker()
  await fresh.dispatch('notificationclick', {
    notification: { close() {}, data: { url: '//evil.example' } },
  })
  assert.deepEqual(fresh.opened, ['https://messeances.fr/compte/notifications'])
})
