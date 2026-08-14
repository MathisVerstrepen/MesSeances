<script setup lang="ts">
import { AlertTriangle, CalendarDays, LoaderCircle, RefreshCw } from '@lucide/vue'
import type { Language, TimelineResponse } from '~/types/api'
import { formatLongDate, todayInParis } from '~/utils/date'

const api = useMovieFlowApi()
const date = ref(todayInParis())
const language = ref<Language>('ALL')
const timeline = ref<TimelineResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
let requestId = 0

async function loadTimeline() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  try {
    const response = await api.timeline({ date: date.value, language: language.value })
    if (currentRequest === requestId) timeline.value = response
  } catch (error) {
    if (currentRequest === requestId) {
      timeline.value = null
      errorMessage.value = getFrenchApiError(error)
    }
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

watch([date, language], loadTimeline)
onMounted(loadTimeline)

const showtimeCount = computed(() => timeline.value?.theaters.reduce((total, theater) => total + theater.showtimes.length, 0) ?? 0)
</script>

<template>
  <main class="mx-auto max-w-[1440px] px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10">
    <div class="flex flex-col justify-between gap-5 xl:flex-row xl:items-end">
      <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Planning des séances</h1>
      <div class="grid gap-3 sm:grid-cols-2 xl:w-[430px]">
        <label class="block text-sm font-medium text-ink">
          <span class="mb-1.5 flex items-center gap-2"><CalendarDays :size="16" class="text-muted" aria-hidden="true" /> Date</span>
          <input v-model="date" type="date" class="field" />
        </label>
        <label class="block text-sm font-medium text-ink">
          <span class="mb-1.5 block">Langue</span>
          <select v-model="language" class="field">
            <option value="ALL">Toutes</option>
            <option value="VOSTFR">VOSTFR</option>
            <option value="VF">VF</option>
          </select>
        </label>
      </div>
    </div>

    <div class="mt-6 flex items-start gap-3 border-l-2 border-accent bg-orange-50 px-4 py-3 text-sm text-stone-700">
      <AlertTriangle :size="18" class="mt-0.5 shrink-0 text-accent" aria-hidden="true" />
      <p><strong>Données de démonstration.</strong> Ces horaires fictifs ne représentent pas la programmation actuelle des cinémas.</p>
    </div>

    <div class="mb-3 mt-8 flex items-baseline justify-between gap-4">
      <h2 class="text-base font-semibold capitalize text-ink">{{ formatLongDate(date) }}</h2>
      <p v-if="timeline && !pending" class="text-sm text-muted">{{ showtimeCount }} séance{{ showtimeCount > 1 ? 's' : '' }}</p>
    </div>

    <div v-if="pending" class="state-panel" role="status" aria-live="polite">
      <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
      <p>Chargement des séances…</p>
    </div>

    <div v-else-if="errorMessage" class="state-panel" role="alert">
      <AlertTriangle :size="28" class="text-red-600" aria-hidden="true" />
      <p class="max-w-lg">{{ errorMessage }}</p>
      <button type="button" class="button-primary" @click="loadTimeline">
        <RefreshCw :size="17" aria-hidden="true" /> Réessayer
      </button>
    </div>

    <div v-else-if="!timeline || showtimeCount === 0" class="state-panel">
      <CalendarDays :size="28" class="text-muted" aria-hidden="true" />
      <p>Aucune séance pour cette date et cette langue.</p>
    </div>

    <TimelineMatrix v-else :timeline="timeline" />
  </main>
</template>
