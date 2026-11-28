<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    term: string
    definition: string
    // Disable keyboard focus when rendered inside a link or button.
    focusable?: boolean
  }>(),
  { focusable: true },
)

const tooltipId = useId()
const trigger = ref<HTMLElement | null>(null)
const tooltip = ref<HTMLElement | null>(null)
const hovered = ref(false)
const focused = ref(false)
const dismissed = ref(false)
const position = ref({ left: 0, top: 0 })
const open = computed(
  () => (hovered.value || focused.value) && !dismissed.value,
)

const GAP = 6
const MARGIN = 8

function place() {
  const anchor = trigger.value?.getBoundingClientRect()
  const bubble = tooltip.value?.getBoundingClientRect()
  if (!anchor || !bubble) return
  const left = Math.max(
    MARGIN,
    Math.min(
      anchor.left + anchor.width / 2 - bubble.width / 2,
      window.innerWidth - bubble.width - MARGIN,
    ),
  )
  const below = anchor.bottom + GAP
  const top =
    below + bubble.height > window.innerHeight - MARGIN
      ? Math.max(MARGIN, anchor.top - bubble.height - GAP)
      : below
  position.value = { left, top }
}

function dismiss(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  event.preventDefault()
  event.stopPropagation()
  dismissed.value = true
}

// The tooltip lives in the top layer so overflow, stacking and modal dialogs
// never clip it; listeners exist only while it is shown.
watch(open, async (visible) => {
  if (!visible) {
    window.removeEventListener('keydown', dismiss, true)
    window.removeEventListener('scroll', place, true)
    window.removeEventListener('resize', place)
    return
  }
  await nextTick()
  const element = tooltip.value
  if (!element || !('showPopover' in element)) return
  if (!element.matches(':popover-open')) element.showPopover()
  place()
  window.addEventListener('keydown', dismiss, true)
  window.addEventListener('scroll', place, { capture: true, passive: true })
  window.addEventListener('resize', place, { passive: true })
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', dismiss, true)
  window.removeEventListener('scroll', place, true)
  window.removeEventListener('resize', place)
})

function enter(event: PointerEvent) {
  if (event.pointerType === 'touch') return
  hovered.value = true
  dismissed.value = false
}

function focus() {
  focused.value = true
  dismissed.value = false
}
</script>

<template>
  <span
    ref="trigger"
    class="cursor-help underline decoration-dotted decoration-from-font underline-offset-[0.2em] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-current"
    :tabindex="props.focusable ? 0 : undefined"
    :aria-describedby="props.focusable ? tooltipId : undefined"
    @pointerenter="enter"
    @pointerleave="hovered = false"
    @focus="focus"
    @blur="focused = false"
    >{{ term
    }}<span
      v-if="open || props.focusable"
      v-show="open"
      :id="tooltipId"
      ref="tooltip"
      popover="manual"
      role="tooltip"
      class="pointer-events-none fixed m-0 max-w-[min(18rem,calc(100vw-1rem))] border-2 border-ink bg-ink px-2.5 py-1.5 text-left font-sans text-xs leading-snug font-medium tracking-normal text-white normal-case no-underline shadow-[3px_3px_0_var(--color-highlight)]"
      :style="{ inset: 'auto', left: `${position.left}px`, top: `${position.top}px` }"
      ><strong class="mr-1 font-mono font-black">{{ term }}</strong>
      {{ definition }}</span
    ></span
  >
</template>
