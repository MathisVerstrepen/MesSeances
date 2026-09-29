<script setup lang="ts">
withDefaults(
  defineProps<{
    variant?: 'navigation' | 'add' | 'remove'
    size?: number
  }>(),
  { variant: 'navigation', size: 24 },
)
</script>

<template>
  <svg
    xmlns="http://www.w3.org/2000/svg"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    :data-watchlist-icon="variant"
  >
    <!-- Clock / ClockPlus geometry from @lucide/vue (ISC), minus omits the vertical stroke. -->
    <template v-if="variant === 'navigation'">
      <circle cx="12" cy="12" r="10" />
      <path d="M12 6v6l4 2" />
    </template>
    <template v-else>
      <path d="M21.92 13.267a10 10 0 1 0-8.653 8.653" />
      <path d="M12 6v6l3.644 1.822" />
      <path d="M16 19h6" />
      <path v-if="variant === 'add'" d="M19 16v6" />
    </template>
  </svg>
</template>
