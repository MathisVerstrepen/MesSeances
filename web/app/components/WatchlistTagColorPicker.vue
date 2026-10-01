<script setup lang="ts">
import { Check } from '@lucide/vue'
import type { WatchlistTagColor } from '~/types/watchlist'
import { watchlistTagPalette, watchlistTagStyle } from '~/utils/watchlistTags'

defineProps<{ modelValue: WatchlistTagColor }>()
const emit = defineEmits<{ 'update:modelValue': [color: WatchlistTagColor] }>()
const groupName = useId()
</script>

<template>
  <fieldset class="mt-3 min-w-0">
    <legend class="account-label">Couleur</legend>
    <div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
      <label
        v-for="(token, color) in watchlistTagPalette"
        :key="color"
        class="grid min-h-11 min-w-0 grid-cols-[20px_minmax(0,1fr)_20px] cursor-pointer items-center gap-1 rounded-none border-2 px-1 py-2 font-mono text-center text-xs font-bold leading-relaxed has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-solid has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-ink has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-white"
        :style="watchlistTagStyle(color)"
      >
        <input
          type="radio"
          class="sr-only focus-visible:ring-0"
          :name="groupName"
          :value="color"
          :checked="modelValue === color"
          @change="emit('update:modelValue', color)"
        >
        <span
          class="grid size-5 shrink-0 place-items-center"
          aria-hidden="true"
        >
          <Check
            v-if="modelValue === color"
            :size="20"
            aria-hidden="true"
            focusable="false"
          />
        </span>
        <span class="min-w-0 justify-self-center [overflow-wrap:anywhere]">{{
          token.label
        }}</span>
      </label>
    </div>
  </fieldset>
</template>
