<script setup lang="ts">
import {
  AlertTriangle,
  CalendarDays,
  LoaderCircle,
  RefreshCw,
} from '@lucide/vue'
import type { TheaterActivityResponse } from '~/types/api'
import {
  activityFullDate,
  activityObservationDay,
  activityShowtimesTarget,
  activityTypeLabel,
  appendActivityItems,
  groupActivityItems,
} from '~/utils/cinemaActivity'
import { cinemaMovieTarget } from '~/utils/cinemaMovieTarget'

const props = defineProps<{ slug: string }>()
const api = useMesSeancesApi()
// Capture identity once. Parent keys this component by slug, never by date/view settings.
const activitySlug = props.slug
const PAGE_SIZE = 20
const response = ref<TheaterActivityResponse | null>(null)
const firstError = ref('')
const moreError = ref('')
const morePending = ref(false)
let disposed = false
let requestId = 0
let initialController: AbortController | undefined
let moreController: AbortController | undefined

function invalidateRequests() {
  requestId++
  initialController?.abort()
  moreController?.abort()
}

// Register before async setup yields, including while the first request is pending.
onScopeDispose(() => {
  disposed = true
  invalidateRequests()
})
watch(() => props.slug, invalidateRequests, { flush: 'sync' })

function isCurrentRequest(id: number): boolean {
  return !disposed && props.slug === activitySlug && id === requestId
}

const initial = await useAsyncData(
  `cinema-activity:${activitySlug}`,
  async (_nuxtApp, { signal }) => {
    const id = ++requestId
    initialController?.abort()
    initialController = new AbortController()
    try {
      const page = await api.theaterActivity(
        activitySlug,
        { limit: PAGE_SIZE },
        AbortSignal.any([signal, initialController.signal]),
      )
      return {
        response: isCurrentRequest(id) ? page : null,
        errorMessage: '',
      }
    } catch (cause) {
      return {
        response: null,
        errorMessage: isCurrentRequest(id)
          ? getFrenchActivityApiError(cause)
          : '',
      }
    }
  },
  { lazy: true },
)

watch(
  initial.data,
  (state) => {
    if (disposed || props.slug !== activitySlug || !state) return
    response.value = state.response
      ? {
          ...state.response,
          items: appendActivityItems([], state.response.items),
        }
      : null
    firstError.value = state.errorMessage
  },
  { immediate: true },
)
const firstPending = computed(
  () => initial.status.value === 'pending' || initial.status.value === 'idle',
)
const groups = computed(() => groupActivityItems(response.value?.items ?? []))
const historyStart = computed(() => {
  const started = response.value?.coverage.history_started_at
  return started ? activityObservationDay(started) : ''
})

async function retryInitial() {
  if (disposed || props.slug !== activitySlug || firstPending.value) return
  await initial.refresh()
}

async function loadMore() {
  const cursor = response.value?.next_cursor
  if (!cursor || morePending.value || firstPending.value || disposed) return
  const id = ++requestId
  moreController = new AbortController()
  morePending.value = true
  moreError.value = ''
  try {
    const page = await api.theaterActivity(
      activitySlug,
      { limit: PAGE_SIZE, cursor },
      moreController.signal,
    )
    if (!isCurrentRequest(id) || response.value?.next_cursor !== cursor) return
    response.value = {
      ...response.value,
      items: appendActivityItems(response.value.items, page.items),
      next_cursor: page.next_cursor,
    }
  } catch (cause) {
    if (isCurrentRequest(id)) moreError.value = getFrenchActivityApiError(cause)
  } finally {
    if (isCurrentRequest(id)) morePending.value = false
  }
}
</script>

<template>
  <div class="mt-8" :aria-busy="firstPending || morePending">
    <div v-if="firstPending" class="max-w-4xl">
      <p role="status" aria-live="polite" class="sr-only">
        Chargement de l’activité…
      </p>
      <div aria-hidden="true" class="space-y-6 motion-safe:animate-pulse">
        <div class="h-5 w-52 bg-ink/10" />
        <div
          v-for="row in 3"
          :key="row"
          class="flex gap-4 border-b border-ink/20 pb-6"
        >
          <div class="h-18 w-12 shrink-0 bg-ink/10" />
          <div class="min-w-0 flex-1 space-y-3 pt-1">
            <div class="h-5 w-3/4 max-w-96 bg-ink/10" />
            <div class="h-4 w-1/2 max-w-64 bg-ink/10" />
          </div>
        </div>
      </div>
    </div>
    <EditorialStatePanel
      v-else-if="firstError || !response"
      semantic="alert"
      size="compact"
      shadow="small"
      class="mx-auto max-w-3xl font-bold"
    >
      <template #icon
        ><AlertTriangle
          :size="30"
          class="text-primary"
          aria-hidden="true"
        /></template
      >
      <template #heading
        ><h3 class="text-2xl font-black">
          Impossible de charger l’activité
        </h3></template
      >
      <p>
        {{ firstError || 'Le service historique ne répond pas. Réessayez.' }}
      </p>
      <template #actions>
        <button type="button" class="editorial-button" @click="retryInitial">
          <RefreshCw :size="17" aria-hidden="true" />
          Réessayer
        </button>
      </template>
    </EditorialStatePanel>
    <template v-else>
      <p v-if="historyStart" class="mb-8 max-w-3xl text-sm leading-6">
        Historique suivi depuis le
        <time :datetime="historyStart">{{
          activityFullDate(historyStart)
        }}</time
        >. Les programmations antérieures ne sont pas reconstituées.
      </p>
      <EditorialStatePanel
        v-if="!response.coverage.history_started_at"
        size="compact"
        shadow="small"
        class="mx-auto max-w-3xl"
      >
        <template #icon
          ><CalendarDays :size="30" aria-hidden="true" /></template
        >
        <template #heading
          ><h3 class="text-2xl font-black">
            Historique en cours d’initialisation
          </h3></template
        >
        <p>
          L’historique commencera après la prochaine synchronisation de ce
          cinéma.
        </p>
      </EditorialStatePanel>
      <EditorialStatePanel
        v-else-if="response.items.length === 0"
        size="compact"
        shadow="small"
        class="mx-auto max-w-3xl"
      >
        <template #icon
          ><CalendarDays :size="30" aria-hidden="true" /></template
        >
        <template #heading
          ><h3 class="text-2xl font-black">
            Aucune nouvelle programmation détectée.
          </h3></template
        >
      </EditorialStatePanel>
      <ol v-else class="max-w-5xl" aria-label="Historique de la programmation">
        <li
          v-for="group in groups"
          :key="group.day"
          class="border-b border-ink/30 py-6 first:pt-0 lg:grid lg:grid-cols-[13rem_minmax(0,1fr)] lg:gap-8"
        >
          <h3 class="mb-5 text-base font-bold capitalize lg:mb-0 lg:pt-1">
            <time :datetime="group.day">{{ activityFullDate(group.day) }}</time>
          </h3>
          <ul class="space-y-6 border-l border-ink/30 pl-4 sm:pl-6">
            <li
              v-for="item in group.items"
              :key="item.event_id"
              class="flex items-start gap-4"
            >
              <NuxtLink
                :to="cinemaMovieTarget(item.movie.slug, response.theater.id)"
                :aria-label="item.movie.title"
                class="block w-12 shrink-0 sm:w-16"
              >
                <PosterImage
                  :src="item.movie.poster_url"
                  alt=""
                  sizes="(min-width: 640px) 64px, 48px"
                  :reset-key="item.event_id"
                  fallback-variant="icon-only"
                  :fallback-icon-size="24"
                  class="aspect-[2/3] bg-subtle"
                  image-class="size-full object-cover"
                  fallback-class="text-muted"
                />
              </NuxtLink>
              <div class="min-w-0 flex-1">
                <h4 class="editorial-heading break-words">
                  <NuxtLink
                    :to="cinemaMovieTarget(item.movie.slug, response.theater.id)"
                    class="hover:underline underline-offset-4"
                  >
                    {{ item.movie.title }}
                  </NuxtLink>
                </h4>
                <p class="mt-2 text-sm font-bold">
                  {{ activityTypeLabel(item.type) }}
                </p>
                <p class="mt-1 text-sm leading-6">
                  Première séance annoncée le
                  <time :datetime="item.first_screening_date">{{
                    activityFullDate(item.first_screening_date)
                  }}</time>
                </p>
                <p
                  v-if="item.type === 'return_to_program' && item.previous_program_end_date"
                  class="text-sm leading-6"
                >
                  Dernière programmation jusqu’au
                  <time :datetime="item.previous_program_end_date">{{
                    activityFullDate(item.previous_program_end_date)
                  }}</time>
                </p>
                <NuxtLink
                  v-if="activityShowtimesTarget(item, response.theater.id)"
                  :to="activityShowtimesTarget(item, response.theater.id)!"
                  class="mt-3 inline-flex min-h-11 items-center text-sm font-bold underline underline-offset-4 hover:text-primary"
                >
                  Voir les séances
                </NuxtLink>
              </div>
            </li>
          </ul>
        </li>
      </ol>
      <div
        v-if="response.next_cursor"
        class="mt-8 flex flex-col items-start gap-4"
      >
        <p
          v-if="moreError"
          role="alert"
          class="flex max-w-3xl items-start gap-3 border-l-2 border-primary pl-4 text-sm text-primary"
        >
          <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
          <span>Impossible de charger la suite. {{ moreError }}</span>
        </p>
        <button
          type="button"
          class="editorial-button-outline"
          :disabled="morePending"
          @click="loadMore"
        >
          <LoaderCircle
            v-if="morePending"
            :size="17"
            class="motion-safe:animate-spin"
            aria-hidden="true"
          />
          {{
            morePending ? 'Chargement…' : moreError ? 'Réessayer' : 'Afficher plus'
          }}
        </button>
        <p role="status" aria-live="polite" class="sr-only">
          {{ morePending ? 'Chargement de la suite de l’activité…' : '' }}
        </p>
      </div>
    </template>
  </div>
</template>
