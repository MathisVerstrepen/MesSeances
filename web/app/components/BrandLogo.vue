<script setup lang="ts">
import imaxLogoLarge from '~/assets/imgs/imax_logo_large.webp?no-inline'
import imaxLogoSmall from '~/assets/imgs/imax_logo_small.webp?no-inline'
import ugcLogoLarge from '~/assets/imgs/ugc_logo_large.webp?no-inline'
import ugcLogoSmall from '~/assets/imgs/ugc_logo_small.webp?no-inline'

type Brand = 'UGC' | 'IMAX'

const props = withDefaults(defineProps<{
  brand: Brand
  variant?: 'inline' | 'display'
  decorative?: boolean
}>(), {
  variant: 'inline',
  decorative: false
})

const sources: Record<Brand, Record<'inline' | 'display', string>> = {
  UGC: { inline: ugcLogoSmall, display: ugcLogoLarge },
  IMAX: { inline: imaxLogoSmall, display: imaxLogoLarge }
}

const source = computed(() => sources[props.brand][props.variant])
</script>

<template>
  <img
    :src="source"
    :alt="decorative ? '' : brand"
    :aria-hidden="decorative ? 'true' : undefined"
    class="inline-block max-w-full shrink-0 select-none object-contain"
    :class="variant === 'display'
      ? (brand === 'UGC' ? 'w-36 sm:w-40' : 'w-44 sm:w-52')
      : (brand === 'UGC' ? 'w-auto align-[-0.18em] h-[1.15em]' : 'w-auto align-[-0.06em] h-[0.68em]')"
  />
</template>
