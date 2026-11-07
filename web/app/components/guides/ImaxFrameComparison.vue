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
const mobileFormatNames = {
  scope: 'Scope',
  digital: 'Étendu',
  full: 'Plein format',
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

function revealScene(event: Event) {
  const button = event.currentTarget
  if (!(button instanceof HTMLElement)) return
  const strip = button.parentElement
  if (!strip || strip.scrollWidth <= strip.clientWidth) return
  const cardBounds = button.getBoundingClientRect()
  const stripBounds = strip.getBoundingClientRect()
  if (cardBounds.left < stripBounds.left)
    strip.scrollLeft += cardBounds.left - stripBounds.left
  else if (cardBounds.right > stripBounds.right)
    strip.scrollLeft += cardBounds.right - stripBounds.right
}

function selectScene(id: (typeof scenes)[number]['id'], event: Event) {
  sceneId.value = id
  revealScene(event)
}
</script>

<template>
  <figure
    class="imax-comparison grid overflow-hidden border-2 border-ink bg-surface text-ink shadow-[3px_3px_0_var(--color-highlight)] sm:shadow-[6px_6px_0_var(--color-highlight)] min-[1180px]:grid-cols-[minmax(0,1fr)_272px]"
  >
    <div class="flex min-w-0 items-center bg-[#10292f] p-2 sm:p-4">
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
          class="mb-2 font-mono text-[10px] font-bold uppercase tracking-[0.12em] text-accent min-[1180px]:mb-3"
        >
          Le format de l’image
        </legend>
        <label
          v-for="format in formatOptions"
          :key="format.id"
          class="relative flex min-h-11 cursor-pointer items-center justify-center gap-1.5 border-b-2 px-1 py-2 font-mono text-xs font-bold transition-colors focus-within:ring-2 focus-within:ring-accent focus-within:ring-inset min-[1180px]:min-h-14 min-[1180px]:justify-start min-[1180px]:gap-3 min-[1180px]:border-2 min-[1180px]:px-3"
          :class="selectedId === format.id ? 'border-accent bg-accent-soft text-accent min-[1180px]:border-ink min-[1180px]:bg-ink min-[1180px]:text-white' : 'border-ink/15 text-ink hover:bg-subtle min-[1180px]:border-ink min-[1180px]:bg-surface min-[1180px]:hover:bg-highlight/30'"
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
            class="hidden h-7 w-8 shrink-0 min-[1180px]:block"
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
              class="mt-0.5 block font-sans text-[10px] font-normal leading-tight min-[1180px]:text-[11px]"
              :class="selectedId === format.id ? 'text-accent min-[1180px]:text-white/70' : 'text-muted'"
              ><span class="min-[1180px]:hidden">{{
                mobileFormatNames[format.id]
              }}</span><span class="hidden min-[1180px]:inline">{{
                formatNames[format.id]
              }}</span></span
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
        class="mt-4 mb-2 font-mono text-[10px] font-bold uppercase tracking-[0.12em] text-accent min-[1180px]:mt-5"
      >
        Choisir un film
      </p>
      <div
        role="group"
        aria-label="Choisir un film"
        class="flex gap-2 overflow-x-auto overscroll-x-contain pb-1 [scrollbar-width:thin] min-[1180px]:grid min-[1180px]:grid-cols-1 min-[1180px]:overflow-visible min-[1180px]:pb-0"
      >
        <button
          v-for="item in scenes"
          :key="item.id"
          type="button"
          :aria-pressed="sceneId === item.id"
          aria-controls="imax-scene-preview"
          class="flex w-52 min-w-0 shrink-0 items-center gap-2.5 border-2 p-2 text-left transition-colors focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset min-[1180px]:w-auto min-[1180px]:gap-3"
          :class="sceneId === item.id ? 'border-accent bg-accent-soft min-[1180px]:border-ink min-[1180px]:bg-highlight' : 'border-ink/15 bg-surface hover:border-ink/40 min-[1180px]:border-transparent min-[1180px]:bg-transparent min-[1180px]:hover:bg-surface'"
          @focus="revealScene"
          @click="selectScene(item.id, $event)"
        >
          <img
            :src="`/images/guides/${item.id}-poster.webp`"
            alt=""
            width="160"
            height="240"
            class="h-12 w-8 shrink-0 border border-ink/15 object-cover min-[1180px]:h-[60px] min-[1180px]:w-10"
            loading="lazy"
            decoding="async"
          >
          <span class="min-w-0"
            ><span
              class="block text-xs font-bold leading-snug min-[1180px]:text-[13px]"
              >{{
                item.label
              }}</span
            ><span
              class="mt-1 block text-[10px] leading-snug text-muted min-[1180px]:text-[11px]"
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
        class="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3 gap-y-2 sm:items-center sm:gap-6"
      >
        <div class="contents sm:block">
          <h3 class="text-sm font-black sm:text-lg">
            {{ selected.value }} — {{ formatNames[selected.id] }}
          </h3>
          <p
            class="col-span-2 max-w-2xl text-[13px] leading-relaxed text-muted sm:mt-1.5 sm:text-sm"
          >
            {{ selected.description }}
          </p>
          <p
            class="col-span-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted sm:mt-2"
          >
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
          class="col-start-2 row-start-1 flex flex-col items-end gap-0.5 sm:col-auto sm:row-auto sm:flex-row sm:items-center sm:gap-3 sm:border-l-4 sm:border-accent sm:bg-accent-soft sm:px-4 sm:py-3"
        >
          <strong
            class="shrink-0 text-xl font-black leading-none tracking-tight text-accent sm:text-2xl"
            >{{
              `+${extraImage} %`
            }}</strong
          ><span class="max-w-36 text-[11px] leading-relaxed text-muted"
            ><span class="block max-w-20 text-right sm:hidden"
              >par rapport au Scope</span
            ><span class="hidden sm:inline"
              >de surface en plus qu’en 2,39:1</span
            ></span
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
