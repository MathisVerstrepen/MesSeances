<script setup lang="ts">
import { LoaderCircle, LocateFixed, Search, X } from '@lucide/vue'
import {
  cinemaDirectorySuggestions,
  type CinemaDirectoryIndex,
  type DirectorySuggestion,
} from '~/utils/cinemaDirectoryIndex'

const props = defineProps<{
  index: CinemaDirectoryIndex | null
  initialQuery: string
  locating: boolean
}>()
const emit = defineEmits<{
  select: [suggestion: DirectorySuggestion]
  nearby: []
}>()

const LISTBOX_ID = 'cinema-search-suggestions'
const query = ref(props.initialQuery)
const open = ref(false)
const activeIndex = ref(-1)
const input = ref<HTMLInputElement | null>(null)

watch(
  () => props.initialQuery,
  (value) => {
    query.value = value
  },
)

const groups = computed(() =>
  props.index && open.value && query.value.trim()
    ? cinemaDirectorySuggestions(props.index, query.value)
    : [],
)
const options = computed(() => groups.value.flatMap((group) => group.items))
const renderedGroups = computed(() => {
  let offset = 0
  return groups.value.map((group) => {
    const items = group.items.map((item, position) => ({
      item,
      index: offset + position,
    }))
    offset += group.items.length
    return { ...group, items }
  })
})
const expanded = computed(() => options.value.length > 0)
const activeId = computed(() =>
  expanded.value && activeIndex.value >= 0
    ? `cinema-search-option-${activeIndex.value}`
    : undefined,
)

function countLabel(count: number) {
  return `${count} cinéma${count > 1 ? 's' : ''}`
}

function itemCount(item: DirectorySuggestion) {
  return 'count' in item ? countLabel(item.count) : ''
}

function itemDetail(item: DirectorySuggestion) {
  return 'detail' in item ? item.detail : ''
}

function onInput(event: Event) {
  if (!(event.currentTarget instanceof HTMLInputElement)) return
  query.value = event.currentTarget.value
  open.value = true
  activeIndex.value = -1
}

function choose(item: DirectorySuggestion) {
  open.value = false
  activeIndex.value = -1
  if (item.kind !== 'search') query.value = ''
  emit('select', item)
}

// Enter without a highlighted option never leaves the directory: it opens the
// best territory, chain or format match, otherwise a free-text search.
function defaultOption(): DirectorySuggestion | undefined {
  const first = options.value[0]
  if (first && first.kind !== 'theater' && first.kind !== 'search') return first
  return options.value.find((item) => item.kind === 'search')
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    open.value = true
    if (options.value.length)
      activeIndex.value = Math.min(
        activeIndex.value + 1,
        options.value.length - 1,
      )
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    activeIndex.value = Math.max(activeIndex.value - 1, -1)
  } else if (event.key === 'Escape') {
    if (expanded.value) event.preventDefault()
    open.value = false
    activeIndex.value = -1
  } else if (event.key === 'Enter') {
    event.preventDefault()
    if (!query.value.trim()) return
    open.value = true
    const item =
      activeIndex.value >= 0
        ? options.value[activeIndex.value]
        : defaultOption()
    if (item) choose(item)
  }
}

watch(activeIndex, async (index) => {
  if (index < 0) return
  await nextTick()
  document
    .getElementById(`cinema-search-option-${index}`)
    ?.scrollIntoView({ block: 'nearest' })
})

function onFocusOut(event: FocusEvent) {
  const next = event.relatedTarget
  if (
    next instanceof Node &&
    event.currentTarget instanceof HTMLElement &&
    event.currentTarget.contains(next)
  )
    return
  open.value = false
  activeIndex.value = -1
}

function clear() {
  query.value = ''
  open.value = false
  activeIndex.value = -1
  input.value?.focus()
}
</script>

<template>
  <form
    role="search"
    aria-label="Rechercher un cinéma"
    class="relative z-20 border-2 border-ink bg-[#f1efe8] p-4 shadow-[7px_7px_0_#27272a] sm:p-6"
    @submit.prevent
  >
    <label
      for="cinema-search"
      class="font-mono text-[0.65rem] font-black tracking-[0.15em] uppercase"
    >
      Ville, code postal, département, cinéma, enseigne ou format
    </label>
    <div class="mt-2.5 flex flex-wrap gap-3">
      <div
        class="relative flex min-w-0 flex-[1_1_26rem]"
        @focusout="onFocusOut"
      >
        <span
          class="grid size-[3.25rem] shrink-0 place-items-center border-2 border-r-0 border-ink bg-[#ffcf3f]"
          aria-hidden="true"
        >
          <Search :size="20" stroke-width="2.5" />
        </span>
        <input
          id="cinema-search"
          ref="input"
          :value="query"
          type="search"
          role="combobox"
          autocomplete="off"
          spellcheck="false"
          enterkeyhint="search"
          aria-autocomplete="list"
          :aria-expanded="expanded"
          :aria-controls="LISTBOX_ID"
          :aria-activedescendant="activeId"
          placeholder="Ex. Bordeaux, 75011, Gironde, IMAX…"
          class="h-[3.25rem] min-w-0 flex-1 rounded-none border-2 border-ink bg-surface px-[0.9rem] text-base font-bold text-ink outline-none [&::-webkit-search-cancel-button]:hidden focus:shadow-[inset_0_0_0_3px_var(--color-highlight)]"
          @input="onInput"
          @focus="open = true"
          @keydown="onKeydown"
        >
        <button
          v-if="query"
          type="button"
          class="absolute top-1/2 right-2 grid size-9 -translate-y-1/2 place-items-center text-ink hover:text-primary"
          aria-label="Effacer la recherche"
          @click="clear"
        >
          <X :size="18" aria-hidden="true" />
        </button>
        <ul
          v-show="expanded"
          :id="LISTBOX_ID"
          role="listbox"
          aria-label="Suggestions"
          class="absolute top-[calc(100%+0.4rem)] right-0 left-0 z-30 max-h-[min(28rem,70vh)] overflow-y-auto overscroll-contain border-2 border-ink bg-surface py-1.5 shadow-[6px_6px_0_#27272a]"
        >
          <li
            v-for="group in renderedGroups"
            :key="group.key"
            role="presentation"
            class="py-1"
          >
            <p
              :id="`cinema-search-group-${group.key}`"
              class="px-4 py-1.5 font-mono text-[0.62rem] font-black tracking-[0.14em] text-muted uppercase"
            >
              {{ group.title }}
            </p>
            <ul
              role="group"
              :aria-labelledby="`cinema-search-group-${group.key}`"
            >
              <li
                v-for="row in group.items"
                :id="`cinema-search-option-${row.index}`"
                :key="row.index"
                role="option"
                :aria-selected="row.index === activeIndex"
                class="flex cursor-pointer items-center justify-between gap-3 px-4 py-2.5 text-[0.95rem]"
                :class="row.index === activeIndex ? 'bg-[#fff3c4]' : 'hover:bg-[#fff3c4]'"
                @mousedown.prevent
                @click="choose(row.item)"
              >
                <span class="min-w-0 [overflow-wrap:anywhere]">
                  <strong class="font-extrabold">{{ row.item.label }}</strong>
                  <span
                    v-if="itemDetail(row.item)"
                    class="ml-2 text-sm font-semibold text-muted"
                    >{{
                      itemDetail(row.item)
                    }}</span
                  >
                </span>
                <span
                  v-if="itemCount(row.item)"
                  class="shrink-0 font-mono text-[0.65rem] font-bold text-muted uppercase"
                  >{{
                    itemCount(row.item)
                  }}</span
                >
              </li>
            </ul>
          </li>
        </ul>
      </div>
      <button
        type="button"
        class="inline-flex min-h-[3.25rem] shrink-0 items-center justify-center gap-2 border-2 border-ink bg-ink px-4 font-mono text-[0.68rem] font-black tracking-[0.08em] text-white uppercase shadow-[3px_3px_0_var(--color-highlight)] hover:bg-primary disabled:cursor-wait disabled:opacity-70 max-sm:flex-1"
        :disabled="locating"
        @click="emit('nearby')"
      >
        <LoaderCircle
          v-if="locating"
          :size="18"
          class="animate-spin motion-reduce:animate-none"
          aria-hidden="true"
        />
        <LocateFixed v-else :size="18" aria-hidden="true" />
        {{ locating ? 'Localisation…' : 'Autour de moi' }}
      </button>
    </div>
    <slot />
  </form>
</template>
