<script setup lang="ts">
import { computed } from 'vue'
import type { Provider } from '~/types/api'

const props = defineProps<{
  name: string
  provider: Provider
  decorative?: boolean
  logoClass?: string
}>()

const providerBrands = {
  ugc: 'UGC',
  cgr: 'CGR',
  kinepolis: 'KINEPOLIS',
  pathe: 'PATHE',
  megarama: 'MEGARAMA',
  cineville: 'CINEVILLE',
  mk2: 'MK2',
  cinewest: 'CINEWEST'
} as const satisfies Record<Provider, string>

// Normalize only for accessible-name detection, never for visible source text.
// Accent folding covers Pathé/Pathe and Cinéville/Cineville, including decomposed accents.
const nameIncludesProvider = computed(() => props.name.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase().split(/[^\p{L}\p{N}_]+/u).includes(props.provider))
</script>

<template>
  <span :aria-hidden="decorative ? 'true' : undefined"><BrandLogo :brand="providerBrands[provider]" :decorative="decorative || nameIncludesProvider" :class="logoClass" /> {{ name }}</span>
</template>
