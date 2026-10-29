<script setup lang="ts">
import { Check, ChevronDown, LoaderCircle, Settings2, X } from '@lucide/vue'
import type { Language, QueryFormat, Theater } from '~/types/api'
import { theaterSelectionsEqual } from '~/utils/sharedTheaterSelection'
import {
  queryFormatOptions,
  queryLanguageOptions,
} from '~/utils/showtimeFilters'
import { theaterDisplayName } from '~/utils/theaterDisplayName'

interface SettingsQuery {
  mode?: string
  format?: string
  language?: string
  zoom?: string
  shared_theaters?: string
}

const props = defineProps<{
  mode: 'theater' | 'movie'
  format: QueryFormat
  language: Language
  zoom: 15 | 30 | 60
  theaterIds: readonly string[]
  favoriteTheaterIds: readonly string[]
  theaters: readonly Pick<Theater, 'id' | 'name' | 'city' | 'provider'>[]
  ready: boolean
  loading: boolean
  error: string | null
}>()
const emit = defineEmits<{
  apply: [values: SettingsQuery]
  retry: []
}>()
const dialog = ref<HTMLDialogElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const open = ref(false)
const theaterListOpen = ref(false)
const validationMessage = ref('')
const draftMode = ref(props.mode)
const draftFormat = ref(props.format)
const draftLanguage = ref(props.language)
const draftZoom = ref(props.zoom)
const draftTheaterIds = ref<string[]>([])
const draftTheaterSet = computed(() => new Set(draftTheaterIds.value))
// Keep unchecked favorites available when reopening a temporary selection.
const scopeTheaters = computed(() => {
  if (!props.favoriteTheaterIds.length) return props.theaters
  const ids = new Set([...props.favoriteTheaterIds, ...props.theaterIds])
  return props.theaters.filter((theater) => !ids.size || ids.has(theater.id))
})
const effectiveTheaterIds = computed(() =>
  props.theaterIds.length
    ? props.theaterIds
    : props.theaters.map((theater) => theater.id),
)
const theaterSummary = computed(() => {
  const count = draftTheaterIds.value.length
  return `${count} cinéma${count === 1 ? '' : 's'} inclus`
})
let bodyOverflow: string | null = null
let active = true

function resetDraft() {
  draftMode.value = props.mode
  draftFormat.value = props.format
  draftLanguage.value = props.language
  draftZoom.value = props.zoom
  draftTheaterIds.value = [...effectiveTheaterIds.value]
  validationMessage.value = ''
}

async function openModal() {
  if (open.value) return
  resetDraft()
  theaterListOpen.value = false
  open.value = true
  await nextTick()
  if (!active || !open.value || !dialog.value) return
  dialog.value.showModal()
  bodyOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  closeButton.value?.focus({ preventScroll: true })
}

function unlockScroll() {
  if (bodyOverflow === null) return
  document.body.style.overflow = bodyOverflow
  bodyOverflow = null
}

function closeModal() {
  if (!open.value) return
  dialog.value?.close()
  open.value = false
  unlockScroll()
  trigger.value?.focus({ preventScroll: true })
}

function backdrop(event: MouseEvent) {
  if (event.target !== dialog.value || !dialog.value) return
  const rect = dialog.value.getBoundingClientRect()
  if (
    event.clientX < rect.left ||
    event.clientX > rect.right ||
    event.clientY < rect.top ||
    event.clientY > rect.bottom
  )
    closeModal()
}

function keepFocusWithin(event: KeyboardEvent) {
  if (event.key !== 'Tab' || !dialog.value) return
  const elements = [
    ...dialog.value.querySelectorAll<HTMLElement>(
      'a[href], button, input, select',
    ),
  ].filter(
    (element) =>
      !element.hasAttribute('disabled') && element.getClientRects().length > 0,
  )
  const first = elements[0]
  const last = elements.at(-1)
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last?.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first?.focus()
  }
}

function toggleTheater(id: string) {
  const ids = new Set(draftTheaterIds.value)
  if (ids.has(id)) ids.delete(id)
  else ids.add(id)
  draftTheaterIds.value = props.theaters
    .filter((theater) => ids.has(theater.id))
    .map((theater) => theater.id)
  validationMessage.value = ''
}

function apply() {
  if (
    props.ready &&
    scopeTheaters.value.length &&
    !draftTheaterIds.value.length
  ) {
    validationMessage.value = 'Sélectionnez au moins un cinéma.'
    theaterListOpen.value = true
    return
  }
  const values: SettingsQuery = {
    mode: draftMode.value === 'theater' ? undefined : draftMode.value,
    format: draftFormat.value === 'ALL' ? undefined : draftFormat.value,
    language: draftLanguage.value === 'ALL' ? undefined : draftLanguage.value,
    zoom: draftZoom.value === 30 ? undefined : String(draftZoom.value),
  }
  if (
    props.ready &&
    !theaterSelectionsEqual(draftTheaterIds.value, effectiveTheaterIds.value)
  )
    values.shared_theaters = theaterSelectionsEqual(
      draftTheaterIds.value,
      props.favoriteTheaterIds.length
        ? props.favoriteTheaterIds
        : props.theaters.map((theater) => theater.id),
    )
      ? undefined
      : draftTheaterIds.value.join(',')
  emit('apply', values)
  closeModal()
}

watch(
  () => [
    props.mode,
    props.format,
    props.language,
    props.zoom,
    props.theaterIds,
    props.favoriteTheaterIds,
    props.theaters,
    props.ready,
  ],
  () => {
    if (open.value) resetDraft()
  },
)

onBeforeUnmount(() => {
  active = false
  dialog.value?.close()
  unlockScroll()
})
</script>

<template>
  <div class="ml-auto shrink-0 sm:ml-0">
    <button
      ref="trigger"
      type="button"
      class="inline-flex h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.7rem] font-mono text-[0.65rem] font-black tracking-[0.08em] text-surface uppercase hover:bg-primary focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent"
      aria-label="Modifier les paramètres du planning"
      aria-haspopup="dialog"
      aria-controls="planning-settings"
      :aria-expanded="open"
      @click="openModal"
    >
      <Settings2 :size="17" aria-hidden="true" />
      <span class="hidden sm:inline">Paramètres</span>
    </button>
    <Teleport to="body">
      <dialog
        v-if="open"
        id="planning-settings"
        ref="dialog"
        class="fixed inset-x-0 top-auto bottom-0 m-0 mx-auto flex max-h-[calc(100dvh-1rem)] w-full max-w-2xl flex-col overflow-hidden border-2 border-b-0 border-ink bg-[#f8f7f2] p-0 text-ink shadow-[0_-8px_0_#27272a] backdrop:bg-black/60"
        aria-labelledby="planning-settings-title"
        @cancel.prevent="closeModal"
        @close="closeModal"
        @click="backdrop"
        @keydown="keepFocusWithin"
      >
        <header
          class="flex shrink-0 items-center gap-2.5 border-b-2 border-ink p-4 sm:px-6"
        >
          <Settings2 :size="18" aria-hidden="true" />
          <h2
            id="planning-settings-title"
            class="text-xl font-black tracking-[-0.035em]"
          >
            Paramètres du planning
          </h2>
          <button
            ref="closeButton"
            type="button"
            class="ml-auto inline-flex size-11 shrink-0 items-center justify-center border-2 border-ink bg-surface hover:bg-[#e8e6de] focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ink"
            aria-label="Fermer les paramètres"
            @click="closeModal"
          >
            <X :size="20" aria-hidden="true" />
          </button>
        </header>
        <form class="flex min-h-0 flex-col" @submit.prevent="apply">
          <div
            class="min-h-0 space-y-5 overflow-y-auto overscroll-contain p-4 sm:px-6"
          >
            <fieldset
              :aria-invalid="validationMessage ? 'true' : undefined"
              :aria-describedby="validationMessage ? 'planning-theater-message' : undefined"
            >
              <legend
                class="float-left mb-2 font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]"
              >
                Cinémas
              </legend>
              <NuxtLink
                to="/cinemas"
                class="float-right mb-2 border-b-2 border-ink font-mono text-[10px] font-bold uppercase tracking-[0.08em] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
                @click="closeModal"
                >Gérer mes cinémas</NuxtLink
              >
              <div
                v-if="error && !ready"
                class="clear-both border-2 border-ink bg-surface p-3 text-sm text-primary"
                role="alert"
              >
                <p>{{ error }}</p>
                <button
                  type="button"
                  class="mt-2 min-h-11 font-bold underline underline-offset-4"
                  @click="emit('retry')"
                >
                  Réessayer
                </button>
              </div>
              <div
                v-else-if="loading || !ready"
                class="clear-both flex min-h-12 items-center gap-2 border-2 border-ink bg-surface px-3 text-sm text-muted"
              >
                <LoaderCircle
                  :size="16"
                  class="animate-spin motion-reduce:animate-none"
                  aria-hidden="true"
                />
                Chargement des cinémas…
              </div>
              <div v-else-if="scopeTheaters.length" class="clear-both">
                <button
                  type="button"
                  class="flex min-h-12 w-full items-center justify-between gap-3 border-2 border-ink bg-surface px-3 py-2.5 text-left text-sm font-bold hover:bg-[#e8e6de] focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
                  :aria-expanded="theaterListOpen"
                  aria-controls="planning-theater-list"
                  @click="theaterListOpen = !theaterListOpen"
                >
                  <span>{{ theaterSummary }}</span>
                  <ChevronDown
                    :size="18"
                    class="shrink-0 transition-transform motion-reduce:transition-none"
                    :class="theaterListOpen ? 'rotate-180' : ''"
                    aria-hidden="true"
                  />
                </button>
                <div
                  v-show="theaterListOpen"
                  id="planning-theater-list"
                  class="border-2 border-t-0 border-ink bg-surface"
                >
                  <label
                    v-for="theater in scopeTheaters"
                    :key="theater.id"
                    class="flex min-h-12 cursor-pointer items-center gap-3 border-b border-ink/25 px-3 py-2.5 text-sm last:border-b-0 hover:bg-[#e8e6de]"
                  >
                    <input
                      type="checkbox"
                      class="peer sr-only"
                      :checked="draftTheaterSet.has(theater.id)"
                      :aria-label="`Inclure ${theaterDisplayName(theater)}`"
                      @change="toggleTheater(theater.id)"
                    >
                    <span
                      class="grid size-5 shrink-0 place-items-center border-2 border-ink bg-surface peer-focus-visible:outline-3 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-ink peer-checked:bg-ink peer-checked:text-white"
                      aria-hidden="true"
                    >
                      <Check
                        v-if="draftTheaterSet.has(theater.id)"
                        :size="14"
                        stroke-width="3"
                      />
                    </span>
                    <TheaterName
                      :name="theaterDisplayName(theater)"
                      :provider="theater.provider"
                      class="min-w-0 font-bold leading-tight"
                    />
                  </label>
                </div>
              </div>
              <p
                v-else
                class="clear-both border-2 border-ink bg-surface px-3 py-3 text-sm"
              >
                Aucun cinéma disponible.
              </p>
              <p
                v-if="validationMessage"
                id="planning-theater-message"
                role="alert"
                class="mt-1.5 text-sm text-primary"
              >
                {{ validationMessage }}
              </p>
            </fieldset>
            <fieldset>
              <legend
                class="mb-1.5 font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]"
              >
                Affichage
              </legend>
              <div
                class="flex w-full border-2 border-ink bg-surface p-[0.2rem]"
              >
                <button
                  v-for="option in [{ value: 'theater', label: 'Par cinéma' }, { value: 'movie', label: 'Par film' }] as const"
                  :key="option.value"
                  type="button"
                  class="min-h-11 flex-1 px-[0.7rem] text-xs font-extrabold text-ink focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent"
                  :class="draftMode === option.value ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : 'hover:bg-[#e8e6de]'"
                  :aria-pressed="draftMode === option.value"
                  @click="draftMode = option.value"
                >
                  {{ option.label }}
                </button>
              </div>
            </fieldset>

            <fieldset class="min-w-0">
              <legend
                class="mb-1.5 font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]"
              >
                Format
              </legend>
              <div
                class="flex w-full flex-wrap border-2 border-ink bg-surface p-[0.2rem] [scrollbar-width:thin]"
              >
                <button
                  v-for="option in queryFormatOptions"
                  :key="option.value"
                  type="button"
                  class="min-h-11 shrink-0 px-[0.7rem] text-xs font-extrabold text-ink focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent"
                  :class="draftFormat === option.value ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : 'hover:bg-[#e8e6de]'"
                  :aria-label="option.label"
                  :aria-pressed="draftFormat === option.value"
                  @click="draftFormat = option.value"
                >
                  <BrandLogo
                    v-if="option.brand"
                    :brand="option.brand"
                    decorative
                    :class="draftFormat === option.value ? 'brightness-0 invert' : ''"
                  />
                  <span v-else>{{ option.label }}</span>
                </button>
              </div>
            </fieldset>

            <label class="block">
              <span
                class="mb-1.5 block font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]"
                >Langue</span
              >
              <select
                v-model="draftLanguage"
                class="h-12 w-full min-w-0 rounded-none border-2 border-ink bg-surface pr-8 pl-[0.7rem] text-[0.8rem] font-extrabold text-ink focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent"
              >
                <option
                  v-for="option in queryLanguageOptions"
                  :key="option.value"
                  :value="option.value"
                >
                  {{ option.label }}
                </option>
              </select>
            </label>

            <fieldset>
              <legend
                class="mb-1.5 font-mono text-[0.62rem] font-black uppercase tracking-[0.14em]"
              >
                Zoom
              </legend>
              <div
                class="flex w-full border-2 border-ink bg-surface p-[0.2rem]"
              >
                <button
                  v-for="option in [{ value: 15, label: '15 min' }, { value: 30, label: '30 min' }, { value: 60, label: '1 h' }] as const"
                  :key="option.value"
                  type="button"
                  class="min-h-11 flex-1 px-[0.7rem] text-xs font-extrabold text-ink focus-visible:z-[1] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent"
                  :class="draftZoom === option.value ? 'bg-ink text-surface shadow-[inset_0_-3px_0_var(--color-highlight)]' : 'hover:bg-[#e8e6de]'"
                  :aria-pressed="draftZoom === option.value"
                  @click="draftZoom = option.value"
                >
                  {{ option.label }}
                </button>
              </div>
            </fieldset>
          </div>
          <footer
            class="shrink-0 border-t-2 border-ink px-4 pt-4 pb-[max(1rem,env(safe-area-inset-bottom))] sm:px-6"
          >
            <button
              type="submit"
              class="min-h-12 w-full border-2 border-ink bg-ink px-4 py-3 text-sm font-black text-surface hover:bg-primary focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ink"
            >
              Afficher le planning
            </button>
          </footer>
        </form>
      </dialog>
    </Teleport>
  </div>
</template>
