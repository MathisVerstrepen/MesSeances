<script setup lang="ts">
import { computed } from 'vue'
import type { Provider } from '~/types/api'

const props = defineProps<{
  name: string
  provider: Provider
  variant?: 'inline' | 'hero'
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
  cinewest: 'CINEWEST',
  grandecran: 'Grand Ecran',
  noecinemas: 'Noé Cinémas',
  veo: 'Véo Cinémas',
  majestic: 'Majestic Cinémas',
  cineode: 'Cinéode',
  cinemovida: 'Ciné-Movida',
  ocine: 'Ociné',
} as const satisfies Record<Provider, string>

// Normalize only for accessible-name detection, never for visible source text.
// Accent folding also covers multi-word Grand Écran, including decomposed accents.
const nameIncludesProvider = computed(() => {
  const name = props.name.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()
  return (
    name.split(/[^\p{L}\p{N}_]+/u).includes(props.provider) ||
    (props.provider === 'grandecran' &&
      /(?<![\p{L}\p{N}_])grand\s+ecran(?![\p{L}\p{N}_])/u.test(name)) ||
    (props.provider === 'noecinemas' &&
      /(?<![\p{L}\p{N}_])noe\s+cinemas(?![\p{L}\p{N}_])/u.test(name)) ||
    (props.provider === 'cinemovida' &&
      /(?<![\p{L}\p{N}_])cine[-\s]?movida(?![\p{L}\p{N}_])/u.test(name))
  )
})
</script>

<template>
  <span
    v-if="variant === 'hero'"
    class="block"
    :aria-hidden="decorative ? 'true' : undefined"
  >
    <span
      class="mb-4 flex max-w-full items-center justify-center border-2 border-ink bg-white p-2"
      :class="provider === 'ocine' ? 'h-20 w-28 lg:h-24 lg:w-32' : provider === 'cinemovida' ? 'h-20 w-20 lg:h-24 lg:w-24' : 'h-[52px] w-[130px] lg:h-14 lg:w-36'"
    >
      <BrandLogo
        :brand="providerBrands[provider]"
        variant="display"
        :decorative="decorative || nameIncludesProvider"
        class="max-w-full object-contain"
        :class="[provider === 'cinemovida' || provider === 'ocine' ? 'h-full w-full' : 'h-8 lg:h-9', logoClass]"
      />
    </span>
    <span class="block break-words [overflow-wrap:anywhere]">{{ name }}</span>
  </span>
  <span v-else :aria-hidden="decorative ? 'true' : undefined"
    ><BrandLogo
      :brand="providerBrands[provider]"
      :decorative="decorative || nameIncludesProvider"
      :class="logoClass"
    /> {{ name }}</span
  >
</template>
