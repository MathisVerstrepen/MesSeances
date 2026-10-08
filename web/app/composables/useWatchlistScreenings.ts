import type { MoviesResponse, ScreeningWindow } from '~/types/api'

const pageSize = 100
const frenchNumber = new Intl.NumberFormat('fr-FR', {
  maximumFractionDigits: 1,
})
const frenchDate = new Intl.DateTimeFormat('fr-FR', {
  timeZone: 'UTC',
  day: 'numeric',
  month: 'long',
  year: 'numeric',
})

export function screeningAverage(count: number, window: ScreeningWindow) {
  return {
    text: `${frenchNumber.format(count / window.day_count)} séances/j`,
    label: `Moyenne quotidienne des séances restantes jusqu’au mardi ${frenchDate.format(new Date(`${window.through}T00:00:00Z`))} inclus`,
  }
}

function calendarDate(value: string) {
  // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Public response boundary: reject coerced dates before calendar validation.
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value))
    throw new Error('Invalid screening date')
  const date = new Date(`${value}T00:00:00Z`)
  if (
    !Number.isFinite(date.getTime()) ||
    date.toISOString().slice(0, 10) !== value
  )
    throw new Error('Invalid screening date')
  return date
}

function timestamp(value: string) {
  return (
    // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Public response boundary: timestamps must be strings, not coercible JSON values.
    typeof value === 'string' &&
    /^\d{4}-\d{2}-\d{2}T.+(?:Z|[+-]\d{2}:\d{2})$/.test(value) &&
    Number.isFinite(Date.parse(value))
  )
}

function checkPage(
  value: MoviesResponse,
  page: number,
  first?: MoviesResponse,
) {
  const window = value.screening_window
  if (
    !window ||
    window.timezone !== 'Europe/Paris' ||
    !timestamp(window.as_of) ||
    !timestamp(value.generated_at) ||
    // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Public response boundary: only genuine revision strings may authorize a complete walk.
    typeof value.catalog_revision !== 'string' ||
    !value.catalog_revision ||
    value.page !== page ||
    value.page_size !== pageSize ||
    !Number.isSafeInteger(value.total) ||
    value.total < 0 ||
    !Number.isInteger(window.day_count) ||
    window.day_count < 1 ||
    window.day_count > 7 ||
    !Array.isArray(value.items)
  )
    throw new Error('Incomplete screening catalog')
  const from = calendarDate(window.from)
  const through = calendarDate(window.through)
  if (
    through.getUTCDay() !== 2 ||
    (through.getTime() - from.getTime()) / 86400000 + 1 !== window.day_count ||
    value.items.length !==
      Math.min(pageSize, Math.max(0, value.total - (page - 1) * pageSize)) ||
    (page > 1 && (page - 1) * pageSize >= value.total)
  )
    throw new Error('Incomplete screening catalog')
  if (
    first &&
    (value.catalog_revision !== first.catalog_revision ||
      value.generated_at !== first.generated_at ||
      value.total !== first.total ||
      window.from !== first.screening_window?.from ||
      window.through !== first.screening_window?.through ||
      window.day_count !== first.screening_window?.day_count ||
      window.timezone !== first.screening_window?.timezone)
  )
    throw new Error('Screening catalog changed')
  return window
}

export function useWatchlistScreenings() {
  const account = useAccountSession()
  const watchlist = useWatchlist()
  const preferences = useCinemaPreferences()
  const api = useMesSeancesApi()
  const result = ref<{
    window: ScreeningWindow
    counts: Map<string, { next7Days: number; remaining: number }>
  } | null>(null)
  const loading = ref(false)
  const failure = ref(false)
  const scopeVersion = ref(0)
  let mounted = false
  const active = ref(false)
  let generation = 0
  let controller: AbortController | undefined
  const membership = computed(() =>
    JSON.stringify([...watchlist.slugs.value].sort()),
  )
  const selection = computed(() =>
    JSON.stringify([...preferences.favoriteTheaterIds.value].sort()),
  )
  const error = computed(
    () =>
      !!watchlist.owner.value &&
      !!watchlist.items.value.length &&
      (failure.value || !!preferences.error.value),
  )

  function clear() {
    generation++
    controller?.abort()
    result.value = null
    loading.value = false
    failure.value = false
  }
  const lifetime = useAccountLifetime(() => {
    active.value = false
    clear()
  })
  function admitted() {
    return (
      import.meta.client &&
      mounted &&
      active.value &&
      !!watchlist.owner.value &&
      watchlist.ready.value &&
      preferences.isInitialized.value &&
      !preferences.error.value &&
      !account.revalidating.value &&
      !!watchlist.items.value.length
    )
  }

  async function refresh() {
    clear()
    if (!admitted()) return
    const current = generation
    const alive = lifetime.capture()
    const valid = () => current === generation && alive() && admitted()
    const theaters = preferences.favoriteTheaterIds.value.join(',') || undefined
    controller = new AbortController()
    const signal = controller.signal
    loading.value = true
    try {
      const counts = new Map<string, { next7Days: number; remaining: number }>()
      let first: MoviesResponse | undefined
      let window: ScreeningWindow | undefined
      for (let page = 1; ; page++) {
        const value = await api.movies(
          {
            currently_screened: true,
            screening_summary: true,
            sort: 'title_asc',
            page_size: pageSize,
            page,
            theaters,
          },
          signal,
        )
        if (!valid()) return
        window = checkPage(value, page, first)
        first ??= value
        for (const item of value.items) {
          const currentCount = item.showtime_count
          const remaining = item.remaining_showtime_count
          const next7Days = item.next_7_days_showtime_count
          if (
            // oxlint-disable-next-line anti-slop/no-runtime-typeof -- Public response boundary: only genuine canonical slug strings can establish catalog absence.
            typeof item.slug !== 'string' ||
            !item.slug ||
            counts.has(item.slug) ||
            currentCount === undefined ||
            !Number.isSafeInteger(currentCount) ||
            currentCount < 0 ||
            remaining === undefined ||
            !Number.isSafeInteger(remaining) ||
            remaining < 0 ||
            next7Days === undefined ||
            !Number.isSafeInteger(next7Days) ||
            next7Days < 0
          )
            throw new Error('Incomplete screening counts')
          counts.set(item.slug, {
            next7Days,
            remaining,
          })
        }
        if (counts.size === value.total) break
      }
      if (valid() && window) result.value = { window, counts }
    } catch {
      if (valid()) failure.value = true
    } finally {
      if (valid()) loading.value = false
    }
  }

  async function retry() {
    if (!active.value || !mounted) return
    if (!preferences.isInitialized.value || preferences.error.value)
      await preferences.retrySynchronization()
    if (!loading.value) await refresh()
  }

  // Invalidate synchronously, including A -> B -> A before any watcher flush.
  watch(
    [
      watchlist.owner,
      watchlist.scopeKey,
      watchlist.ready,
      preferences.selectionScopeKey,
      preferences.isInitialized,
      selection,
      membership,
      account.revision,
      preferences.error,
    ],
    () => {
      clear()
      scopeVersion.value++
    },
    { flush: 'sync' },
  )
  watch([scopeVersion, account.revalidating], () => {
    if (!loading.value) void refresh()
  })

  onMounted(() => {
    mounted = true
    active.value = true
    window.addEventListener('offline', clear)
    window.addEventListener('pagehide', clear)
    void preferences.initialize().then(() => {
      if (active.value && !loading.value && !result.value && !failure.value)
        void refresh()
    })
    void refresh()
  })
  if (import.meta.client) {
    const runtime = useAccountNavigation()
    const path = useRoute().path
    const router = useRouter()
    const resume = () => {
      if (mounted && router.currentRoute.value.path === path && !active.value) {
        active.value = true
        void refresh()
      }
    }
    runtime.arrivals.add(resume)
    onBeforeUnmount(() => runtime.arrivals.delete(resume))
  }
  onBeforeUnmount(() => {
    mounted = false
    active.value = false
    clear()
    window.removeEventListener('offline', clear)
    window.removeEventListener('pagehide', clear)
  })

  return {
    loading: computed(
      () =>
        loading.value ||
        (active.value &&
          !!watchlist.owner.value &&
          watchlist.ready.value &&
          !!watchlist.items.value.length &&
          !preferences.isInitialized.value &&
          !preferences.error.value),
    ),
    error,
    retry,
    forMovie(slug: string) {
      if (!result.value) return null
      const counts = result.value.counts.get(slug)
      return {
        inTheaters: (counts?.next7Days ?? 0) > 0,
        average: screeningAverage(counts?.remaining ?? 0, result.value.window),
      }
    },
  }
}
