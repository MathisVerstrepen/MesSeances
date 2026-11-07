<script setup lang="ts">
import { Check } from '@lucide/vue'
import { imaxExtraImage, imaxRatios } from '~/utils/imaxGuide'

defineProps<{ movieTargets?: Record<string, string> }>()

const selectedId = ref<(typeof imaxRatios)[number]['id']>('digital')
const formatOptions = [...imaxRatios].reverse()
const formatNames = {
  scope: 'Cinéma Scope',
  digital: 'IMAX étendu',
  full: 'IMAX plein format',
}
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
  <figure
    class="imax-comparison grid overflow-hidden border-2 border-ink bg-surface text-ink shadow-[6px_6px_0_var(--color-highlight)] min-[1180px]:grid-cols-[minmax(0,1fr)_272px]"
  >
    <div class="flex min-w-0 items-center bg-[#10292f] p-3 sm:p-4">
      <div
        id="imax-scene-preview"
        class="relative mx-auto aspect-[1.43] w-full max-w-[680px] overflow-hidden border border-white/25 bg-[#10292f]"
        role="img"
        :aria-label="`${scene.alt} Simulation au format ${selected.value}. ${extraImage} % de surface en plus qu’au format Scope, à largeur égale.`"
      >
        <img
          :src="`/images/guides/${scene.id}-1600.webp`"
          :srcset="`/images/guides/${scene.id}-800.webp 800w, /images/guides/${scene.id}-1600.webp 1600w`"
          sizes="(max-width: 767px) 90vw, (max-width: 1179px) 75vw, 680px"
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
          class="absolute top-2 left-2 border border-white/20 bg-ink px-2 py-1 font-mono text-xs font-bold text-white sm:top-3 sm:left-3"
          >{{
            selected.value
          }}</span
        >
      </div>
    </div>

    <aside
      class="min-w-0 border-t-2 border-ink bg-[#f8f7f2] p-3 sm:p-4 min-[1180px]:border-t-0 min-[1180px]:border-l-2"
      aria-label="Sélections du comparateur"
    >
      <fieldset class="grid grid-cols-3 gap-2 min-[1180px]:grid-cols-1">
        <legend
          class="mb-3 font-mono text-[10px] font-bold uppercase tracking-[0.12em] text-accent"
        >
          Le format de l’image
        </legend>
        <label
          v-for="format in formatOptions"
          :key="format.id"
          class="relative flex min-h-11 cursor-pointer items-center justify-center gap-1.5 border-2 border-ink px-1 py-2 font-mono text-[11px] font-bold transition-colors focus-within:ring-2 focus-within:ring-accent focus-within:ring-inset min-[1180px]:min-h-14 min-[1180px]:justify-start min-[1180px]:gap-3 min-[1180px]:px-3"
          :class="selectedId === format.id ? 'bg-ink text-white' : 'bg-surface text-ink hover:bg-highlight/30'"
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
            viewBox="0 0 40 32"
            class="h-5 w-5 shrink-0 min-[1180px]:h-7 min-[1180px]:w-8"
            fill="none"
            aria-hidden="true"
          >
            <rect
              x="3"
              :y="16 - 17 / format.ratio"
              width="34"
              :height="34 / format.ratio"
              rx="1"
              stroke="currentColor"
              stroke-width="1.3"
            />
          </svg>
          <span class="min-w-0 flex-1 text-center min-[1180px]:text-left"
            ><span class="block min-[1180px]:text-sm">{{ format.value }}</span
            ><span
              class="mt-0.5 hidden font-sans text-[11px] font-normal min-[1180px]:block"
              :class="selectedId === format.id ? 'text-white/70' : 'text-muted'"
              >{{
                formatNames[format.id]
              }}</span
            ></span
          >
          <Check
            v-if="selectedId === format.id"
            :size="14"
            class="hidden shrink-0 min-[1180px]:block"
            aria-hidden="true"
          />
        </label>
      </fieldset>

      <p
        class="mt-5 mb-2 font-mono text-[10px] font-bold uppercase tracking-[0.12em] text-accent"
      >
        Choisir un film
      </p>
      <div
        role="group"
        aria-label="Choisir un film"
        class="grid grid-cols-3 gap-1.5 min-[1180px]:grid-cols-1 min-[1180px]:gap-2"
      >
        <button
          v-for="item in scenes"
          :key="item.id"
          type="button"
          :aria-pressed="sceneId === item.id"
          aria-controls="imax-scene-preview"
          class="flex min-w-0 flex-col items-center gap-2 border-2 p-2 text-center transition-colors focus-visible:ring-accent focus-visible:ring-inset sm:flex-row sm:gap-3 sm:text-left"
          :class="sceneId === item.id ? 'border-ink bg-highlight' : 'border-transparent hover:border-ink/30 hover:bg-surface'"
          @click="sceneId = item.id"
        >
          <img
            :src="`/images/guides/${item.id}-poster.webp`"
            alt=""
            width="160"
            height="240"
            class="h-14 w-[37px] shrink-0 border border-ink/15 object-cover sm:h-[60px] sm:w-10"
            loading="lazy"
            decoding="async"
          >
          <span class="min-w-0"
            ><span
              class="block text-[11px] font-bold leading-snug sm:text-[13px]"
              >{{
                item.label
              }}</span
            ><span
              class="mt-1 block text-[10px] leading-snug text-muted sm:text-[11px]"
              >{{
                item.director
              }}</span
            ></span
          >
        </button>
      </div>
    </aside>

    <figcaption
      id="imax-frame-result"
      class="border-t-2 border-ink p-4 sm:p-5 min-[1180px]:col-span-2"
      aria-live="polite"
      aria-atomic="true"
    >
      <div
        class="grid gap-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:gap-6"
      >
        <div>
          <h3 class="text-base font-black sm:text-lg">
            {{ selected.value }} — {{ formatNames[selected.id] }}
          </h3>
          <p class="mt-1.5 max-w-2xl text-sm leading-relaxed text-muted">
            {{ selected.description }}
          </p>
          <p class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted">
            <span>Simulation de cadrage, à largeur égale.</span>
            <NuxtLink
              v-if="movieTargets?.[scene.id]"
              :to="movieTargets[scene.id]"
              class="underline decoration-ink/30 underline-offset-4 hover:text-accent"
              >{{ scene.label }}
              ↗</NuxtLink
            >
          </p>
        </div>
        <p
          class="flex items-center gap-3 border-l-4 border-accent bg-accent-soft px-4 py-3"
        >
          <strong
            class="shrink-0 text-2xl font-black tracking-tight text-accent"
            >{{
              `+${extraImage} %`
            }}</strong
          ><span class="max-w-36 text-[11px] leading-relaxed text-muted"
            >de surface en plus qu’en 2,39:1</span
          >
        </p>
      </div>
    </figcaption>
  </figure>
</template>

<style scoped>
.frame-mask {
  transition: height 450ms ease;
}
</style>
