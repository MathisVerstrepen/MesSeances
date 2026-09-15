<script setup lang="ts">
import { statisticsSearchOptions, statisticsSelectionSummary, toggleStatisticsSelection, type StatisticsSelectOption } from '~/utils/statistics'

const props = defineProps<{
  id: string
  label: string
  allLabel: string
  modelValue: string[]
  options: readonly StatisticsSelectOption[]
  maxSelections: number
  externalSearch?: boolean
  single?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string[]]; search: [value: string]; open: [] }>()
const disclosure = useTemplateRef<HTMLDetailsElement>('disclosure')
const summary = useTemplateRef<HTMLElement>('summary')
const search = ref('')
const selected = computed(() => new Set(props.modelValue))
const atLimit = computed(() => !props.single && props.modelValue.length >= props.maxSelections)
const visibleOptions = computed(() => props.externalSearch ? props.options : statisticsSearchOptions(props.options, props.modelValue, search.value))
watch(search, value => { if (props.externalSearch) emit('search', value) })
const summaryText = computed(() => statisticsSelectionSummary(props.options, props.modelValue, props.label, props.allLabel))
function close(event: KeyboardEvent) {
  if (!disclosure.value?.open) return
  event.preventDefault()
  event.stopPropagation()
  disclosure.value.open = false
  summary.value?.focus()
}
function toggle(value: string) {
  emit('update:modelValue', props.single ? [value] : toggleStatisticsSelection(props.modelValue, value, props.maxSelections))
}
function onToggle() { if (disclosure.value?.open) emit('open') }
const focusClass = 'focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink'
</script>

<template>
  <fieldset class="min-w-0">
    <legend :id="`${id}-label`" class="text-xs font-extrabold uppercase tracking-wide">{{ label }}</legend>
    <details ref="disclosure" class="relative mt-2 min-w-0 border-2 border-ink bg-surface open:z-30" @keydown.esc="close" @toggle="onToggle">
      <summary ref="summary" :aria-labelledby="`${id}-label ${id}-summary`" :class="['min-h-11 cursor-pointer px-3 py-3 text-sm font-bold [overflow-wrap:anywhere]', focusClass]">
        <span :id="`${id}-summary`">{{ summaryText }}</span>
      </summary>
      <div class="absolute -inset-x-0.5 top-full min-w-0 border-2 border-ink bg-surface p-3 shadow-[5px_5px_0_#27272a]">
        <label :for="`${id}-search`" class="text-xs font-extrabold">Rechercher</label>
        <input :id="`${id}-search`" v-model="search" type="search" :class="['mt-2 min-h-11 w-full min-w-0 rounded-none border-2 border-ink bg-surface px-3 text-sm', focusClass]" @keydown.enter.prevent />
        <button type="button" :class="['my-2 min-h-11 px-3 text-sm font-extrabold underline underline-offset-4 disabled:opacity-40', focusClass]" :disabled="modelValue.length === 0" @click="emit('update:modelValue', [])">Effacer</button>
        <p v-if="atLimit" :id="`${id}-limit`" role="status" class="mb-2 text-sm font-bold">{{ maxSelections }} sélections maximum. Retirez une sélection pour en ajouter une autre.</p>
        <slot name="feedback" />
        <div class="max-h-64 overflow-y-auto overscroll-contain p-1">
          <label v-for="(option, index) in visibleOptions" :key="option.value" :for="`${id}-option-${index}`" class="flex min-h-11 cursor-pointer items-center gap-3 py-2 text-sm has-disabled:cursor-not-allowed has-disabled:opacity-40">
            <input v-if="single" :id="`${id}-option-${index}`" type="radio" :name="`${id}-choice`" :checked="selected.has(option.value)" :class="['size-5 shrink-0 accent-ink', focusClass]" @change="toggle(option.value)" />
            <input v-else :id="`${id}-option-${index}`" type="checkbox" :checked="selected.has(option.value)" :disabled="atLimit && !selected.has(option.value)" :aria-describedby="atLimit ? `${id}-limit` : undefined" :class="['size-5 shrink-0 accent-ink', focusClass]" @change="toggle(option.value)" />
            <span class="min-w-0 [overflow-wrap:anywhere]">{{ option.label }}</span>
          </label>
          <p v-if="visibleOptions.length === 0" class="py-3 text-sm">Aucune option</p>
        </div>
      </div>
    </details>
  </fieldset>
</template>
