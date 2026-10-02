<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  useTemplateRef,
  watch,
} from 'vue'
import { Check, ChevronDown } from '@lucide/vue'
import type { Provider } from '~/types/api'
import { toggleCinemaChain } from '~/utils/cinemaDirectoryChains'
import { THEATER_PROVIDER_LABELS } from '~/utils/theaterMap'

const props = defineProps<{
  modelValue: Provider[]
  options: readonly Provider[]
  inSettings?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: Provider[]]
}>()
const disclosure = useTemplateRef<HTMLElement>('disclosure')
const summary = useTemplateRef<HTMLElement>('summary')
const panel = useTemplateRef<HTMLFieldSetElement>('panel')
const optionScroll = useTemplateRef<HTMLElement>('optionScroll')
const panelStyle = ref<{ left: string; top: string; maxHeight: string }>()
const overflowRemaining = ref(false)
let frame: number | null = null
let desktopMediaQuery: MediaQueryList | null = null
const selected = computed(() => new Set(props.modelValue))
const summaryText = computed(() => {
  const first = props.modelValue[0]
  if (!first) return 'Toutes les enseignes'
  return props.modelValue.length === 1
    ? THEATER_PROVIDER_LABELS[first]
    : `${props.modelValue.length} enseignes`
})

function close(event: KeyboardEvent) {
  if (!isOpen()) return
  event.preventDefault()
  event.stopPropagation()
  resetDisclosure()
  summary.value?.focus({ preventScroll: true })
}

function isOpen() {
  return disclosure.value instanceof HTMLDetailsElement && disclosure.value.open
}

function resetDisclosure() {
  if (disclosure.value instanceof HTMLDetailsElement)
    disclosure.value.open = false
  panelStyle.value = undefined
  overflowRemaining.value = false
  if (frame !== null) cancelAnimationFrame(frame)
  frame = null
}

function updateOverflow() {
  const scroll = optionScroll.value
  overflowRemaining.value =
    !props.inSettings &&
    !!scroll &&
    scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight > 1
}

function positionPanel() {
  if (!isOpen() || !summary.value || !panel.value || !optionScroll.value) return
  const anchor = summary.value.getBoundingClientRect()
  if (anchor.bottom <= 0 || anchor.top >= window.innerHeight) {
    resetDisclosure()
    return
  }
  const bounds = panel.value.getBoundingClientRect()
  const scroll = optionScroll.value
  const naturalHeight =
    bounds.height + scroll.scrollHeight - scroll.clientHeight
  const desiredHeight = Math.min(440, naturalHeight)
  const below = Math.max(0, window.innerHeight - anchor.bottom - 16)
  const above = Math.max(0, anchor.top - 16)
  const placeAbove = below < desiredHeight && above > below
  const height = Math.min(desiredHeight, placeAbove ? above : below)
  panelStyle.value = {
    left: `${Math.max(8, Math.min(anchor.right - bounds.width, window.innerWidth - bounds.width - 8))}px`,
    top: `${placeAbove ? Math.max(8, anchor.top - height - 8) : anchor.bottom + 8}px`,
    maxHeight: `${height}px`,
  }
  nextTick(updateOverflow)
}

function schedulePosition() {
  if (!isOpen() || frame !== null) return
  frame = requestAnimationFrame(() => {
    frame = null
    positionPanel()
  })
}

function handleToggle() {
  if (isOpen()) nextTick(positionPanel)
  else resetDisclosure()
}

function closeOutside(event: PointerEvent) {
  if (
    isOpen() &&
    event.target instanceof Node &&
    !disclosure.value?.contains(event.target)
  )
    resetDisclosure()
}

function closeOnFocusLeave(event: FocusEvent) {
  if (
    isOpen() &&
    event.relatedTarget instanceof Node &&
    !disclosure.value?.contains(event.relatedTarget)
  )
    resetDisclosure()
}

// Teleporting into/out of the sheet or crossing the toolbar breakpoint closes
// the disclosure before its previous open state can reappear in another view.
watch(() => props.inSettings, resetDisclosure, { flush: 'sync' })
onMounted(() => {
  document.addEventListener('pointerdown', closeOutside)
  document.addEventListener('scroll', schedulePosition, true)
  window.addEventListener('resize', schedulePosition)
  desktopMediaQuery = window.matchMedia('(min-width: 1024px)')
  desktopMediaQuery.addEventListener('change', resetDisclosure)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', closeOutside)
  document.removeEventListener('scroll', schedulePosition, true)
  window.removeEventListener('resize', schedulePosition)
  desktopMediaQuery?.removeEventListener('change', resetDisclosure)
  resetDisclosure()
})
</script>

<template>
  <component
    :is="inSettings ? 'section' : 'details'"
    ref="disclosure"
    class="group relative min-w-0"
    :class="inSettings ? 'w-full' : ''"
    @keydown.esc="close"
    @focusout="closeOnFocusLeave"
    @toggle="handleToggle"
  >
    <summary
      v-if="!inSettings"
      ref="summary"
      class="flex min-h-11 max-w-full cursor-pointer list-none items-center justify-between gap-3 border-2 border-ink bg-surface px-3 py-2 text-sm font-bold focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink [&::-webkit-details-marker]:hidden"
      :class="modelValue.length > 0 ? 'bg-[#fff3c4]' : ''"
    >
      <span class="min-w-0 [overflow-wrap:anywhere]">{{ summaryText }}</span>
      <ChevronDown
        :size="18"
        class="shrink-0 group-open:rotate-180"
        aria-hidden="true"
      />
    </summary>
    <fieldset
      ref="panel"
      class="min-w-0"
      :class="inSettings ? 'w-full' : 'fixed z-30 flex max-h-[440px] w-[350px] max-w-[calc(100vw-1rem)] flex-col border-2 border-ink bg-surface p-3 shadow-[4px_4px_0_#27272a]'"
      :style="inSettings ? undefined : panelStyle"
    >
      <legend class="sr-only">Filtrer par enseigne</legend>
      <div class="mb-2 flex shrink-0 items-center justify-between gap-2">
        <h3 class="text-base font-black">Enseignes</h3>
        <button
          type="button"
          class="min-h-11 px-2 text-sm font-bold underline underline-offset-4 focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ink disabled:opacity-40"
          :disabled="modelValue.length === 0"
          @click="emit('update:modelValue', [])"
        >
          Réinitialiser
        </button>
      </div>
      <div class="relative min-h-0" :class="inSettings ? '' : 'flex-1'">
        <div
          ref="optionScroll"
          class="grid grid-cols-2 gap-2"
          :class="inSettings ? '' : 'max-h-full overflow-y-auto overscroll-contain p-1'"
          @scroll="updateOverflow"
        >
          <label
            v-for="provider in options"
            :key="provider"
            class="relative flex min-h-16 min-w-0 cursor-pointer items-center gap-2 border-2 border-ink p-2 text-xs font-bold hover:bg-[#fff3c4] has-focus-visible:outline-3 has-focus-visible:outline-offset-1 has-focus-visible:outline-ink"
            :class="selected.has(provider) ? 'bg-[#fff3c4] shadow-[inset_0_0_0_1px_var(--color-ink)]' : 'bg-surface'"
          >
            <input
              type="checkbox"
              :value="provider"
              :checked="selected.has(provider)"
              class="peer sr-only"
              @change="emit('update:modelValue', toggleCinemaChain(modelValue, provider))"
            >
            <span
              class="flex size-5 shrink-0 items-center justify-center border-2 border-ink peer-checked:bg-ink peer-checked:text-white"
              aria-hidden="true"
            >
              <Check
                v-if="selected.has(provider)"
                :size="16"
                :stroke-width="3"
              />
            </span>
            <TheaterName
              :name="THEATER_PROVIDER_LABELS[provider]"
              :provider="provider"
              class="flex min-w-0 flex-1 flex-col items-center gap-1 text-center leading-4 [overflow-wrap:anywhere]"
              logo-class="!h-6 !w-12 !max-w-full object-contain"
            />
          </label>
        </div>
        <div
          v-if="overflowRemaining"
          class="pointer-events-none absolute inset-x-1 bottom-0 flex h-7 items-end justify-center bg-linear-to-t from-surface to-transparent"
          aria-hidden="true"
        >
          <ChevronDown :size="18" class="bg-surface" />
        </div>
      </div>
    </fieldset>
  </component>
</template>
