<script setup lang="ts">
import type { Component } from 'vue'
import { loadVueDatePicker } from '~/utils/vueDatePickerLoader'

defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<{
  active?: boolean
}>(), {
  active: false
})

interface DatePickerExposed {
  openMenu: () => void
  inputRef: () => { $el?: Element } | undefined
}

const datePickerComponent = shallowRef<Component>()
const datePicker = ref<DatePickerExposed | null>(null)
let loadPromise: Promise<void> | null = null
let openAfterLoad = false

async function ensureLoaded() {
  if (datePickerComponent.value) return
  loadPromise ??= loadVueDatePicker()
    .then((component) => {
      datePickerComponent.value = component
    })
    .finally(() => {
      loadPromise = null
    })
  await loadPromise
}

async function loadAndOpen() {
  openAfterLoad = true
  try {
    await ensureLoaded()
    await nextTick()
    if (!openAfterLoad) return
    openAfterLoad = false
    const inputRoot = datePicker.value?.inputRef()?.$el
    if (inputRoot instanceof HTMLElement) inputRoot.querySelector<HTMLElement>('button')?.focus({ preventScroll: true })
    datePicker.value?.openMenu()
  } catch {
    openAfterLoad = false
  }
}

function activate() {
  if (datePickerComponent.value) return
  void loadAndOpen()
}

function loadWhenActive() {
  void ensureLoaded().catch(() => undefined)
}

onMounted(() => {
  if (props.active) loadWhenActive()
})

watch(() => props.active, (active) => {
  if (active) loadWhenActive()
})
</script>

<template>
  <component
    :is="datePickerComponent"
    v-if="datePickerComponent"
    ref="datePicker"
    v-bind="$attrs"
  >
    <template v-if="$slots.trigger" #trigger>
      <slot name="trigger" />
    </template>
    <template v-if="$slots['input-icon']" #input-icon>
      <slot name="input-icon" />
    </template>
  </component>
  <div v-else-if="$slots.trigger" :class="$attrs.class" @click="activate">
    <slot name="trigger" />
  </div>
</template>
