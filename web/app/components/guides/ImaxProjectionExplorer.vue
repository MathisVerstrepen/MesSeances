<script setup lang="ts">
const systems = [
  {
    id: 'laser',
    label: 'Laser 4K',
    short: '1 projecteur',
    resolution: 'Numérique 4K',
    ratio: 'Jusqu’à 1,90:1',
    aspectRatio: 1.9,
    name: 'IMAX Laser · simple laser',
    description:
      'La projection numérique courante des multiplexes IMAX, avec une image lumineuse et détaillée.',
    technical:
      'Le simple laser est aussi appelé CoLa, pour Commercial Laser. Son cadre maximal est de 1,90:1.',
  },
  {
    id: 'gt',
    label: 'Double laser',
    short: '2 projecteurs',
    resolution: 'Numérique 4K',
    ratio: 'Jusqu’à 1,43:1',
    aspectRatio: 1.43,
    name: 'IMAX · double laser',
    description:
      'Deux projecteurs laser et une optique adaptée permettent d’afficher les versions 1,43 sur un écran compatible.',
    technical:
      'Cette configuration est appelée GT Laser, pour Grand Theatre. La présence de deux projecteurs ne suffit pas à identifier un système GT : la génération et l’optique comptent aussi.',
  },
  {
    id: 'film',
    label: 'Pellicule 70 mm',
    short: '1 projecteur à film',
    resolution: 'Pellicule 15/70',
    ratio: 'Jusqu’à 1,43:1',
    aspectRatio: 1.43,
    name: 'IMAX 70 mm · pellicule',
    description:
      'Une grande image photographique sur pellicule, pour une expérience rare. La séance doit porter la mention « IMAX 70 mm ».',
    technical:
      'Chaque photogramme occupe 15 perforations et défile horizontalement. Le son est lu séparément et synchronisé avec l’image.',
  },
  {
    id: 'xenon',
    label: 'Xénon 2K',
    short: '2 projecteurs',
    resolution: 'Numérique 2K',
    ratio: 'Jusqu’à 1,90:1',
    aspectRatio: 1.9,
    name: 'IMAX numérique · xénon',
    description:
      'L’ancienne génération numérique, avec deux projecteurs à lampes. Sa définition et son contraste sont inférieurs à ceux du laser.',
    technical:
      'Les deux images 2K sont superposées : cela ne constitue pas une projection 4K native.',
  },
] as const
const selectedId = ref('laser')
const selected = computed(
  () => systems.find((system) => system.id === selectedId.value) ?? systems[0],
)
const showTechnical = ref(false)
</script>

<template>
  <div class="border-2 border-ink bg-surface">
    <fieldset class="grid grid-cols-2 border-b-2 border-ink lg:grid-cols-4">
      <legend class="sr-only">Comparer les systèmes de projection IMAX</legend>
      <label
        v-for="system in systems"
        :key="system.id"
        class="flex min-h-12 cursor-pointer items-center justify-center border border-ink/20 p-3 text-center font-mono text-xs font-bold focus-within:ring-2 focus-within:ring-inset focus-within:ring-accent"
        :class="selectedId === system.id ? 'bg-highlight' : 'hover:bg-subtle'"
      >
        <input
          v-model="selectedId"
          type="radio"
          name="imax-projection"
          :value="system.id"
          class="sr-only"
          aria-controls="imax-projection-result"
        >{{ system.label }}
      </label>
    </fieldset>
    <div
      id="imax-projection-result"
      class="grid sm:grid-cols-[0.8fr_1.2fr]"
      aria-live="polite"
      aria-atomic="true"
    >
      <div
        class="flex items-center justify-center gap-3 bg-[#f1f0e9] px-5 py-6"
        aria-hidden="true"
      >
        <div class="grid gap-2">
          <span
            v-for="number in selected.id === 'gt' || selected.id === 'xenon' ? 2 : 1"
            :key="number"
            class="grid h-8 w-12 place-items-center border-2 border-ink bg-ink font-mono text-[10px] text-white"
            >{{
              selected.id === 'film' ? '15/70' : selected.id === 'xenon' ? '2K' : '4K'
            }}</span
          >
        </div>
        <svg viewBox="0 0 100 90" class="w-12 text-accent">
          <path d="m0 40 100-35v80L0 50Z" fill="currentColor" opacity=".16" />
          <path
            d="m0 40 100-35M0 50l100 35"
            stroke="currentColor"
            stroke-width="2"
            stroke-dasharray="4 4"
          />
        </svg>
        <div class="grid h-24 w-24 place-items-center">
          <span
            class="grid w-full place-items-center border-2 border-ink bg-accent font-mono text-sm font-bold text-white transition-[aspect-ratio] motion-reduce:transition-none"
            :style="{ aspectRatio: selected.aspectRatio }"
            >{{
              selected.aspectRatio.toLocaleString('fr-FR', { minimumFractionDigits: 2 })
            }}:1</span
          >
        </div>
      </div>
      <div class="border-t-2 border-ink p-5 sm:border-t-0 sm:border-l-2 sm:p-6">
        <p class="text-lg font-black text-accent">
          {{ selected.resolution }} · {{ selected.ratio }}
        </p>
        <h3 class="mt-3 text-sm font-bold">{{ selected.name }}</h3>
        <p class="mt-2 text-sm leading-relaxed">{{ selected.description }}</p>
      </div>
    </div>
    <div class="border-t border-line">
      <button
        type="button"
        class="flex min-h-11 w-full items-center justify-between gap-3 px-5 py-3 text-left text-xs font-bold hover:bg-subtle"
        :aria-expanded="showTechnical"
        aria-controls="imax-projection-technical"
        @click="showTechnical = !showTechnical"
      >
        Détails techniques
        <span aria-hidden="true">{{ showTechnical ? '−' : '+' }}</span>
      </button>
      <p
        v-if="showTechnical"
        id="imax-projection-technical"
        class="border-t border-line px-5 py-4 text-sm leading-relaxed"
      >
        {{ selected.technical }}
      </p>
    </div>
  </div>
</template>
