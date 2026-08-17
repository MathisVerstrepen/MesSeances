<script setup lang="ts">
import { formatBrand, formatLabel } from '~/utils/formats'

const props = withDefaults(defineProps<{
  format: string
  decorative?: boolean
  logoClass?: string
}>(), {
  decorative: false,
  logoClass: ''
})

const brand = computed(() => formatBrand(props.format))
const label = computed(() => formatLabel(props.format))
</script>

<template>
  <span :aria-hidden="decorative ? 'true' : undefined">
    <span v-if="brand && !decorative" class="sr-only">{{ label }}</span>
    <BrandLogo v-if="brand" :brand="brand" decorative :class="logoClass" />
    <span v-else>{{ label }}</span>
  </span>
</template>
