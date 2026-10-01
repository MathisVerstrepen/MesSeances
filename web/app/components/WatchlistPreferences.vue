<script setup lang="ts">
import { ArrowDownUp, List, ListFilter, Tags } from '@lucide/vue'
import type {
  WatchlistSortOrder,
  WatchlistTag,
  WatchlistViewMode,
} from '~/types/watchlist'
import { watchlistSortOptions } from '~/utils/watchlistSort'

defineProps<{
  mobile?: boolean
  mode: WatchlistViewMode | undefined
  filter: string
  sort: WatchlistSortOrder | undefined
  tags: WatchlistTag[]
  ready: boolean
  blocked: boolean
  error: string
}>()
defineEmits<{
  display: [mode: WatchlistViewMode, event: Event]
  filter: [event: Event]
  sort: [event: Event]
}>()
const tagFilter = useTemplateRef('tagFilter')
defineExpose({
  focusFilter: () => tagFilter.value?.focus({ preventScroll: true }),
})
</script>

<template>
  <div
    :class="mobile ? 'space-y-5' : 'grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-end gap-3'"
  >
    <div
      :class="mobile ? '' : 'col-span-3 flex items-center justify-between gap-3'"
    >
      <slot name="heading" />
      <div
        role="group"
        aria-label="Affichage des films"
        class="inline-flex shrink-0 border-2 border-ink bg-surface"
      >
        <button
          v-for="option in ([{ value: 'list', label: 'Liste' }, { value: 'tags', label: 'Par tag' }] as const)"
          :key="option.value"
          type="button"
          :aria-pressed="mode === option.value"
          :disabled="blocked"
          class="relative inline-flex min-h-12 min-w-27 items-center justify-center gap-2 px-4 font-mono text-xs font-black uppercase tracking-[0.08em] not-first:border-l-2 not-first:border-ink focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-solid focus-visible:outline-offset-2 focus-visible:outline-ink focus-visible:ring-offset-0 disabled:opacity-50"
          :class="mode === option.value ? 'bg-ink text-surface shadow-[inset_0_-4px_0_var(--color-highlight)]' : 'bg-surface text-ink enabled:hover:bg-subtle'"
          @click="$emit('display', option.value, $event)"
        >
          <component
            :is="option.value === 'list' ? List : Tags"
            :size="18"
            class="shrink-0"
            aria-hidden="true"
            focusable="false"
          />
          {{ option.label }}
        </button>
      </div>
    </div>
    <div class="relative min-w-0">
      <label
        :for="mobile ? 'watchlist-mobile-tag-filter' : 'watchlist-tag-filter'"
        class="sr-only"
        >Filtrer par tag</label
      >
      <ListFilter
        :size="20"
        class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink"
        aria-hidden="true"
      />
      <select
        :id="mobile ? 'watchlist-mobile-tag-filter' : 'watchlist-tag-filter'"
        ref="tagFilter"
        :value="filter"
        class="account-input min-h-11 w-full min-w-0 pl-10!"
        :disabled="blocked"
        @change="$emit('filter', $event)"
      >
        <option v-if="!mode" value="" disabled>
          {{ error ? 'Filtre indisponible' : 'Filtrer par tag' }}
        </option>
        <option v-else value="">Tous les films</option>
        <option v-for="tag in tags" :key="tag.id" :value="tag.id">
          {{ tag.name }}
        </option>
      </select>
    </div>
    <div class="relative min-w-0">
      <label
        :for="mobile ? 'watchlist-mobile-sort' : 'watchlist-sort'"
        class="sr-only"
        >Trier par</label
      >
      <ArrowDownUp
        :size="20"
        class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink"
        aria-hidden="true"
      />
      <select
        :id="mobile ? 'watchlist-mobile-sort' : 'watchlist-sort'"
        class="account-input min-h-11 w-full min-w-0 pl-10!"
        :value="ready ? sort : ''"
        :disabled="!ready || blocked"
        @change="$emit('sort', $event)"
      >
        <option v-if="!ready" value="" disabled>
          {{ error ? 'Tri indisponible' : 'Trier par' }}
        </option>
        <option
          v-for="option in watchlistSortOptions"
          :key="option.value"
          :value="option.value"
          :aria-label="option.label"
        >
          {{ option.shortLabel }}
        </option>
      </select>
    </div>
    <slot />
  </div>
</template>
