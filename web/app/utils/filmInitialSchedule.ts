import type {
  MovieShowtimesBundleResponse,
  MovieShowtimesResponse,
} from '../types/api.ts'

export interface InitialFilmSchedule {
  scoped: MovieShowtimesResponse
  nationwide: MovieShowtimesResponse
  selectedDate: string
}

export type InitialScheduleFailureCause =
  | Error
  | string
  | number
  | boolean
  | bigint
  | symbol
  | null
  | undefined

export class NationwideInitialScheduleError extends Error {
  readonly selectedDate: string
  readonly upstreamCause: InitialScheduleFailureCause

  constructor(
    selectedDate: string,
    upstreamCause: InitialScheduleFailureCause,
  ) {
    super('Nationwide schedule unavailable')
    this.selectedDate = selectedDate
    this.upstreamCause = upstreamCause
  }
}

interface InitialFilmScheduleOptions {
  requestedDate: string
  today: string
  fetchScoped?: (date: string) => Promise<MovieShowtimesResponse>
  fetchNationwide: (date: string) => Promise<MovieShowtimesResponse>
  fetchBundle?: (date: string) => Promise<MovieShowtimesBundleResponse>
}

function calendarDate(value: string): boolean {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!match) return false
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const date = new Date(Date.UTC(year, month - 1, day, 12))
  return (
    date.getUTCFullYear() === year &&
    date.getUTCMonth() === month - 1 &&
    date.getUTCDate() === day
  )
}

function availableDates(
  response: MovieShowtimesResponse,
  today: string,
): string[] {
  return [...new Set(response.available_dates)]
    .filter((date) => calendarDate(date) && date >= today)
    .sort()
}

function selectedDate(
  dates: string[],
  requestedDate: string,
  today: string,
): string {
  if (dates.includes(requestedDate)) return requestedDate
  return dates.includes(today) ? today : (dates[0] ?? today)
}

export async function loadInitialFilmSchedule(
  options: InitialFilmScheduleOptions,
): Promise<InitialFilmSchedule> {
  const { requestedDate, today, fetchBundle, fetchNationwide, fetchScoped } =
    options

  if (fetchBundle) {
    let bundle = await fetchBundle(requestedDate)
    const dates = availableDates(bundle.scoped, today)
    const resolvedDate = selectedDate(dates, requestedDate, today)
    if (!dates.includes(requestedDate) && dates.length > 0)
      bundle = await fetchBundle(resolvedDate)
    return {
      scoped: bundle.scoped,
      nationwide: bundle.nationwide,
      selectedDate: resolvedDate,
    }
  }

  // Client navigation can wait for the exact selection instead of fetching a
  // disposable Paris schedule. Only public movie evidence enters this result.
  if (!fetchScoped) {
    let date = requestedDate
    const fetch = () =>
      fetchNationwide(date).catch((error) => {
        throw new NationwideInitialScheduleError(date, error)
      })
    let nationwide = await fetch()
    const dates = availableDates(nationwide, today)
    date = selectedDate(dates, date, today)
    if (date !== requestedDate && dates.length) nationwide = await fetch()
    return {
      scoped: { ...nationwide, theaters: [] },
      nationwide,
      selectedDate: date,
    }
  }

  const initialNationwide = fetchNationwide(requestedDate).then(
    (schedule) => ({ schedule, error: undefined }),
    (error) => ({ schedule: undefined, error }),
  )
  let scoped = await fetchScoped(requestedDate)
  const dates = availableDates(scoped, today)
  const resolvedDate = selectedDate(dates, requestedDate, today)
  if (!dates.includes(requestedDate) && dates.length > 0)
    scoped = await fetchScoped(resolvedDate)

  const nationwideResult =
    resolvedDate === requestedDate
      ? await initialNationwide
      : await fetchNationwide(resolvedDate).then(
          (schedule) => ({ schedule, error: undefined }),
          (error) => ({ schedule: undefined, error }),
        )
  if (nationwideResult.error !== undefined)
    throw new NationwideInitialScheduleError(
      resolvedDate,
      nationwideResult.error,
    )
  if (!nationwideResult.schedule)
    throw new Error('Nationwide schedule unavailable')

  return {
    scoped,
    nationwide: nationwideResult.schedule,
    selectedDate: resolvedDate,
  }
}
