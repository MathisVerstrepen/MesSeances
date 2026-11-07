<script setup lang="ts">
import { computed } from 'vue'
import { Check } from '@lucide/vue'
import {
  cinemaTechnologyOptions,
  normalizeCinemaTechnologies,
} from '~/utils/cinemaDirectoryTechnologies'
import type { FormatBrand } from '~/utils/formats'

const props = defineProps<{
  modelValue: FormatBrand[]
  options: readonly { value: FormatBrand; label: string }[]
}>()
const emit = defineEmits<{
  'update:modelValue': [value: FormatBrand[]]
}>()
// Keep an unavailable URL selection visible so it can always be removed.
const visibleOptions = computed(() =>
  cinemaTechnologyOptions.filter(
    (option) =>
      props.options.some((available) => available.value === option.value) ||
      props.modelValue.includes(option.value),
  ),
)

function toggle(value: FormatBrand) {
  emit(
    'update:modelValue',
    normalizeCinemaTechnologies(
      props.modelValue.includes(value)
        ? props.modelValue.filter((selected) => selected !== value)
        : [...props.modelValue, value],
    ),
  )
}
</script>

<template>
  <fieldset v-if="visibleOptions.length" class="min-w-0 w-full">
    <legend class="sr-only">Filtrer par technologie</legend>
    <div
      class="mb-2 flex min-h-11 flex-wrap items-center justify-between gap-2"
    >
      <h3 class="text-base font-black">Technologies</h3>
      <button
        v-if="modelValue.length"
        type="button"
        class="min-h-11 px-2 text-sm font-bold underline underline-offset-4 focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ink"
        @click="emit('update:modelValue', [])"
      >
        Toutes les technologies
      </button>
    </div>
    <p class="mb-3 text-xs font-medium">
      Selon la programmation disponible. Au moins une des technologies
      sélectionnées.
    </p>
    <div class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-2">
      <label
        v-for="option in visibleOptions"
        :key="option.value"
        :title="option.label"
        class="relative flex min-h-16 min-w-0 cursor-pointer items-center gap-2 border-2 border-ink p-2 text-xs font-bold hover:bg-[#fff3c4] has-focus-visible:outline-3 has-focus-visible:outline-offset-2 has-focus-visible:outline-ink"
        :class="modelValue.includes(option.value) ? 'bg-[#fff3c4] shadow-[inset_0_0_0_1px_var(--color-ink)]' : 'bg-surface'"
      >
        <input
          type="checkbox"
          :value="option.value"
          :aria-label="option.label"
          :checked="modelValue.includes(option.value)"
          class="peer sr-only"
          @change="toggle(option.value)"
        >
        <span
          class="flex size-5 shrink-0 items-center justify-center border-2 border-ink peer-checked:bg-ink peer-checked:text-white"
          aria-hidden="true"
        >
          <Check
            v-if="modelValue.includes(option.value)"
            :size="16"
            :stroke-width="3"
          />
        </span>
        <span
          class="flex min-w-0 flex-1 items-center justify-center"
          aria-hidden="true"
        >
          <span v-if="option.value === '3D'" class="text-lg font-black"
            >3D</span
          >
          <BrandLogo
            v-else
            :brand="option.value"
            decorative
            class="!h-8 !w-24 !max-w-full object-contain"
          />
        </span>
      </label>
    </div>
  </fieldset>
</template>
