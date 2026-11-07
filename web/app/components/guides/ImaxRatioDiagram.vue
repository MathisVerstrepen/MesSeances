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
      D’où vient l’image supplémentaire ?
    </h3>
    <p class="mt-2 text-sm leading-relaxed text-muted">
      Quand le film le prévoit, les formats IMAX révèlent plus d’image en haut
      et en bas.
    </p>

    <div
      class="mt-6 grid items-center gap-6 min-[900px]:grid-cols-[1.15fr_1fr] min-[900px]:gap-8"
    >
      <div>
        <div
          class="relative aspect-[1.43] border-2 border-ink bg-[#e8eee4]"
          role="img"
          aria-label="Trois cadres centrés de même largeur. La zone centrale représente le Scope en 2,39:1. Le 1,90:1 ajoute de la hauteur au-dessus et en dessous, et le 1,43:1 en ajoute davantage."
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
            L’image au format Scope
            <span class="mt-1 block text-xs font-normal text-white/80"
              >Les zones claires montrent l’image supplémentaire.</span
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
        <div class="grid gap-7">
          <div class="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-4">
            <div>
              <h4 class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <span class="font-mono text-sm font-bold">2,39:1</span>
                <span class="font-bold">Cinéma Scope</span>
              </h4>
              <p class="mt-1 text-sm leading-relaxed text-muted">
                Un cadre panoramique, très courant au cinéma.
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
              <p class="mt-1 text-sm leading-relaxed text-muted">
                Le format IMAX courant en multiplexe. Pour les scènes
                compatibles, l’image s’ouvre en haut et en bas par rapport au
                Scope.
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
              <p class="mt-1 text-sm leading-relaxed text-muted">
                Le cadre historique de l’IMAX, proche du carré. Il révèle encore
                plus de hauteur, avec une version du film et une salle
                compatibles.
              </p>
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
