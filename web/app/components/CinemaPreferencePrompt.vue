<script setup lang="ts">
import { ArrowRight, Clapperboard, X } from '@lucide/vue'
import { isAccountPage } from '~~/shared/accountPrivacy'

const STORAGE_KEY = 'messeances.cinemaSelectionPromptDismissed.v1'
const route = useRoute()
const { $appUpdate } = useNuxtApp()
const preferences = useCinemaPreferences()
const dismissalReady = ref(false)
const dismissed = ref(false)
const visible = computed(
  () =>
    dismissalReady.value &&
    !dismissed.value &&
    preferences.hasSavedTheaterSelection.value === false &&
    !preferences.error.value &&
    !preferences.writesBlocked.value &&
    !preferences.syncError.value &&
    route.path !== '/cinemas' &&
    !isAccountPage(route.path) &&
    !/^\/admin(?:\/|$)/i.test(route.path) &&
    !$appUpdate.state.available,
)

const popup = ref<HTMLElement | null>(null)
const dragX = ref(0)
const dragY = ref(0)
let gesture: { id: number; x: number; y: number } | null = null
let suppressClick = false

function resetGesture() {
  const id = gesture?.id
  gesture = null
  dragX.value = 0
  dragY.value = 0
  if (id !== undefined && popup.value?.hasPointerCapture(id)) {
    popup.value.releasePointerCapture(id)
  }
}

watch(visible, resetGesture, { flush: 'sync' })

function onPointerDown(event: PointerEvent) {
  if (!event.isPrimary) {
    if (gesture) suppressClick = true
    resetGesture()
    return
  }
  suppressClick = false
  resetGesture()
  if (
    !visible.value ||
    event.pointerType !== 'touch' ||
    !window.matchMedia('(any-pointer: coarse)').matches ||
    (event.target as Element).closest('button')
  ) {
    return
  }
  gesture = { id: event.pointerId, x: event.clientX, y: event.clientY }
}

function onPointerMove(event: PointerEvent) {
  if (!gesture || event.pointerId !== gesture.id) return
  const x = event.clientX - gesture.x
  const y = event.clientY - gesture.y
  if (Math.hypot(x, y) > 10) {
    suppressClick = true
    popup.value?.setPointerCapture(event.pointerId)
  }
  // Upward or ambiguous diagonal drags do not move the prompt.
  const horizontal = Math.abs(x) >= Math.abs(y) * 1.25
  const downward = y >= Math.abs(x) * 1.25
  dragX.value = horizontal ? x : 0
  dragY.value = downward ? y : 0
}

function onPointerUp(event: PointerEvent) {
  if (!gesture || event.pointerId !== gesture.id) return
  onPointerMove(event)
  // Less travel is available below a bottom-anchored prompt.
  const shouldDismiss = Math.abs(dragX.value) >= 64 || dragY.value >= 40
  resetGesture()
  if (shouldDismiss) dismiss()
}

function onPointerCancel(event: PointerEvent) {
  if (!gesture || event.pointerId !== gesture.id) return
  suppressClick = true
  resetGesture()
}

function onLostPointerCapture(event: PointerEvent) {
  // Implicit touch capture on the CTA is transferred to the popup during a drag.
  if (event.target === popup.value) onPointerCancel(event)
}

function onClick(event: MouseEvent) {
  // Capture runs before NuxtLink navigation, including clicks after a short drag.
  if (!suppressClick || event.detail === 0) return
  event.preventDefault()
  event.stopPropagation()
  suppressClick = false
}

onBeforeUnmount(resetGesture)

onMounted(() => {
  try {
    dismissed.value = localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    // The app-local flag still prevents repeats when storage is unavailable.
  }
  dismissalReady.value = true
})

function dismiss() {
  dismissed.value = true
  try {
    localStorage.setItem(STORAGE_KEY, '1')
  } catch {
    // Browser storage is optional; no account preference is changed.
  }
}
</script>

<template>
  <div
    v-if="visible"
    class="pointer-events-none fixed inset-x-0 bottom-[calc(1rem+env(safe-area-inset-bottom))] z-40 flex justify-center px-4"
    @keydown.esc.stop="dismiss"
  >
    <div
      ref="popup"
      class="group pointer-events-auto relative flex max-w-full translate-x-[var(--prompt-x)] translate-y-[var(--prompt-y)] items-center gap-3 rounded-[3px] border-2 border-ink bg-[#f8f7f2] px-3 py-2 text-ink shadow-[2px_2px_0_#27272a] motion-reduce:translate-none [@media(any-pointer:coarse)]:touch-none"
      :style="{ '--prompt-x': `${dragX}px`, '--prompt-y': `${dragY}px` }"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerCancel"
      @lostpointercapture="onLostPointerCapture"
      @click.capture="onClick"
    >
      <p
        role="status"
        aria-live="polite"
        class="min-w-0 flex-1 text-sm leading-5 font-bold"
      >
        <Clapperboard
          :size="16"
          class="mr-2 inline-block align-text-bottom"
          aria-hidden="true"
        />
        Choisissez vos cinémas
      </p>
      <NuxtLink
        to="/cinemas"
        class="inline-flex min-h-11 shrink-0 items-center justify-center gap-2 rounded-[3px] bg-[#ffcf3f] px-2 text-sm font-bold text-ink hover:bg-[#ffcf3f]/80 focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
      >
        Choisir
        <ArrowRight :size="14" aria-hidden="true" />
      </NuxtLink>
      <button
        type="button"
        aria-label="Ne plus afficher"
        class="pointer-events-none absolute -top-11 right-0 grid size-11 place-items-end rounded-[3px] pr-1 pb-1 text-muted opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-has-[:focus-visible]:pointer-events-auto group-has-[:focus-visible]:opacity-100 hover:text-ink focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
        @click="dismiss"
      >
        <X :size="16" aria-hidden="true" />
      </button>
    </div>
  </div>
</template>
