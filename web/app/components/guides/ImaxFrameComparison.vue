<script setup lang="ts">
import { imaxExtraImage, imaxRatios } from '~/utils/imaxGuide'

const selectedId = ref<(typeof imaxRatios)[number]['id']>('digital')
const scopeOnly = ref(false)
const showGuides = ref(false)
const scenes = [
  {
    id: 'interstellar-edmund',
    label: 'Interstellar · le paysage',
    credit: 'Interstellar (2014), Christopher Nolan',
    alt: 'Une astronaute dans un paysage rocheux sous un vaste ciel.',
  },
  {
    id: 'interstellar-gargantua',
    label: 'Interstellar · Gargantua',
    credit: 'Interstellar (2014), Christopher Nolan',
    alt: 'Le trou noir Gargantua et son disque lumineux dans l’espace.',
  },
  {
    id: 'odyssee-voilier',
    label: 'L’Odyssée · le voilier',
    credit: 'L’Odyssée (2026), Christopher Nolan',
    alt: 'Un voilier à voile rouge navigue en mer.',
  },
] as const
const sceneId = ref('interstellar-edmund')
const scene = computed(
  () => scenes.find((item) => item.id === sceneId.value) ?? scenes[0],
)
const selected = computed(
  () =>
    imaxRatios.find((ratio) => ratio.id === selectedId.value) ?? imaxRatios[1],
)
const effectiveRatio = computed(() =>
  scopeOnly.value ? 2.39 : selected.value.ratio,
)
const maskHeight = computed(() => `${(1 - 1.43 / effectiveRatio.value) * 50}%`)
const extraImage = computed(() => imaxExtraImage(effectiveRatio.value))
</script>

<template>
  <div
    class="border-2 border-ink bg-surface shadow-[6px_6px_0_var(--color-highlight)]"
  >
    <div
      class="flex flex-wrap items-center justify-between gap-3 bg-[#f8f7f2] p-4"
    >
      <label for="imax-scene" class="font-mono text-xs font-bold uppercase"
        >Choisir une scène</label
      >
      <select
        id="imax-scene"
        v-model="sceneId"
        class="editorial-field w-full sm:w-auto"
      >
        <option v-for="item in scenes" :key="item.id" :value="item.id">
          {{ item.label }}
        </option>
      </select>
    </div>
    <fieldset class="grid grid-cols-3 border-b-2 border-ink">
      <legend class="sr-only">Choisir le cadrage à comparer</legend>
      <label
        v-for="format in imaxRatios"
        :key="format.id"
        class="relative flex min-h-20 cursor-pointer flex-col justify-center gap-1 border-r-2 border-ink p-3 text-center last:border-r-0 focus-within:z-10 focus-within:ring-2 focus-within:ring-accent focus-within:ring-inset"
        :class="selectedId === format.id ? 'bg-ink text-white' : 'hover:bg-subtle'"
      >
        <input
          v-model="selectedId"
          type="radio"
          name="imax-ratio"
          :value="format.id"
          class="sr-only"
          :aria-label="`${format.label} (${format.value})`"
          aria-controls="imax-frame-result"
        >
        <span
          class="font-mono text-[10px] font-bold uppercase tracking-wider sm:text-xs"
          >{{
            format.label
          }}</span
        >
        <span class="text-lg font-black sm:text-2xl">{{ format.value }}</span>
      </label>
    </fieldset>
    <figure>
      <div class="relative overflow-hidden bg-[#10292f] p-3 sm:p-7">
        <div
          class="relative mx-auto aspect-[1.43] max-w-3xl overflow-hidden border border-white/30"
          role="img"
          :aria-label="`${scene.alt} Simulation au format ${scopeOnly ? '2,39:1' : selected.value}. ${extraImage} % de surface en plus qu’au format Scope, à largeur égale.`"
        >
          <img
            :src="`/images/guides/${scene.id}-1600.webp`"
            :srcset="`/images/guides/${scene.id}-800.webp 800w, /images/guides/${scene.id}-1600.webp 1600w`"
            sizes="(max-width: 800px) 100vw, 768px"
            width="1430"
            height="1000"
            alt=""
            class="absolute inset-0 size-full object-cover"
            loading="lazy"
            decoding="async"
          >
          <div
            class="frame-mask absolute inset-x-0 top-0 bg-[#10292f]"
            :style="{ height: maskHeight }"
          />
          <div
            class="frame-mask absolute inset-x-0 bottom-0 bg-[#10292f]"
            :style="{ height: maskHeight }"
          />
          <div
            v-if="showGuides"
            class="pointer-events-none absolute inset-x-0 top-1/2 aspect-[2.39] -translate-y-1/2 border-y-2 border-dashed border-[#f5d998]"
            aria-hidden="true"
          />
          <span
            class="absolute bottom-2 left-2 bg-[#10292f] px-2 py-1 font-mono text-[10px] font-bold text-white sm:bottom-3 sm:left-3 sm:text-xs"
            >MÊME SCÈNE · MÊME LARGEUR</span
          >
        </div>
      </div>
      <figcaption
        id="imax-frame-result"
        class="grid gap-4 border-t-2 border-ink p-5 sm:grid-cols-[auto_1fr] sm:gap-8 sm:p-7"
        aria-live="polite"
        aria-atomic="true"
      >
        <p class="font-black text-accent">
          <span class="block text-5xl tracking-tighter">{{
            `+${extraImage} %`
          }}</span><span
            class="mt-1 block font-mono text-[10px] uppercase tracking-wide"
            >de surface d’image</span
          >
        </p>
        <div>
          <p class="font-bold">
            {{
              scopeOnly ? 'Un film en Scope reste en Scope.' : selected.description
            }}
          </p>
          <p class="mt-2 text-sm leading-relaxed text-muted">
            {{
              scopeOnly ? 'Le logo IMAX ne crée pas de contenu au-dessus et en dessous. Sans version à cadre étendu, l’image reste panoramique, même dans une salle 1,43.' : 'La scène s’ouvre en haut et en bas, sans étirer l’image. Les pourcentages comparent des rectangles à largeur identique, pas la taille réelle des écrans.'
            }}
          </p>
        </div>
      </figcaption>
    </figure>
    <p
      class="border-t border-line px-5 py-3 text-xs leading-relaxed text-muted sm:px-7"
    >
      {{ scene.credit }}. Scans de photogrammes fournis pour ce guide ; droits
      des images à leurs titulaires respectifs. Recadrages pédagogiques centrés
      : les cadrages des versions distribuées peuvent différer.
    </p>
    <div
      class="flex flex-col gap-3 border-t border-line bg-[#f8f7f2] p-5 text-sm sm:flex-row sm:flex-wrap sm:gap-x-8 sm:px-7"
    >
      <label class="flex min-h-8 cursor-pointer items-center gap-3"
        ><input
          v-model="scopeOnly"
          type="checkbox"
          class="size-4 accent-ink"
        >Tester un film sans image étendue</label
      >
      <label class="flex min-h-8 cursor-pointer items-center gap-3"
        ><input
          v-model="showGuides"
          type="checkbox"
          class="size-4 accent-ink"
        >Afficher les repères du Scope</label
      >
    </div>
  </div>
</template>

<style scoped>
.frame-mask {
  transition: height 450ms ease;
}
</style>
