<script setup lang="ts">
const props = withDefaults(defineProps<{
  semantic?: 'neutral' | 'status' | 'alert'
  live?: 'off' | 'polite' | 'assertive'
  size?: 'compact' | 'standard' | 'detail' | 'tall' | 'viewport' | 'viewport-compact'
  shadow?: 'small' | 'medium' | 'large'
}>(), {
  semantic: 'neutral',
  live: undefined,
  size: 'standard',
  shadow: 'large'
})

const role = computed(() => props.semantic === 'neutral' ? undefined : props.semantic)

const sizeClasses = {
  compact: 'min-h-[17rem]',
  standard: 'min-h-[20rem]',
  detail: 'min-h-[22rem] max-sm:min-h-[19rem]',
  tall: 'min-h-[24rem] max-sm:min-h-[19rem]',
  viewport: 'min-h-[max(22rem,calc(100vh-23rem))] max-sm:min-h-[20rem]',
  'viewport-compact': 'min-h-[max(20rem,calc(100vh-23rem))]'
} satisfies Record<NonNullable<typeof props.size>, string>

const shadowClasses = {
  small: 'shadow-[6px_6px_0_#27272a]',
  medium: 'shadow-[7px_7px_0_#27272a]',
  large: 'shadow-[8px_8px_0_#27272a]'
} satisfies Record<NonNullable<typeof props.shadow>, string>
</script>

<template>
  <div
    class="flex flex-col items-center justify-center gap-4 border-2 border-ink bg-surface p-8 text-center"
    :class="[sizeClasses[size], shadowClasses[shadow]]"
    :role="role"
    :aria-live="live"
  >
    <slot name="icon" />
    <slot name="heading" />
    <slot />
    <slot name="actions" />
  </div>
</template>
