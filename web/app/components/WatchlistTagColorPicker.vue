<script setup lang="ts">
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
        class="flex min-h-11 min-w-0 cursor-pointer items-center gap-2 rounded-md border border-ink/50 bg-surface px-2 text-sm text-ink has-[:checked]:border-ink has-[:checked]:bg-subtle has-[:checked]:font-semibold has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-[#27272a] has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-white"
      >
        <input
          type="radio"
          class="size-4 shrink-0 accent-current"
          :name="groupName"
          :value="color"
          :checked="modelValue === color"
          @change="emit('update:modelValue', color)"
        >
        <span
          class="size-3 shrink-0 rounded-full border"
          :style="watchlistTagStyle(color)"
          aria-hidden="true"
        />
        {{ token.label }}
      </label>
    </div>
  </fieldset>
</template>
