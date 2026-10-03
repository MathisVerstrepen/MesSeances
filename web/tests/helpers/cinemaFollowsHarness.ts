import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { computed, effectScope, nextTick, readonly, ref, watch } from 'vue'
import type { useCinemaFollows } from '../../app/composables/useCinemaFollows.ts'
import type { useAccountSession } from '../../app/composables/useAccountSession.ts'
import type { useAccountActivity } from '../../app/composables/useAccountActivity.ts'
import type { AccountSession } from '../../app/types/account.ts'
import type {
  AccountActivityResponse,
  AccountTheaterFollows,
  SaveAccountTheaterFollow,
} from '../../app/types/cinemaFollows.ts'
import * as errors from '../../app/utils/accountState.ts'
import * as activity from '../../app/utils/cinemaActivity.ts'

export function session(username = 'alice'): AccountSession {
  return {
    enabled: true,
    state: 'complete',
    account: {
      username,
      email: `${username}@example.test`,
      has_password: true,
      google_linked: false,
    },
  }
}
export function snapshot(
  revision = '0',
  theater_ids: string[] = [],
  username = 'alice',
): AccountTheaterFollows {
  return { username, revision, theater_ids }
}
export function feed(
  revision = '0',
  eventIds: string[] = [],
  cursor: string | null = null,
  username = 'alice',
): AccountActivityResponse {
  return {
    username,
    follows_revision: revision,
    followed_theater_count: 2,
    generated_at: '2026-10-03T08:00:00Z',
    timezone: 'Europe/Paris',
    coverage: {
      initialized_theater_count: 2,
      completeness: 'partial',
      bootstrap: 'baseline',
      return_minimum_break_days: 28,
    },
    limit: 20,
    next_cursor: cursor,
    items: eventIds.map((event_id, index) => ({
      event_id,
      type: 'added_to_program',
      detected_at: `2026-10-03T08:00:00.00000${index}Z`,
      first_screening_date: '2026-10-03',
      previous_program_end_date: null,
      movie: {
        slug: 'same-film',
        title: 'Même film',
        poster_url: null,
        updated_at: '2026-10-03T08:00:00Z',
      },
      has_upcoming_showtimes: true,
      next_showtime_date: '2026-10-03',
      theater: {
        id: index % 2 ? 'cinema-b' : 'cinema-a',
        slug: index % 2 ? 'cinema-b' : 'cinema-a',
        name: index % 2 ? 'Cinéma B' : 'Cinéma A',
        city: 'Lille',
        provider: 'ugc',
      },
    })),
  }
}
export function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
export async function settle() {
  await nextTick()
  await new Promise<void>((resolve) => setImmediate(resolve))
}

export async function fixture(client = true) {
  const states = new Map()
  const messages: string[] = []
  const app = {
    _accountChannel: { postMessage: (value: string) => messages.push(value) },
  }
  const posts: SaveAccountTheaterFollow[] = []
  const cursors: (string | undefined)[] = []
  const scopes: ReturnType<typeof effectScope>[] = []
  const starts = new Set<() => void>()
  const arrivals = new Set<() => void>()
  const mounted: (() => void)[] = []
  const unmounted: (() => void)[] = []
  let admitted = session()
  let value = snapshot()
  let gets = 0
  let read = async () => value
  let sessionRead = async () => admitted
  let activityRead = async (_cursor?: string) => feed()
  let write = async (input: SaveAccountTheaterFollow) => {
    value = snapshot(
      String(BigInt(input.expected_revision) + 1n),
      input.followed === 'true' ? [input.theater_id] : [],
      input.expected_username,
    )
    return value
  }
  const context = {
    ref,
    computed,
    readonly,
    watch,
    AbortController,
    effectScope: () => {
      const scope = effectScope(true)
      scopes.push(scope)
      return scope
    },
    useState: <T>(key: string, init: () => T) => {
      if (!states.has(key)) states.set(key, ref(init()))
      return states.get(key)
    },
    useNuxtApp: () => app,
    onMounted: (fn: () => void) => mounted.push(fn),
    onBeforeUnmount: (fn: () => void) => unmounted.push(fn),
    useRoute: () => ({ path: '/compte/activite' }),
    useRouter: () => ({ currentRoute: ref({ path: '/compte/activite' }) }),
    useAccountNavigation: () => ({ starts, arrivals }),
    useAccountApi: () => ({
      session: () => sessionRead(),
      theaterFollows: () => {
        gets++
        return read()
      },
      saveTheaterFollow: (input: SaveAccountTheaterFollow) => {
        posts.push(input)
        return write(input)
      },
      accountActivity: (cursor?: string) => {
        cursors.push(cursor)
        return activityRead(cursor)
      },
    }),
    require: (id: string) =>
      id === '~/utils/cinemaActivity' ? activity : errors,
  }
  async function compile<T>(name: string, extra = {}): Promise<T> {
    const source = await readFile(
      new URL(`../../app/composables/${name}.ts`, import.meta.url),
      'utf8',
    )
    const exports = {}
    runInNewContext(
      ts.transpileModule(
        source.replaceAll('import.meta.client', String(client)),
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      { ...context, ...extra, exports },
    )
    // SAFETY: Caller supplies the export type of this exact source module.
    return exports as T
  }
  const account = (
    await compile<{ useAccountSession: typeof useAccountSession }>(
      'useAccountSession',
    )
  ).useAccountSession()
  const module = await compile<{ useCinemaFollows: typeof useCinemaFollows }>(
    'useCinemaFollows',
    { useAccountSession: () => account },
  )
  const follows = module.useCinemaFollows()
  follows.startSynchronization()
  let page: ReturnType<typeof useAccountActivity> | undefined
  return {
    account,
    follows,
    posts,
    messages,
    states,
    cursors,
    starts,
    arrivals,
    get gets() {
      return gets
    },
    another: () => module.useCinemaFollows(),
    setRead: (fn: typeof read) => {
      read = fn
    },
    setWrite: (fn: typeof write) => {
      write = fn
    },
    setActivity: (fn: typeof activityRead) => {
      activityRead = fn
    },
    setSessionRead: (fn: typeof sessionRead) => {
      sessionRead = fn
    },
    setSnapshot: (next: AccountTheaterFollows) => {
      value = next
    },
    admit: (next = session()) => {
      admitted = next
      account.accept(next)
    },
    async mountActivity() {
      const lifetime = await compile<{
        useAccountLifetime: typeof import('../../app/composables/useAccountLifetime.ts')['useAccountLifetime']
      }>('useAccountLifetime', { useAccountSession: () => account })
      const scope = effectScope()
      scopes.push(scope)
      const module = await compile<{
        useAccountActivity: typeof useAccountActivity
      }>('useAccountActivity', {
        useAccountSession: () => account,
        useCinemaFollows: () => follows,
        useAccountLifetime: lifetime.useAccountLifetime,
      })
      page = scope.run(() => module.useAccountActivity())!
      mounted.forEach((fn) => fn())
      await settle()
      return page
    },
    depart: () => starts.forEach((fn) => fn()),
    stop: () => {
      unmounted.forEach((fn) => fn())
      scopes.forEach((scope) => scope.stop())
    },
  }
}
