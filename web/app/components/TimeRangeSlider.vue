<script setup lang="ts">
interface TimeOption {
  value: string
  label: string
}

type Handle = 'start' | 'end'

const props = defineProps<{
  start: string
  end: string
  options: readonly TimeOption[]
}>()

const emit = defineEmits<{
  'update:start': [value: string]
  'update:end': [value: string]
}>()

const track = ref<HTMLElement | null>(null)
const startHandle = ref<HTMLButtonElement | null>(null)
const endHandle = ref<HTMLButtonElement | null>(null)
const activeHandle = ref<Handle>('start')
const activePointerId = ref<number | null>(null)

const lastIndex = computed(() => Math.max(props.options.length - 1, 0))
const startIndex = computed(() => Math.max(props.options.findIndex((option) => option.value === props.start), 0))
const endIndex = computed(() => {
  const index = props.options.findIndex((option) => option.value === props.end)
  return index === -1 ? lastIndex.value : index
})
const startPercent = computed(() => lastIndex.value ? startIndex.value / lastIndex.value * 100 : 0)
const endPercent = computed(() => lastIndex.value ? endIndex.value / lastIndex.value * 100 : 100)
const selectedWidth = computed(() => Math.max(endPercent.value - startPercent.value, 0))
const startValueText = computed(() => props.options[startIndex.value]?.label ?? props.start)
const endValueText = computed(() => props.options[endIndex.value]?.label ?? props.end)
const startModel = computed({
  get: () => props.start,
  set: (value: string) => {
    const index = props.options.findIndex((option) => option.value === value)
    if (index !== -1) setStart(index)
  }
})
const endModel = computed({
  get: () => props.end,
  set: (value: string) => {
    const index = props.options.findIndex((option) => option.value === value)
    if (index !== -1) setEnd(index)
  }
})

function emitOption(handle: Handle, index: number) {
  const option = props.options[index]
  if (!option) return
  if (handle === 'start') emit('update:start', option.value)
  else emit('update:end', option.value)
}

function setStart(index: number) {
  emitOption('start', Math.min(Math.max(index, 0), endIndex.value - 1))
}

function setEnd(index: number) {
  emitOption('end', Math.max(Math.min(index, lastIndex.value), startIndex.value + 1))
}

function setHandle(handle: Handle, index: number) {
  if (handle === 'start') setStart(index)
  else setEnd(index)
}

function indexFromPointer(event: PointerEvent) {
  const bounds = track.value?.getBoundingClientRect()
  if (!bounds || bounds.width === 0) return 0
  const ratio = Math.min(Math.max((event.clientX - bounds.left) / bounds.width, 0), 1)
  return Math.round(ratio * lastIndex.value)
}

function onPointerDown(event: PointerEvent) {
  if (!props.options.length) return
  const index = indexFromPointer(event)
  const startDistance = Math.abs(index - startIndex.value)
  const endDistance = Math.abs(index - endIndex.value)
  activeHandle.value = startDistance === endDistance ? activeHandle.value : startDistance < endDistance ? 'start' : 'end'
  activePointerId.value = event.pointerId
  // SAFETY: currentTarget is the slider element that registered this pointer handler.
  const slider = event.currentTarget as HTMLElement
  slider.setPointerCapture(event.pointerId)
  ;(activeHandle.value === 'start' ? startHandle.value : endHandle.value)?.focus({ preventScroll: true })
  setHandle(activeHandle.value, index)
}

function onPointerMove(event: PointerEvent) {
  if (event.pointerId !== activePointerId.value) return
  setHandle(activeHandle.value, indexFromPointer(event))
}

function onPointerEnd(event: PointerEvent) {
  if (event.pointerId !== activePointerId.value) return
  activePointerId.value = null
  // SAFETY: currentTarget is the slider element that registered this pointer handler.
  const target = event.currentTarget as HTMLElement
  if (target.hasPointerCapture(event.pointerId)) target.releasePointerCapture(event.pointerId)
}

function onKeydown(handle: Handle, event: KeyboardEvent) {
  const currentIndex = handle === 'start' ? startIndex.value : endIndex.value
  let nextIndex: number | undefined

  if (event.key === 'ArrowLeft' || event.key === 'ArrowDown') nextIndex = currentIndex - 1
  else if (event.key === 'ArrowRight' || event.key === 'ArrowUp') nextIndex = currentIndex + 1
  else if (event.key === 'PageDown') nextIndex = currentIndex - 4
  else if (event.key === 'PageUp') nextIndex = currentIndex + 4
  else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = lastIndex.value

  if (nextIndex === undefined) return
  event.preventDefault()
  activeHandle.value = handle
  setHandle(handle, nextIndex)
}

</script>

<template>
  <fieldset>
    <legend class="mb-1.5 text-sm font-medium text-ink">Créneau horaire</legend>

    <div
      class="relative h-11 touch-none select-none"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerEnd"
      @pointercancel="onPointerEnd"
    >
      <div ref="track" class="absolute inset-x-[11px] inset-y-0">
        <div class="absolute inset-x-0 top-1/2 h-1 -translate-y-1/2 rounded-full bg-line" aria-hidden="true" />
        <div
          class="absolute top-1/2 h-1 -translate-y-1/2 rounded-full bg-accent"
          :style="{ left: `${startPercent}%`, width: `${selectedWidth}%` }"
          aria-hidden="true"
        />

        <button
          ref="startHandle"
          type="button"
          role="slider"
          aria-label="À partir de"
          :aria-valuemin="0"
          :aria-valuemax="Math.max(endIndex - 1, 0)"
          :aria-valuenow="startIndex"
          :aria-valuetext="startValueText"
          class="pointer-events-none absolute top-0 z-10 flex size-11 -translate-x-1/2 items-center justify-center rounded-full focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2"
          :style="{ left: `${startPercent}%` }"
          @focus="activeHandle = 'start'"
          @keydown="onKeydown('start', $event)"
        >
          <span class="size-[22px] rounded-full border-2 border-accent bg-surface shadow-sm" aria-hidden="true" />
        </button>

        <button
          ref="endHandle"
          type="button"
          role="slider"
          aria-label="Terminé avant"
          :aria-valuemin="Math.min(startIndex + 1, lastIndex)"
          :aria-valuemax="lastIndex"
          :aria-valuenow="endIndex"
          :aria-valuetext="endValueText"
          class="pointer-events-none absolute top-0 z-10 flex size-11 -translate-x-1/2 items-center justify-center rounded-full focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2"
          :style="{ left: `${endPercent}%` }"
          @focus="activeHandle = 'end'"
          @keydown="onKeydown('end', $event)"
        >
          <span class="size-[22px] rounded-full border-2 border-accent bg-surface shadow-sm" aria-hidden="true" />
        </button>
      </div>
    </div>

    <div class="mt-1 grid grid-cols-2 gap-3">
      <label class="block text-sm font-medium text-ink">
        <span class="mb-1.5 block">À partir de</span>
        <select v-model="startModel" class="field">
          <option v-for="option in options" :key="`start-${option.value}`" :value="option.value">{{ option.label }}</option>
        </select>
      </label>
      <label class="block text-sm font-medium text-ink">
        <span class="mb-1.5 block">Terminé avant</span>
        <select v-model="endModel" class="field">
          <option v-for="option in options" :key="`end-${option.value}`" :value="option.value">{{ option.label }}</option>
        </select>
      </label>
    </div>
  </fieldset>
</template>
