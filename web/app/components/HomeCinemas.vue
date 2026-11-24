<script setup lang="ts">
import { ArrowRight, Building2, Pencil, RefreshCw } from '@lucide/vue'

const {
  favoriteTheaters,
  isInitialized,
  error,
  initialize,
  retrySynchronization,
} = useCinemaPreferences()

onMounted(() => {
  void initialize()
})
</script>

<template>
  <section
    class="border-b-2 border-ink bg-[#f1efe8] text-ink"
    aria-labelledby="home-cinemas-title"
    :aria-busy="!isInitialized && !error"
  >
    <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10">
      <div class="flex items-center justify-between gap-3">
        <h2
          id="home-cinemas-title"
          class="editorial-title text-[clamp(1.75rem,4vw,2.75rem)]"
        >
          Mes cinémas<span class="text-primary">.</span>
        </h2>
        <NuxtLink
          v-if="isInitialized && favoriteTheaters.length > 0"
          to="/cinemas"
          aria-label="Modifier ma sélection"
          class="inline-flex min-h-11 shrink-0 items-center font-mono text-xs font-bold tracking-wider uppercase hover:text-primary"
        >
          <span
            class="inline-flex items-center gap-1.5 border-b-2 border-current pb-1 sm:gap-2"
          >
            <span
              >Modifier<span class="hidden sm:inline"> ma sélection</span></span
            >
            <Pencil :size="14" aria-hidden="true" />
          </span>
        </NuxtLink>
      </div>

      <div
        v-if="error"
        class="editorial-alert mt-5 flex flex-wrap items-center justify-between gap-4 p-4 sm:p-5"
        role="alert"
      >
        <div>
          <p class="font-bold">Votre sélection n’a pas pu être chargée.</p>
          <p class="mt-2 text-sm text-muted">{{ error }}</p>
        </div>
        <button
          type="button"
          class="editorial-button-outline"
          @click="retrySynchronization"
        >
          <RefreshCw :size="16" aria-hidden="true" />
          Réessayer
        </button>
      </div>

      <div v-else-if="!isInitialized" class="mt-5" role="status">
        <p class="text-sm font-medium text-muted">Chargement de vos cinémas…</p>
        <div class="mt-4 flex gap-4 overflow-hidden" aria-hidden="true">
          <div
            v-for="index in 3"
            :key="index"
            class="h-48 w-[85%] shrink-0 rounded-[0.4rem] border-2 border-ink/25 bg-[#e8e6de] sm:h-52 sm:w-[calc((100%-1rem)/2)] lg:w-[calc((100%-2rem)/3)]"
          />
        </div>
      </div>

      <ul
        v-else-if="favoriteTheaters.length > 0"
        class="-mx-1.5 mt-4 flex snap-x snap-proximity scroll-pl-1.5 gap-4 overflow-x-auto overscroll-x-contain px-1.5 pt-1.5 pb-3"
        aria-label="Mes cinémas sélectionnés"
        tabindex="0"
      >
        <li
          v-for="theater in favoriteTheaters"
          :key="theater.id"
          class="w-[85%] min-w-0 shrink-0 snap-start sm:w-[calc((100%-1rem)/2)] lg:w-[calc((100%-2rem)/3)]"
        >
          <HomeCinemaCard :theater="theater" />
        </li>
      </ul>

      <div
        v-else
        class="mt-5 flex flex-wrap items-center gap-x-4 gap-y-3 rounded-[0.4rem] border-2 border-ink bg-surface p-4 shadow-[4px_4px_0_var(--color-ink)] sm:p-5"
      >
        <span
          class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-highlight"
          aria-hidden="true"
        >
          <Building2 :size="21" stroke-width="1.75" />
        </span>
        <p class="min-w-0 flex-1 basis-40 text-sm leading-snug">
          Ajoutez vos cinémas favoris pour retrouver leurs séances ici.
        </p>
        <NuxtLink to="/cinemas" class="editorial-button">
          Choisir mes cinémas <ArrowRight :size="16" aria-hidden="true" />
        </NuxtLink>
      </div>
    </div>
  </section>
</template>
