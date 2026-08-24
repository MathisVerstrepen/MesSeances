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
</script>

<template>
  <div
    class="editorial-state-panel"
    :class="[`editorial-state-panel--${size}`, `editorial-state-panel--shadow-${shadow}`]"
    :role="role"
    :aria-live="live"
  >
    <slot name="icon" />
    <slot name="heading" />
    <slot />
    <slot name="actions" />
  </div>
</template>

<style scoped>
.editorial-state-panel {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 2rem;
  text-align: center;
}

.editorial-state-panel--compact { min-height: 17rem; }
.editorial-state-panel--standard { min-height: 20rem; }
.editorial-state-panel--detail { min-height: 22rem; }
.editorial-state-panel--tall { min-height: 24rem; }
.editorial-state-panel--viewport { min-height: max(22rem, calc(100vh - 23rem)); }
.editorial-state-panel--viewport-compact { min-height: max(20rem, calc(100vh - 23rem)); }
.editorial-state-panel--shadow-small { box-shadow: 6px 6px 0 #27272a; }
.editorial-state-panel--shadow-medium { box-shadow: 7px 7px 0 #27272a; }
.editorial-state-panel--shadow-large { box-shadow: 8px 8px 0 #27272a; }

@media (max-width: 639px) {
  .editorial-state-panel--detail,
  .editorial-state-panel--tall { min-height: 19rem; }
  .editorial-state-panel--viewport { min-height: 20rem; }
}
</style>
