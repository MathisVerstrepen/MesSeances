<script setup lang="ts">
import {
  AlertTriangle,
  CalendarDays,
  LoaderCircle,
  RefreshCw,
} from '@lucide/vue'
import type { TheaterActivityResponse } from '~/types/api'
import {
  activityHistoryDate,
  activityObservationDay,
  appendActivityItems,
  groupActivityItems,
} from '~/utils/cinemaActivity'

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
    <div v-if="firstPending">
      <p role="status" aria-live="polite" class="sr-only">
        Chargement de l’activité…
      </p>
      <ActivityTimelineSkeleton />
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
      <details v-if="historyStart" class="mb-6 max-w-3xl text-sm text-muted">
        <summary
          class="min-h-11 w-fit max-w-full cursor-pointer content-center py-2 leading-5 hover:text-ink"
        >
          Historique suivi depuis le
          <time :datetime="historyStart">{{
            activityHistoryDate(historyStart)
          }}</time>
        </summary>
        <p class="max-w-prose pb-2 leading-5">
          Les programmations antérieures ne sont pas reconstituées.
        </p>
      </details>
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
      <ActivityTimeline
        v-else
        :groups="groups"
        :theater-id="response.theater.id"
        :date-heading-level="3"
        label="Historique de la programmation"
      />
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
