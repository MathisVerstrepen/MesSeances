<script setup lang="ts">
type Brand = 'UGC' | 'IMAX' | 'KINEPOLIS'
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
  .split(/(?<![\p{L}\p{N}_])(UGC|IMAX|Kinepolis)(?![\p{L}\p{N}_])/giu)
  .filter(Boolean)
  .map((value) => {
    const brand = value.toUpperCase()
    return brand === 'UGC' || brand === 'IMAX' || brand === 'KINEPOLIS' ? { value, brand } : { value }
  }))
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
