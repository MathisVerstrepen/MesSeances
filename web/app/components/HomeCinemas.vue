<script setup lang="ts">
import {
  ArrowRight,
  Building2,
  MapPin,
  Pencil,
  Plus,
  RefreshCw,
} from '@lucide/vue'
import { theaterDisplayName } from '~/utils/theaterDisplayName'

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
    <div class="mx-auto max-w-[1440px] px-4 py-10 sm:px-6 sm:py-14 lg:px-10">
      <div>
        <p class="font-mono text-xs font-bold uppercase tracking-[0.18em]">
          Vos salles, à portée de main
        </p>
        <div class="mt-3 flex items-center justify-between gap-3">
          <h2
            id="home-cinemas-title"
            class="min-w-0 text-4xl font-black leading-none tracking-[-0.05em] sm:text-5xl"
          >
            Mes cinémas<span class="text-primary">.</span>
          </h2>
          <NuxtLink
            v-if="isInitialized && favoriteTheaters.length > 0"
            to="/cinemas"
            aria-label="Modifier ma sélection"
            class="inline-flex size-11 shrink-0 items-center justify-center gap-2 text-xs font-bold uppercase tracking-[0.1em] hover:text-primary sm:h-auto sm:min-h-11 sm:w-auto sm:border-b-2 sm:border-ink"
          >
            <Pencil :size="21" class="sm:hidden" aria-hidden="true" />
            <span class="hidden sm:inline">Modifier ma sélection</span>
            <Plus :size="17" class="hidden sm:block" aria-hidden="true" />
          </NuxtLink>
        </div>
      </div>

      <div
        v-if="error"
        class="mt-8 flex flex-wrap items-center justify-between gap-4 border-2 border-ink bg-surface p-6"
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

      <div v-else-if="!isInitialized" class="mt-8" role="status">
        <p class="text-sm font-medium text-muted">Chargement de vos cinémas…</p>
        <div class="mt-4 flex gap-5 overflow-hidden" aria-hidden="true">
          <div
            v-for="index in 3"
            :key="index"
            class="h-56 w-[85%] shrink-0 border-2 border-ink/20 bg-surface sm:w-[calc((100%-1.25rem)/2)] lg:w-[calc((100%-2.5rem)/3)]"
          />
        </div>
      </div>

      <template v-else-if="favoriteTheaters.length > 0">
        <ul
          class="-mx-1 mt-8 flex snap-x snap-proximity gap-5 overflow-x-auto overscroll-x-contain px-1 pt-1 pb-4"
          aria-label="Mes cinémas sélectionnés"
          tabindex="0"
        >
          <li
            v-for="theater in favoriteTheaters"
            :key="theater.id"
            class="w-[85%] min-w-0 shrink-0 snap-start sm:w-[calc((100%-1.25rem)/2)] lg:w-[calc((100%-2.5rem)/3)]"
          >
            <NuxtLink
              :to="`/cinema/${encodeURIComponent(theater.slug)}`"
              :aria-label="`Voir les séances : ${theaterDisplayName(theater)}`"
              class="group flex h-full flex-col border-2 border-ink bg-surface shadow-[4px_4px_0_#27272a] transition-colors hover:bg-[#e8eddf]"
            >
              <div
                class="flex items-center justify-between gap-4 border-b-2 border-ink bg-highlight px-5 py-3"
              >
                <span
                  class="flex min-w-0 items-center gap-2 font-mono text-[11px] font-bold uppercase tracking-[0.08em]"
                >
                  <MapPin :size="15" class="shrink-0" aria-hidden="true" />
                  <span class="break-words [overflow-wrap:anywhere]">{{
                    theater.city
                  }}</span>
                </span>
                <Building2 :size="22" class="shrink-0" aria-hidden="true" />
              </div>
              <div class="flex flex-1 flex-col p-5">
                <h3
                  class="text-xl font-black leading-tight tracking-[-0.03em] [overflow-wrap:anywhere]"
                >
                  <TheaterName
                    :name="theaterDisplayName(theater)"
                    :provider="theater.provider"
                  />
                </h3>
                <p
                  class="mt-3 text-sm leading-6 text-muted [overflow-wrap:anywhere]"
                >
                  <template v-if="theater.address"
                    >{{ theater.address }}<br></template
                  >
                  {{ theater.postal_code }} {{ theater.city }}
                </p>
                <span
                  class="mt-6 flex items-center justify-between gap-3 border-t border-dashed border-ink/40 pt-4 text-xs font-black uppercase tracking-[0.08em]"
                >
                  Voir les séances
                  <ArrowRight
                    :size="18"
                    class="shrink-0 transition-transform group-hover:translate-x-1 motion-reduce:transition-none"
                    aria-hidden="true"
                  />
                </span>
              </div>
            </NuxtLink>
          </li>
        </ul>
      </template>

      <div
        v-else
        class="mt-8 grid gap-6 border-2 border-ink bg-surface p-6 shadow-[5px_5px_0_#27272a] sm:p-8 lg:grid-cols-[auto_minmax(0,1fr)_auto] lg:items-center lg:gap-8"
      >
        <span
          class="grid size-20 -rotate-3 place-items-center border-2 border-ink bg-highlight shadow-[4px_4px_0_#27272a]"
          aria-hidden="true"
        >
          <Building2 :size="38" stroke-width="1.75" />
        </span>
        <div>
          <h3
            class="text-2xl font-black leading-tight tracking-[-0.035em] sm:text-3xl"
          >
            Et si on commençait par vos salles ?
          </h3>
          <p class="mt-3 max-w-xl text-sm font-medium leading-6 text-muted">
            Choisissez les cinémas où vous aimez aller. Retrouvez-les ici pour
            consulter leurs séances en un clic.
          </p>
        </div>
        <NuxtLink
          to="/cinemas"
          class="editorial-button justify-self-start lg:justify-self-end"
        >
          Choisir mes cinémas <ArrowRight :size="18" aria-hidden="true" />
        </NuxtLink>
      </div>
    </div>
  </section>
</template>
