<script setup lang="ts">
import type { Theater, TheaterActivityItem } from '~/types/api'
import {
  activityHistoryDate,
  activityObservationDay,
  activityShowtimesTarget,
  activityTypeLabel,
} from '~/utils/cinemaActivity'
import { cinemaMovieTarget } from '~/utils/cinemaMovieTarget'
import { theaterDisplayName } from '~/utils/theaterDisplayName'

type ActivityPreview = TheaterActivityItem & { theater: Theater }

const previewLimit = 4
const concurrentRequests = 4
const preferences = useCinemaPreferences()
const api = useMesSeancesApi()
const { favoriteTheaters, isInitialized, error: preferenceError } = preferences
const selection = computed(() =>
  favoriteTheaters.value.map((theater) => theater.slug).join(','),
)
const mounted = ref(false)
const items = shallowRef<ActivityPreview[]>([])
const pending = ref(false)
const failed = ref(false)
const retryCount = ref(0)
const loading = computed(
  () => !mounted.value || preferences.isLoading.value || pending.value,
)

onMounted(() => {
  mounted.value = true
})

watch(
  [
    mounted,
    isInitialized,
    preferences.selectionScopeKey,
    selection,
    preferenceError,
    retryCount,
  ],
  async ([isMounted, isReady, , theaters, cinemaError], _, onCleanup) => {
    items.value = []
    failed.value = false
    pending.value = false
    if (!isMounted || !isReady || !theaters || cinemaError) return

    const request = new AbortController()
    const { signal } = request
    onCleanup(() => request.abort())
    pending.value = true
    const selected = favoriteTheaters.value
    const events = new Map<string, ActivityPreview>()
    let hadFailure = false
    for (let start = 0; start < selected.length; start += concurrentRequests) {
      const batch = await Promise.allSettled(
        selected
          .slice(start, start + concurrentRequests)
          .map(async (theater) => {
            const response = await api.theaterActivity(
              theater.slug,
              { limit: previewLimit },
              signal,
            )
            return response.items.map((item) => ({ ...item, theater }))
          }),
      )
      if (signal.aborted) return
      for (const result of batch) {
        if (result.status === 'rejected') hadFailure = true
        else for (const item of result.value) events.set(item.event_id, item)
      }
    }
    items.value = [...events.values()]
      .sort(
        (left, right) =>
          Date.parse(right.detected_at) - Date.parse(left.detected_at) ||
          right.event_id.length - left.event_id.length ||
          right.event_id.localeCompare(left.event_id),
      )
      .slice(0, previewLimit)
    failed.value = hadFailure
    pending.value = false
  },
)

function retry() {
  if (preferenceError.value) void preferences.retrySynchronization()
  else retryCount.value++
}
</script>

<template>
  <HomeMovieSection
    title="Nouveau dans mes cinémas"
    :loading="loading"
    class="bg-[#f1efe8]"
  >
    <div
      v-if="preferenceError || failed"
      class="editorial-alert flex flex-wrap items-center justify-between gap-3 p-4"
    >
      <p role="alert">
        {{
          items.length ? 'Les nouveautés de certains cinémas sont indisponibles.' : 'Impossible de charger les nouveautés de vos cinémas.'
        }}
      </p>
      <button type="button" class="editorial-button-outline" @click="retry">
        Réessayer
      </button>
    </div>
    <div v-else-if="!selection" class="flex flex-wrap items-center gap-3">
      <p class="text-sm text-muted">
        Choisissez vos cinémas pour suivre les nouveautés de leur programmation.
      </p>
      <NuxtLink to="/cinemas" class="editorial-button-outline"
        >Choisir mes cinémas</NuxtLink
      >
    </div>
    <p v-else-if="!items.length" class="text-sm text-muted">
      Aucun ajout à la programmation détecté dans vos cinémas pour le moment.
    </p>
    <ul
      v-if="items.length"
      class="grid gap-4 lg:grid-cols-2"
      :class="{ 'mt-4': failed }"
    >
      <li v-for="item in items" :key="item.event_id" class="min-w-0">
        <HomeMoviePreviewCard
          :title="item.movie.title"
          :poster="item.movie.poster_url"
          :to="activityShowtimesTarget(item, item.theater.id) ?? cinemaMovieTarget(item.movie.slug, item.theater.id)"
        >
          <p class="text-sm font-semibold">
            {{ activityTypeLabel(item.type) }} ·
            <time :datetime="item.detected_at">{{
              activityHistoryDate(activityObservationDay(item.detected_at))
            }}</time>
          </p>
          <NuxtLink
            :to="{ path: `/cinema/${item.theater.slug}`, query: { view: 'activity' } }"
            class="mt-1 inline-flex min-h-7 items-center text-sm text-muted underline underline-offset-4 hover:text-primary [overflow-wrap:anywhere]"
          >
            {{ theaterDisplayName(item.theater) }}
          </NuxtLink>
        </HomeMoviePreviewCard>
      </li>
    </ul>
  </HomeMovieSection>
</template>
