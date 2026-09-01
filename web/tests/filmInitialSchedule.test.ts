import assert from 'node:assert/strict'
import test from 'node:test'
import type { MovieShowtimesResponse } from '../app/types/api.ts'
import { loadInitialFilmSchedule, NationwideInitialScheduleError } from '../app/utils/filmInitialSchedule.ts'

const REQUESTED_DATE = '2026-09-01'
const RESOLVED_DATE = '2026-09-02'

function schedule(date: string, availableDates: string[], marker: string): MovieShowtimesResponse {
  return {
    movie: {
      slug: 'film-1',
      title: marker,
      runtime_minutes: 100,
      updated_at: `${date}T00:00:00Z`,
      poster_url: null,
      tmdb_id: null,
      imdb_id: null,
      overview: null,
      release_date: date,
      genres: []
    },
    backdrop_url: null,
    date,
    currently_screened: true,
    available_dates: availableDates,
    theaters: []
  }
}

test('configured SSR uses one bundle request for a date already available', async () => {
  const calls: string[] = []
  const scoped = schedule(REQUESTED_DATE, [REQUESTED_DATE], 'Paris')
  const nationwide = schedule(REQUESTED_DATE, [REQUESTED_DATE], 'France')
  const result = await loadInitialFilmSchedule({
    requestedDate: REQUESTED_DATE,
    today: REQUESTED_DATE,
    fetchScoped: async () => { throw new Error('public scoped request used') },
    fetchNationwide: async () => { throw new Error('public nationwide request used') },
    fetchBundle: async (date) => {
      calls.push(date)
      return { scoped, nationwide }
    }
  })

  assert.deepEqual(calls, [REQUESTED_DATE])
  assert.deepEqual(result, { scoped, nationwide, selectedDate: REQUESTED_DATE })
})

test('configured SSR makes exactly one bundle request per distinct fallback date', async () => {
  const calls: string[] = []
  const initialScoped = schedule(REQUESTED_DATE, [RESOLVED_DATE], 'Initial Paris')
  const finalScoped = schedule(RESOLVED_DATE, [RESOLVED_DATE], 'Final Paris')
  const finalNationwide = schedule(RESOLVED_DATE, [RESOLVED_DATE], 'Final France')
  const result = await loadInitialFilmSchedule({
    requestedDate: REQUESTED_DATE,
    today: REQUESTED_DATE,
    fetchScoped: async () => { throw new Error('public scoped request used') },
    fetchNationwide: async () => { throw new Error('public nationwide request used') },
    fetchBundle: async (date) => {
      calls.push(date)
      return date === REQUESTED_DATE
        ? { scoped: initialScoped, nationwide: schedule(REQUESTED_DATE, [RESOLVED_DATE], 'Discarded France') }
        : { scoped: finalScoped, nationwide: finalNationwide }
    }
  })

  assert.deepEqual(calls, [REQUESTED_DATE, RESOLVED_DATE])
  assert.deepEqual(result, { scoped: finalScoped, nationwide: finalNationwide, selectedDate: RESOLVED_DATE })
})

test('blank-secret fallback preserves public Paris and nationwide request behavior', async () => {
  const calls: string[] = []
  const initialScoped = schedule(REQUESTED_DATE, [RESOLVED_DATE], 'Initial Paris')
  const finalScoped = schedule(RESOLVED_DATE, [RESOLVED_DATE], 'Final Paris')
  const finalNationwide = schedule(RESOLVED_DATE, [RESOLVED_DATE], 'Final France')
  const result = await loadInitialFilmSchedule({
    requestedDate: REQUESTED_DATE,
    today: REQUESTED_DATE,
    fetchScoped: async (date) => {
      calls.push(`scoped:${date}`)
      return date === REQUESTED_DATE ? initialScoped : finalScoped
    },
    fetchNationwide: async (date) => {
      calls.push(`nationwide:${date}`)
      return date === REQUESTED_DATE ? schedule(date, [RESOLVED_DATE], 'Discarded France') : finalNationwide
    }
  })

  assert.deepEqual(calls, [
    `nationwide:${REQUESTED_DATE}`,
    `scoped:${REQUESTED_DATE}`,
    `scoped:${RESOLVED_DATE}`,
    `nationwide:${RESOLVED_DATE}`
  ])
  assert.deepEqual(result, { scoped: finalScoped, nationwide: finalNationwide, selectedDate: RESOLVED_DATE })
})

test('blank-secret typical SSR performs one Paris and one nationwide request', async () => {
  const calls: string[] = []
  const scoped = schedule(REQUESTED_DATE, [REQUESTED_DATE], 'Paris')
  const nationwide = schedule(REQUESTED_DATE, [REQUESTED_DATE], 'France')
  await loadInitialFilmSchedule({
    requestedDate: REQUESTED_DATE,
    today: REQUESTED_DATE,
    fetchScoped: async (date) => { calls.push(`scoped:${date}`); return scoped },
    fetchNationwide: async (date) => { calls.push(`nationwide:${date}`); return nationwide }
  })
  assert.deepEqual(calls, [`nationwide:${REQUESTED_DATE}`, `scoped:${REQUESTED_DATE}`])
})

test('blank-secret nationwide failure preserves resolved-date upstream error behavior', async () => {
  const upstreamCause = Object.assign(new Error('upstream unavailable'), { status: 502 })
  await assert.rejects(
    loadInitialFilmSchedule({
      requestedDate: REQUESTED_DATE,
      today: REQUESTED_DATE,
      fetchScoped: async (date) => schedule(date, [RESOLVED_DATE], 'Paris'),
      fetchNationwide: async (date) => {
        if (date === REQUESTED_DATE) return schedule(date, [RESOLVED_DATE], 'Discarded France')
        throw upstreamCause
      }
    }),
    (error) => error instanceof NationwideInitialScheduleError
      && error.selectedDate === RESOLVED_DATE
      && error.upstreamCause === upstreamCause
  )
})
