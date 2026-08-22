<script setup lang="ts">
import { Check, ChevronDown } from '@lucide/vue'

interface SettingOption {
  value: string
  label: string
}

const props = defineProps<{
  id: string
  label: string
  currentValue: string
  options: [SettingOption, SettingOption]
}>()

const emit = defineEmits<{
  select: [value: string]
}>()

const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const optionButtons = ref<HTMLButtonElement[]>([])
const isOpen = ref(false)
const menuId = computed(() => `${props.id}-menu`)
const currentLabel = computed(() => props.options.find((option) => option.value === props.currentValue)?.label ?? '')

function setOptionButton(element: Element | null, index: number) {
  if (element instanceof HTMLButtonElement) optionButtons.value[index] = element
}

async function openMenu() {
  isOpen.value = true
  await nextTick()
  const checkedIndex = Math.max(0, props.options.findIndex((option) => option.value === props.currentValue))
  optionButtons.value[checkedIndex]?.focus()
}

function closeMenu({ restoreFocus = false } = {}) {
  if (!isOpen.value) return
  isOpen.value = false
  if (restoreFocus) nextTick(() => trigger.value?.focus())
}

function toggleMenu() {
  if (isOpen.value) closeMenu()
  else void openMenu()
}

function handleTriggerKeydown(event: KeyboardEvent) {
  if (!['ArrowDown', 'ArrowUp'].includes(event.key)) return
  event.preventDefault()
  if (!isOpen.value) void openMenu()
}

function focusedOptionIndex(): number {
  return optionButtons.value.findIndex((button) => button === document.activeElement)
}

function handleMenuKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    closeMenu({ restoreFocus: true })
    return
  }
  if (event.key === 'Tab') {
    closeMenu()
    return
  }

  const currentIndex = focusedOptionIndex()
  let nextIndex: number | null = null
  if (event.key === 'ArrowDown') nextIndex = currentIndex >= props.options.length - 1 ? 0 : currentIndex + 1
  else if (event.key === 'ArrowUp') nextIndex = currentIndex <= 0 ? props.options.length - 1 : currentIndex - 1
  else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = props.options.length - 1
  if (nextIndex === null) return
  event.preventDefault()
  optionButtons.value[nextIndex]?.focus()
}

function selectOption(value: string) {
  closeMenu({ restoreFocus: true })
  emit('select', value)
}

function handleDocumentPointerDown(event: PointerEvent) {
  if (isOpen.value && event.target instanceof Node && !root.value?.contains(event.target)) closeMenu()
}

function handleFocusOut(event: FocusEvent) {
  if (event.relatedTarget instanceof Node && root.value?.contains(event.relatedTarget)) return
  closeMenu()
}

onMounted(() => document.addEventListener('pointerdown', handleDocumentPointerDown))
onBeforeUnmount(() => document.removeEventListener('pointerdown', handleDocumentPointerDown))
</script>

<template>
  <div ref="root" class="relative min-w-0" @focusout="handleFocusOut">
    <button
      :id="id"
      ref="trigger"
      type="button"
      class="flex min-h-11 w-full min-w-0 items-center justify-between gap-1.5 bg-surface px-2.5 text-left text-ink hover:bg-[#e8e6de] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ink sm:px-3"
      aria-haspopup="menu"
      :aria-controls="menuId"
      :aria-expanded="isOpen"
      @click="toggleMenu"
      @keydown="handleTriggerKeydown"
    >
      <span class="flex min-w-0 flex-col font-mono font-black uppercase leading-none">
        <span class="text-[9px] tracking-[0.08em] text-muted">{{ label }}</span>
        <span class="mt-1 truncate text-[10px]">{{ currentLabel }}</span>
      </span>
      <ChevronDown :size="14" class="shrink-0 transition-transform" :class="isOpen ? 'rotate-180' : undefined" aria-hidden="true" />
    </button>

    <div
      v-if="isOpen"
      :id="menuId"
      role="menu"
      class="absolute right-0 top-full z-40 mt-1 w-44 max-w-[calc(100vw-2rem)] border-2 border-ink bg-surface p-1 shadow-[4px_4px_0_#27272a]"
      :aria-label="label"
      @keydown="handleMenuKeydown"
    >
      <button
        v-for="(option, index) in options"
        :key="option.value"
        :ref="(element) => setOptionButton(element as Element | null, index)"
        type="button"
        role="menuitemradio"
        :aria-checked="currentValue === option.value"
        class="flex min-h-11 w-full items-center justify-between gap-3 px-3 text-left text-sm font-bold text-ink hover:bg-[#e8e6de] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ink"
        @click="selectOption(option.value)"
      >
        <span>{{ option.label }}</span>
        <Check v-if="currentValue === option.value" :size="16" class="shrink-0" aria-hidden="true" />
      </button>
    </div>
  </div>
</template>
