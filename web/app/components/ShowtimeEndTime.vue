<script setup lang="ts">
import type { ResolvedShowtimeEnd } from '~/utils/showtimeEnd'
import { formatParisTime } from '~/utils/date'

const props = defineProps<{
  end: ResolvedShowtimeEnd
  advertisedStart: string
  runtimeMinutes: number
}>()

const tooltipId = useId()
const trigger = ref<HTMLElement | null>(null)
const hovered = ref(false)
const focused = ref(false)
const dismissed = ref(false)
const tooltipLeft = ref(0)
const tooltipWidth = ref(240)
let hoverCloseTimer: ReturnType<typeof setTimeout> | undefined
const tooltipVisible = computed(() => (hovered.value || focused.value) && !dismissed.value)
const explanation = computed(() => `Fin estimée : début annoncé à ${formatParisTime(props.advertisedStart)} + ${props.end.adsMinutes} min de publicités + ${props.runtimeMinutes} min de film.`)

function openTooltip(interaction: 'hover' | 'focus') {
  if (interaction === 'hover') {
    clearTimeout(hoverCloseTimer)
    hovered.value = true
  } else focused.value = true
  dismissed.value = false
  const bounds = trigger.value?.getBoundingClientRect()
  if (!bounds) return
  tooltipWidth.value = Math.min(240, window.innerWidth - 32)
  // Keep this local absolute tooltip inside the viewport, including narrow cards.
  const left = Math.max(16, Math.min(bounds.left + bounds.width / 2 - tooltipWidth.value / 2, window.innerWidth - tooltipWidth.value - 16))
  tooltipLeft.value = left - bounds.left
}

function leaveTooltip() {
  // Allow crossing the small diagonal gap when a viewport edge shifts the tooltip.
  hoverCloseTimer = setTimeout(() => { hovered.value = false }, 100)
}

function dismissTooltip(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !tooltipVisible.value) return
  event.preventDefault()
  event.stopPropagation()
  dismissed.value = true
}

// Capture Escape even for mouse-only hover, before the timeline inspector handles it.
onMounted(() => window.addEventListener('keydown', dismissTooltip, true))
onBeforeUnmount(() => {
  clearTimeout(hoverCloseTimer)
  window.removeEventListener('keydown', dismissTooltip, true)
})
</script>

<template>
  <span v-if="!end.estimated">{{ formatParisTime(end.time) }}</span>
  <span
    v-else
    class="relative inline-block"
    :class="tooltipVisible ? 'z-30' : 'z-20'"
    @mouseenter="openTooltip('hover')"
    @mouseleave="leaveTooltip"
    @click.stop
    @pointerdown.stop
  >
    <span
      ref="trigger"
      tabindex="0"
      :aria-describedby="tooltipId"
      class="cursor-help underline decoration-dotted underline-offset-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
      @focus="openTooltip('focus')"
      @blur="focused = false"
    ><span class="sr-only">Fin estimée : </span>{{ formatParisTime(end.time) }}</span>
    <span
      :id="tooltipId"
      role="tooltip"
      class="absolute top-full block pt-2 text-center font-sans text-xs font-normal normal-case tracking-normal"
      :class="tooltipVisible ? 'visible' : 'invisible'"
      :style="{ left: `${tooltipLeft}px`, width: `${tooltipWidth}px` }"
    ><span class="block border border-ink bg-ink px-2 py-1 text-white shadow-sm">{{ explanation }}</span></span>
  </span>
</template>
