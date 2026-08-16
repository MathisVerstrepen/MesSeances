<script setup lang="ts">
type Brand = 'UGC' | 'IMAX'
type Segment = { value: string; brand?: Brand }

const props = withDefaults(defineProps<{
  text: string
  decorative?: boolean
  logoClass?: string
}>(), {
  decorative: false,
  logoClass: ''
})

const segments = computed<Segment[]>(() => props.text
  .split(/(?<![\p{L}\p{N}_])(UGC|IMAX)(?![\p{L}\p{N}_])/gu)
  .filter(Boolean)
  .map((value) => value === 'UGC' || value === 'IMAX' ? { value, brand: value } : { value }))
</script>

<template>
  <span :aria-hidden="decorative ? 'true' : undefined">
    <span v-if="!decorative" class="sr-only">{{ text }}</span>
    <span aria-hidden="true">
      <template v-for="(segment, index) in segments" :key="`${index}-${segment.value}`">
        <BrandLogo v-if="segment.brand" :brand="segment.brand" decorative :class="logoClass" />
        <span v-else>{{ segment.value }}</span>
      </template>
    </span>
  </span>
</template>
