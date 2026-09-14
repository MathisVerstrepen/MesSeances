<script setup lang="ts">
type Brand = 'UGC' | 'CGR' | 'MEGARAMA' | 'CINEVILLE' | 'MK2' | 'CINEWEST' | 'Grand Ecran' | 'IMAX' | 'KINEPOLIS' | 'PATHE'
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
  .split(/(?<![\p{L}\p{N}_])(UGC|CGR|Megarama|IMAX|Kinepolis|Pathé|Pathe|Cinéville|Cineville|MK2|Cinewest|Grand [EÉ]cran)(?![\p{L}\p{N}_])/giu)
  .filter(Boolean)
  .map((value) => {
    const brand = value.toUpperCase().replace('É', 'E')
    if (brand === 'GRAND ECRAN') return { value, brand: 'Grand Ecran' }
    return brand === 'UGC' || brand === 'CGR' || brand === 'MEGARAMA' || brand === 'CINEVILLE' || brand === 'MK2' || brand === 'CINEWEST' || brand === 'IMAX' || brand === 'KINEPOLIS' || brand === 'PATHE' ? { value, brand } : { value }
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
