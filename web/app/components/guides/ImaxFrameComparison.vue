<script setup lang="ts">
import { imaxExtraImage, imaxRatios } from '~/utils/imaxGuide'

const selectedId = ref<(typeof imaxRatios)[number]['id']>('digital')
const scenes = [
  {
    id: 'dune',
    label: 'Dune : Deuxième Partie',
    director: 'Denis Villeneuve',
    alt: 'Dune : Deuxième Partie, deux personnages assis au milieu des dunes.',
  },
  {
    id: 'project-hail-mary',
    label: 'Projet Dernière Chance',
    director: 'Phil Lord et Christopher Miller',
    alt: 'Une silhouette en apesanteur devant un astre vert.',
  },
  {
    id: 'odyssee-voilier',
    label: 'L’Odyssée',
    director: 'Christopher Nolan',
    alt: 'Un voilier à voile rouge navigue en mer.',
  },
] as const
const sceneId = ref('dune')
const scene = computed(
  () => scenes.find((item) => item.id === sceneId.value) ?? scenes[0],
)
const selected = computed(
  () =>
    imaxRatios.find((ratio) => ratio.id === selectedId.value) ?? imaxRatios[1],
)
const maskHeight = computed(() => `${(1 - 1.43 / selected.value.ratio) * 50}%`)
const extraImage = computed(() => imaxExtraImage(selected.value.ratio))
</script>

<template>
  <div
    class="border-2 border-ink bg-surface shadow-[6px_6px_0_var(--color-highlight)]"
  >
    <figure>
      <div class="relative overflow-hidden bg-[#10292f] p-3 sm:p-7">
        <div
          id="imax-scene-preview"
          class="relative mx-auto aspect-[1.43] max-w-3xl overflow-hidden border border-white/30"
          role="img"
          :aria-label="`${scene.alt} Simulation au format ${selected.value}. ${extraImage} % de surface en plus qu’au format Scope, à largeur égale.`"
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
          <span
            class="absolute bottom-2 left-2 bg-[#10292f] px-2 py-1 font-mono text-[10px] font-bold text-white sm:bottom-3 sm:left-3 sm:text-xs"
            >MÊME SCÈNE · MÊME LARGEUR</span
          >
        </div>
        <div
          role="group"
          aria-label="Choisir un film"
          class="mt-4 flex flex-wrap justify-center gap-2 sm:gap-3"
        >
          <button
            v-for="item in scenes"
            :key="item.id"
            type="button"
            :aria-pressed="sceneId === item.id"
            aria-controls="imax-scene-preview"
            class="inline-flex min-h-12 flex-col items-center justify-center gap-1 rounded-sm border px-3 py-2 text-center shadow-sm transition-colors focus-visible:ring-highlight focus-visible:ring-offset-[#10292f] sm:px-4"
            :class="sceneId === item.id ? 'border-highlight bg-highlight text-ink' : 'border-white/25 bg-white/5 text-white hover:border-white/50 hover:bg-white/10'"
            @click="sceneId = item.id"
          >
            <span class="text-xs font-bold sm:text-sm">{{ item.label }}</span>
            <span
              class="text-[10px] leading-tight sm:text-xs"
              :class="sceneId === item.id ? 'text-ink/75' : 'text-white/70'"
              >{{
                item.director
              }}</span
            >
          </button>
        </div>
      </div>
      <fieldset
        class="grid grid-cols-3 border-t-2 border-ink bg-ink text-white"
      >
        <legend class="sr-only">Choisir le cadrage à comparer</legend>
        <label
          v-for="format in imaxRatios"
          :key="format.id"
          class="relative flex min-h-32 cursor-pointer flex-col items-center justify-center gap-1 border-r border-white/20 p-3 text-center last:border-r-0 focus-within:z-10 focus-within:ring-2 focus-within:ring-highlight focus-within:ring-inset"
          :class="selectedId === format.id ? 'text-white' : 'text-white/60 hover:text-white'"
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
          <svg
            viewBox="0 0 64 48"
            class="mb-1 h-10 w-14"
            fill="none"
            aria-hidden="true"
          >
            <rect
              x="8"
              :y="24 - 24 / format.ratio"
              width="48"
              :height="48 / format.ratio"
              rx="1"
              stroke="currentColor"
              stroke-width="2"
            />
          </svg>
          <span
            class="font-mono text-[10px] font-bold uppercase tracking-wider sm:text-xs"
            >{{
              format.label
            }}</span
          >
          <span class="text-lg font-black sm:text-xl">{{ format.value }}</span>
          <span
            class="mt-1 h-1 w-8"
            :class="selectedId === format.id ? 'bg-highlight' : 'bg-transparent'"
            aria-hidden="true"
          />
        </label>
      </fieldset>
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
            {{ selected.description }}
          </p>
          <p class="mt-2 text-sm leading-relaxed text-muted">
            La scène s’ouvre en haut et en bas, sans étirer l’image. Les
            surfaces sont comparées à largeur égale.
          </p>
        </div>
      </figcaption>
    </figure>
  </div>
</template>

<style scoped>
.frame-mask {
  transition: height 450ms ease;
}
</style>
