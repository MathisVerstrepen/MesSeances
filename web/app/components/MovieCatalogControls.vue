<script setup lang="ts">
import { Search } from '@lucide/vue'
import type { MovieSort } from '~/types/api'
import { movieCatalogSortOptions } from '~/utils/movieCatalogPresentation'

const props = withDefaults(defineProps<{
  search: string
  sort: MovieSort
  pending?: boolean
  compact?: boolean
  inputId?: string
}>(), {
  pending: false,
  compact: false,
  inputId: 'movie-catalog-search'
})

const emit = defineEmits<{
  search: [value: string]
  sort: [value: MovieSort]
}>()

const searchInput = ref(props.search)

watch(() => props.search, (value) => {
  searchInput.value = value
})

function submitSearch() {
  emit('search', searchInput.value.trim())
}

function changeSort(event: Event) {
  if (!(event.currentTarget instanceof HTMLSelectElement)) return
  const value = event.currentTarget.value
  const sort = movieCatalogSortOptions.find((option) => option.value === value)?.value
  if (sort) emit('sort', sort)
}
</script>

<template>
  <div :class="compact ? 'grid gap-3 sm:grid-cols-[minmax(0,1fr)_14rem]' : 'grid grid-cols-[minmax(0,1fr)_3.25rem] items-end gap-4 lg:grid-cols-[minmax(0,1fr)_14rem_3.25rem] lg:gap-5'">
    <form :class="compact ? 'min-w-0' : 'col-span-2 min-w-0 lg:col-span-1'" role="search" @submit.prevent="submitSearch">
      <label class="block font-mono text-[0.65rem] font-extrabold uppercase tracking-[0.16em]" :for="inputId">Rechercher un film</label>
      <div class="mt-2 flex min-w-0">
        <input
          :id="inputId"
          v-model="searchInput"
          type="search"
          class="h-[3.25rem] min-w-0 flex-1 rounded-none border-2 border-r-0 border-ink bg-surface px-[0.9rem] text-[0.9rem] font-bold text-ink outline-none focus:shadow-[inset_0_0_0_2px_var(--color-highlight)] disabled:cursor-not-allowed disabled:opacity-55"
          autocomplete="off"
          placeholder="Titre du film"
          :disabled="pending"
        />
        <button type="submit" class="inline-flex min-h-[3.25rem] shrink-0 items-center justify-center gap-[0.55rem] border-2 border-ink bg-ink px-4 font-mono text-[0.68rem] font-extrabold uppercase tracking-[0.08em] text-white transition-[background-color] duration-150 enabled:hover:bg-primary disabled:cursor-not-allowed disabled:opacity-55 motion-reduce:transition-none" :disabled="pending">
          <Search :size="19" stroke-width="2.5" aria-hidden="true" />
          <span class="hidden sm:inline">Rechercher</span>
          <span class="sr-only sm:hidden">Rechercher</span>
        </button>
      </div>
    </form>

    <label class="block min-w-0">
      <span class="block font-mono text-[0.65rem] font-extrabold uppercase tracking-[0.16em]">Trier par</span>
      <select :value="sort" class="mt-2 h-[3.25rem] w-full rounded-none border-2 border-ink bg-surface px-[0.9rem] text-[0.9rem] font-bold text-ink outline-none focus:shadow-[inset_0_0_0_2px_var(--color-highlight)] disabled:cursor-not-allowed disabled:opacity-55" :disabled="pending" @change="changeSort">
        <option v-for="option in movieCatalogSortOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
      </select>
    </label>

    <slot name="actions" />
  </div>
</template>
