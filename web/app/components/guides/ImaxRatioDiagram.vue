<script setup lang="ts">
import { imaxExtraImage } from '~/utils/imaxGuide'

defineProps<{ fullFormatCinema: string }>()

const frames = [
  { ratio: 1.43, label: '1,43:1', color: 'bg-[#e8eee4] text-ink' },
  { ratio: 1.9, label: '1,90:1', color: 'bg-highlight text-ink' },
  { ratio: 2.39, label: '2,39:1', color: 'bg-accent text-white' },
]
</script>

<template>
  <figure>
    <h3 class="text-lg font-black sm:text-xl">
      Trois cadres, une même largeur.
    </h3>
    <p class="mt-2 text-sm leading-relaxed text-muted">
      Plus le chiffre est petit, plus l’image est haute.
    </p>

    <div
      class="mt-6 grid items-center gap-6 min-[900px]:grid-cols-[1.15fr_1fr] min-[900px]:gap-8"
    >
      <div>
        <div
          class="relative aspect-[1.43] border-2 border-ink bg-[#e8eee4]"
          role="img"
          aria-label="Trois rectangles de même largeur et de même centre. Le cadre 1,43:1 est le plus haut, le 1,90:1 est intermédiaire et le 2,39:1 est le moins haut."
        >
          <div
            v-for="frame in frames"
            :key="frame.ratio"
            class="absolute inset-x-0 border-y border-ink/60"
            :class="frame.color"
            :style="{
              top: `${(1 - 1.43 / frame.ratio) * 50}%`,
              height: `${(1.43 / frame.ratio) * 100}%`,
            }"
            aria-hidden="true"
          >
            <span
              class="absolute top-px left-2 font-mono text-[10px] font-bold leading-3 sm:top-1 sm:left-3 sm:text-xs sm:leading-4"
            >
              {{ frame.label }}
            </span>
          </div>
          <span
            class="absolute inset-x-0 top-1/2 -translate-y-1/2 px-4 text-center text-sm font-bold text-white sm:text-base"
            aria-hidden="true"
          >
            La même largeur.
            <span class="mt-1 block text-xs font-normal text-white/80"
              >L’image s’ouvre en haut et en bas.</span
            >
          </span>
        </div>
        <div
          class="mt-2 flex items-center gap-2 font-mono text-[10px] text-muted"
          aria-hidden="true"
        >
          <span>←</span><span class="h-px flex-1 bg-ink/30" />
          <span>Largeur identique</span><span class="h-px flex-1 bg-ink/30" />
          <span>→</span>
        </div>
      </div>

      <div>
        <p class="mb-5 text-xs text-muted">
          Surface d’image comparée au Scope, à largeur égale
        </p>
        <div class="grid gap-7">
          <div class="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-4">
            <div>
              <h4 class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <span class="font-mono text-sm font-bold">2,39:1</span>
                <span class="font-bold">Cinéma Scope</span>
              </h4>
              <p class="mt-1 text-sm text-muted">
                Le cadre large de référence.
              </p>
            </div>
            <span class="pt-1 text-xs text-muted">Référence</span>
          </div>
          <div class="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-4">
            <div>
              <h4 class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <span class="font-mono text-sm font-bold">1,90:1</span>
                <span class="font-bold">IMAX étendu</span>
              </h4>
              <p class="mt-1 text-sm text-muted">
                Le plus courant en multiplexe.
              </p>
            </div>
            <p class="text-right">
              <strong
                class="block text-2xl font-black leading-none tracking-tight text-accent"
                >+{{ imaxExtraImage(1.9) }}
                %</strong
              >
              <span class="mt-1 block text-[11px] text-muted">d’image</span>
            </p>
          </div>
          <div class="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-4">
            <div>
              <h4 class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <span class="font-mono text-sm font-bold">1,43:1</span>
                <span class="font-bold">IMAX plein format</span>
              </h4>
              <p class="mt-1 text-sm text-muted">Le cadre IMAX le plus haut.</p>
              <NuxtLink
                :to="fullFormatCinema"
                class="mt-2 inline-block text-xs underline decoration-ink/30 underline-offset-4 hover:text-accent"
                >Pathé Odysseum, Montpellier</NuxtLink
              >
            </div>
            <p class="text-right">
              <strong
                class="block text-2xl font-black leading-none tracking-tight text-accent"
                >+{{ imaxExtraImage(1.43) }}
                %</strong
              >
              <span class="mt-1 block text-[11px] text-muted">d’image</span>
            </p>
          </div>
        </div>
      </div>
    </div>
  </figure>
</template>
