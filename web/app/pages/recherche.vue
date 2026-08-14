<script setup lang="ts">
import { AlertTriangle, CalendarSearch, LoaderCircle, Search, SlidersHorizontal } from '@lucide/vue'
import type { Language, SlotResult } from '~/types/api'
import { createServiceTimeOptions, formatLongDate, todayInParis } from '~/utils/date'

const api = useMovieFlowApi()
const form = reactive({
  city: 'Lille',
  date: todayInParis(),
  startAfter: '12:00',
  finishBefore: '15:00',
  language: 'ALL' as Language,
  includeAds: true
})
const timeOptions = createServiceTimeOptions()
const results = ref<SlotResult[] | null>(null)
const pending = ref(false)
const errorMessage = ref('')
const searchedDate = ref('')

async function submitSearch() {
  pending.value = true
  errorMessage.value = ''
  results.value = null
  try {
    results.value = await api.searchSlot({
      city: form.city,
      date: form.date,
      start_after: form.startAfter,
      finish_before: form.finishBefore,
      buffer_ads: form.includeAds ? 20 : 0,
      language: form.language
    })
    searchedDate.value = form.date
  } catch (error) {
    errorMessage.value = getFrenchApiError(error)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <main class="mx-auto max-w-[1280px] px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10">
    <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Trouver une séance</h1>

    <div class="mt-7 grid gap-8 lg:grid-cols-[320px_minmax(0,1fr)] lg:gap-10 lg:items-start">
      <form class="lg:sticky lg:top-6 lg:border-r lg:border-line lg:pr-8" @submit.prevent="submitSearch">
        <div class="mb-5 flex items-center gap-2.5 border-b border-line pb-4">
          <SlidersHorizontal :size="18" class="text-accent" aria-hidden="true" />
          <h2 class="font-semibold text-ink">Votre disponibilité</h2>
        </div>

        <div class="space-y-5">
          <label class="block text-sm font-medium text-ink">
            <span class="mb-1.5 block">Ville</span>
            <input v-model.trim="form.city" required type="text" autocomplete="address-level2" class="field" />
          </label>
          <label class="block text-sm font-medium text-ink">
            <span class="mb-1.5 block">Date de la séance</span>
            <input v-model="form.date" required type="date" class="field" />
          </label>

          <div class="grid grid-cols-2 gap-3">
            <label class="block text-sm font-medium text-ink">
              <span class="mb-1.5 block">À partir de</span>
              <select v-model="form.startAfter" class="field">
                <option v-for="option in timeOptions" :key="`start-${option.value}`" :value="option.value">{{ option.label }}</option>
              </select>
            </label>
            <label class="block text-sm font-medium text-ink">
              <span class="mb-1.5 block">Terminé avant</span>
              <select v-model="form.finishBefore" class="field">
                <option v-for="option in timeOptions" :key="`finish-${option.value}`" :value="option.value">{{ option.label }}</option>
              </select>
            </label>
          </div>

          <label class="block text-sm font-medium text-ink">
            <span class="mb-1.5 block">Langue</span>
            <select v-model="form.language" class="field">
              <option value="ALL">Toutes</option>
              <option value="VOSTFR">VOSTFR</option>
              <option value="VF">VF</option>
            </select>
          </label>

          <label class="flex cursor-pointer items-start gap-3 rounded-md border border-line bg-subtle p-3 text-sm text-ink">
            <input v-model="form.includeAds" type="checkbox" class="mt-0.5 size-4 accent-accent" />
            <span>Inclure les publicités (+20 min)</span>
          </label>

          <button type="submit" class="button-primary w-full" :disabled="pending">
            <LoaderCircle v-if="pending" :size="18" class="animate-spin" aria-hidden="true" />
            <Search v-else :size="18" aria-hidden="true" />
            {{ pending ? 'Recherche…' : 'Trouver une séance' }}
          </button>
        </div>
      </form>

      <section aria-live="polite" aria-label="Résultats de recherche">
        <div class="mb-5 flex items-end justify-between gap-4">
          <div>
            <p class="text-sm font-medium text-muted">Résultats</p>
            <h2 class="mt-1 text-xl font-semibold capitalize text-ink">{{ searchedDate ? formatLongDate(searchedDate) : 'Lancez votre recherche' }}</h2>
          </div>
          <span v-if="results" class="text-sm text-muted">{{ results.length }} séance{{ results.length > 1 ? 's' : '' }}</span>
        </div>

        <div v-if="pending" class="state-panel" role="status">
          <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
          <p>Recherche des séances compatibles…</p>
        </div>
        <div v-else-if="errorMessage" class="state-panel" role="alert">
          <AlertTriangle :size="28" class="text-red-600" aria-hidden="true" />
          <p class="max-w-lg">{{ errorMessage }}</p>
        </div>
        <div v-else-if="results?.length === 0" class="state-panel">
          <CalendarSearch :size="30" class="text-muted" aria-hidden="true" />
          <p>Aucune séance ne tient entièrement dans ce créneau.</p>
        </div>
        <div v-else-if="results" class="divide-y divide-line rounded-lg border border-line bg-surface">
          <SlotResultCard v-for="result in results" :key="result.showtime.id" :result="result" />
        </div>
        <div v-else class="state-panel">
          <CalendarSearch :size="30" class="text-accent" aria-hidden="true" />
          <p>Définissez votre créneau pour voir les séances compatibles.</p>
        </div>
      </section>
    </div>

    <div class="mt-8 flex items-start gap-3 border-l-2 border-accent bg-orange-50 px-4 py-3 text-sm text-stone-700">
      <AlertTriangle :size="18" class="mt-0.5 shrink-0 text-accent" aria-hidden="true" />
      <p><strong>Données de démonstration.</strong> Résultats fictifs, sans lien de réservation.</p>
    </div>
  </main>
</template>
