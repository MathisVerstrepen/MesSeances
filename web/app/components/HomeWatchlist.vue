<script setup lang="ts">
import type { TimelineTheater } from '~/types/api'
import type { WatchlistItem } from '~/types/watchlist'
import { accountDestination } from '~/utils/accountState'
import { activityObservationDay } from '~/utils/cinemaActivity'
import { cinemaMovieTarget } from '~/utils/cinemaMovieTarget'
import {
  addCalendarDays,
  formatLongDate,
  formatParisTime,
  todayInParis,
} from '~/utils/date'
import { theaterDisplayName } from '~/utils/theaterDisplayName'

type MoviePreview = {
  movie: WatchlistItem
  theater: TimelineTheater
  startsAt: string
  startDate: string
  serviceDate: string
}

const previewLimit = 4
const theatersPerRequest = 100
const account = useAccountSession()
const watchlist = useWatchlist()
const preferences = useCinemaPreferences()
const api = useMesSeancesApi()
const mounted = ref(false)
const items = shallowRef<MoviePreview[]>([])
const pending = ref(false)
const failed = ref(false)
const retryCount = ref(0)
const membership = computed(() => [...watchlist.slugs.value].sort().join(','))
const selection = computed(() => preferences.favoriteTheaterIds.value.join(','))
const { error: preferenceError, isInitialized } = preferences
const { ready, owner } = watchlist
const watchlistError = computed(
  () => account.status.value === 'error' || !!watchlist.error.value,
)
const loading = computed(
  () =>
    !mounted.value ||
    account.status.value === 'idle' ||
    account.status.value === 'loading' ||
    (!!owner.value &&
      !watchlistError.value &&
      (!ready.value ||
        (!!membership.value &&
          !isInitialized.value &&
          !preferenceError.value) ||
        pending.value)),
)
const accountAction = computed(() => {
  const session = account.session.value
  if (owner.value) return { to: '/compte/watchlist', label: 'Ma watchlist' }
  if (!session?.enabled) return undefined
  return {
    to: accountDestination(session),
    label:
      session.state === 'anonymous' ? 'Se connecter' : 'Accéder à mon compte',
  }
})

onMounted(() => {
  mounted.value = true
})

watch(
  [
    mounted,
    ready,
    owner,
    watchlist.scopeKey,
    preferences.selectionScopeKey,
    membership,
    selection,
    preferenceError,
    retryCount,
  ],
  async (
    [isMounted, isReady, username, , , slugs, theaters, cinemaError],
    _,
    onCleanup,
  ) => {
    items.value = []
    failed.value = false
    pending.value = false
    if (
      !isMounted ||
      !isReady ||
      !username ||
      !slugs ||
      !theaters ||
      cinemaError
    )
      return

    const request = new AbortController()
    const { signal } = request
    onCleanup(() => request.abort())
    pending.value = true
    const saved = new Map(
      watchlist.items.value.map((movie) => [movie.slug, movie]),
    )
    const firstDate = addCalendarDays(todayInParis(), -1)
    // Sessions after midnight can belong to the previous cinema day.
    const theatersByDate = new Map<string, string[]>()
    for (const theater of preferences.favoriteTheaters.value) {
      for (const date of theater.available_dates) {
        if (date < firstDate) continue
        const ids = theatersByDate.get(date)
        if (ids) ids.push(theater.id)
        else theatersByDate.set(date, [theater.id])
      }
    }
    const nextSessions = new Map<string, MoviePreview>()
    try {
      const dates = [...theatersByDate].sort(([left], [right]) =>
        left.localeCompare(right),
      )
      for (const [date, ids] of dates) {
        for (let start = 0; start < ids.length; start += theatersPerRequest) {
          const timeline = await api.timeline(
            {
              date,
              theaters: ids.slice(start, start + theatersPerRequest).join(','),
            },
            signal,
          )
          if (signal.aborted) return
          const now = Date.now()
          for (const theater of timeline.theaters) {
            for (const showtime of theater.showtimes) {
              const movie = saved.get(showtime.movie.slug)
              const startsAt = Date.parse(showtime.start_time)
              if (!movie || !(startsAt > now)) continue
              const existing = nextSessions.get(movie.slug)
              if (!existing || startsAt < Date.parse(existing.startsAt)) {
                nextSessions.set(movie.slug, {
                  movie,
                  theater,
                  startsAt: showtime.start_time,
                  startDate: activityObservationDay(showtime.start_time),
                  serviceDate: date,
                })
              }
            }
          }
        }
        // Check the following cinema day if an overnight session falls there:
        // some cinemas also schedule early sessions on that calendar date.
        const now = Date.now()
        const settled = [...nextSessions.values()].filter(
          (item) => item.startDate <= date && Date.parse(item.startsAt) > now,
        )
        if (settled.length >= Math.min(previewLimit, saved.size)) break
      }
      const now = Date.now()
      items.value = [...nextSessions.values()]
        .filter((item) => Date.parse(item.startsAt) > now)
        .sort(
          (left, right) =>
            Date.parse(left.startsAt) - Date.parse(right.startsAt),
        )
        .slice(0, previewLimit)
    } catch {
      if (!signal.aborted) failed.value = true
    } finally {
      if (!signal.aborted) pending.value = false
    }
  },
)

function retry() {
  if (watchlistError.value) void watchlist.retry()
  else if (preferenceError.value) void preferences.retrySynchronization()
  else retryCount.value++
}

function showtimeTarget(item: MoviePreview) {
  if (item.serviceDate < todayInParis()) {
    return `/cinema/${item.theater.slug}?date=${item.serviceDate}#cinema-showtimes-heading`
  }
  return `${cinemaMovieTarget(item.movie.slug, item.theater.id)}&date=${item.serviceDate}#schedule-heading`
}
</script>

<template>
  <HomeMovieSection
    title="À voir dans ma liste"
    :loading="loading"
    :action="accountAction"
    class="bg-[#f8f7f2]"
  >
    <div
      v-if="watchlistError || (owner && membership && preferenceError) || failed"
      class="editorial-alert flex flex-wrap items-center justify-between gap-3 p-4"
    >
      <p role="alert">Impossible de charger les séances de votre watchlist.</p>
      <button type="button" class="editorial-button-outline" @click="retry">
        Réessayer
      </button>
    </div>
    <p v-else-if="!owner" class="text-sm text-muted">
      {{
        account.session.value?.enabled ? 'Retrouvez ici les prochaines séances des films de votre watchlist.' : 'La watchlist est indisponible.'
      }}
    </p>
    <p v-else-if="!membership" class="text-sm text-muted">
      Ajoutez des films à votre watchlist pour retrouver leurs prochaines
      séances ici.
    </p>
    <div v-else-if="!selection" class="flex flex-wrap items-center gap-3">
      <p class="text-sm text-muted">
        Choisissez vos cinémas pour y retrouver les séances de vos films.
      </p>
      <NuxtLink to="/cinemas" class="editorial-button-outline"
        >Choisir mes cinémas</NuxtLink
      >
    </div>
    <p v-else-if="!items.length" class="text-sm text-muted">
      Aucune séance à venir pour les films de votre watchlist dans vos cinémas.
    </p>
    <ul v-else class="grid gap-4 lg:grid-cols-2">
      <li v-for="item in items" :key="item.movie.slug" class="min-w-0">
        <HomeMoviePreviewCard
          :title="item.movie.title"
          :poster="item.movie.poster_url"
          :to="showtimeTarget(item)"
        >
          <p class="text-sm font-semibold">
            <time :datetime="item.startsAt"
              >{{ formatLongDate(item.startDate) }}
              à {{ formatParisTime(item.startsAt) }}</time
            >
          </p>
          <p class="mt-1 text-sm text-muted [overflow-wrap:anywhere]">
            {{ theaterDisplayName(item.theater) }}
          </p>
        </HomeMoviePreviewCard>
      </li>
    </ul>
  </HomeMovieSection>
</template>
