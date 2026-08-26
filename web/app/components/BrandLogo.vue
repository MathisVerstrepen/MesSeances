<script setup lang="ts">
import cgrLogoLarge from '~/assets/imgs/cgr_logo_large.webp?no-inline'
import cgrLogoSmall from '~/assets/imgs/cgr_logo_small.webp?no-inline'
import imaxLogoLarge from '~/assets/imgs/imax_logo_large.webp?no-inline'
import imaxLogoSmall from '~/assets/imgs/imax_logo_small.webp?no-inline'
import kinepolisLogoLarge from '~/assets/imgs/kinepolis_logo_large.webp?no-inline'
import kinepolisLogoSmall from '~/assets/imgs/kinepolis_logo_small.webp?no-inline'
import logo4DXLarge from '~/assets/imgs/logo_4DX_large.webp?no-inline'
import logo4DXSmall from '~/assets/imgs/logo_4DX_small.webp?no-inline'
import laserUltraLogoLarge from '~/assets/imgs/logo_laser_ultra_large.webp?no-inline'
import laserUltraLogoSmall from '~/assets/imgs/logo_laser_ultra_small.webp?no-inline'
import logoDolbyLarge from '~/assets/imgs/logo_dolby_large.webp?no-inline'
import logoDolbySmall from '~/assets/imgs/logo_dolby_small.webp?no-inline'
import patheLogoLarge from '~/assets/imgs/pathe_logo_large.webp?no-inline'
import patheLogoSmall from '~/assets/imgs/pathe_logo_small.webp?no-inline'
import screenXLogoLarge from '~/assets/imgs/logo_screenx_large.webp?no-inline'
import screenXLogoSmall from '~/assets/imgs/logo_screenx_small.webp?no-inline'
import ugcLogoLarge from '~/assets/imgs/ugc_logo_large.webp?no-inline'
import ugcLogoSmall from '~/assets/imgs/ugc_logo_small.webp?no-inline'

type Brand = 'UGC' | 'CGR' | 'IMAX' | 'KINEPOLIS' | 'PATHE' | '3D' | 'DOLBY' | 'SCREENX' | 'LASER_ULTRA' | '4DX'

const props = withDefaults(defineProps<{
  brand: Brand
  variant?: 'inline' | 'display'
  decorative?: boolean
}>(), {
  variant: 'inline',
  decorative: false
})

const sources = {
  UGC: { inline: ugcLogoSmall, display: ugcLogoLarge },
  CGR: { inline: cgrLogoSmall, display: cgrLogoLarge },
  IMAX: { inline: imaxLogoSmall, display: imaxLogoLarge },
  KINEPOLIS: { inline: kinepolisLogoSmall, display: kinepolisLogoLarge },
  PATHE: { inline: patheLogoSmall, display: patheLogoLarge },
  DOLBY: { inline: logoDolbySmall, display: logoDolbyLarge },
  SCREENX: { inline: screenXLogoSmall, display: screenXLogoLarge },
  LASER_ULTRA: { inline: laserUltraLogoSmall, display: laserUltraLogoLarge },
  '4DX': { inline: logo4DXSmall, display: logo4DXLarge }
} satisfies Record<Exclude<Brand, '3D'>, Record<'inline' | 'display', string>>

const source = computed(() => props.brand === '3D' ? '' : sources[props.brand][props.variant])
const accessibleNames = {
  UGC: 'UGC',
  CGR: 'CGR Cinémas',
  IMAX: 'IMAX',
  KINEPOLIS: 'Kinepolis',
  PATHE: 'Pathé',
  '3D': '3D',
  DOLBY: 'Dolby',
  SCREENX: 'ScreenX',
  LASER_ULTRA: 'Laser ULTRA by Kinepolis',
  '4DX': '4DX'
} satisfies Record<Brand, string>
</script>

<template>
  <span
    v-if="brand === '3D'"
    :aria-hidden="decorative ? 'true' : undefined"
  >3D</span>
  <img
    v-else
    :src="source"
    :alt="decorative ? '' : accessibleNames[brand]"
    :aria-hidden="decorative ? 'true' : undefined"
    class="inline-block max-w-full shrink-0 select-none object-contain"
    :class="variant === 'display'
      ? (brand === 'UGC' ? 'w-36 sm:w-40' : brand === 'CGR' || brand === 'KINEPOLIS' || brand === 'PATHE' ? 'w-44 sm:w-48' : 'w-44 sm:w-52')
      : (brand === 'UGC' ? 'h-[1.15em] w-auto align-[-0.18em]' : brand === 'CGR' || brand === 'KINEPOLIS' || brand === 'PATHE' ? 'h-[0.9em] w-auto align-[-0.12em]' : 'h-[0.68em] w-auto align-[-0.06em]')"
  />
</template>
