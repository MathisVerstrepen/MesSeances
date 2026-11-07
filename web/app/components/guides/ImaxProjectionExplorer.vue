<script setup lang="ts">
const systems = [
  {
    id: 'laser',
    label: 'Laser courant',
    short: '1 projecteur',
    resolution: '4K',
    ratio: '1,90:1',
    aspectRatio: 1.9,
    name: 'IMAX Laser · simple laser',
    text: 'Souvent appelé CoLa (Commercial Laser). C’est une configuration adaptée aux multiplexes. Elle améliore notamment la netteté, la luminosité et les couleurs par rapport à l’ancienne génération au xénon.',
    takeaway: 'Le simple laser affiche un cadre allant jusqu’à 1,90:1.',
    sound: 'Le son peut être à 12 canaux selon l’installation.',
  },
  {
    id: 'gt',
    label: 'Double laser GT',
    short: '2 projecteurs',
    resolution: '4K',
    ratio: 'jusqu’à 1,43:1',
    aspectRatio: 1.43,
    name: 'IMAX GT Laser · double laser',
    text: 'Deux projecteurs laser et une optique adaptée permettent d’afficher un cadre très haut sur un écran compatible. Le film doit lui aussi disposer d’une version en 1,43.',
    takeaway:
      'Le double laser GT affiche les versions 1,43 sur un écran compatible.',
    sound: 'Une installation sonore immersive, généralement à 12 canaux.',
  },
  {
    id: 'film',
    label: 'Pellicule 70 mm',
    short: '1 projecteur à film',
    resolution: 'Pellicule',
    ratio: 'jusqu’à 1,43:1',
    aspectRatio: 1.43,
    name: 'IMAX 70 mm · 15/70',
    text: 'Une grande image photographique défile horizontalement, avec 15 perforations par photogramme. C’est un support physique, distinct du 70 mm classique à 5 perforations.',
    takeaway:
      'Une expérience rare et liée à une copie précise du film. Cherchez la mention complète « IMAX 70 mm » sur la séance.',
    sound:
      'Le son est lu séparément de la pellicule et synchronisé avec l’image.',
  },
  {
    id: 'xenon',
    label: 'Numérique xénon',
    short: '2 projecteurs',
    resolution: '2K',
    ratio: '1,90:1',
    aspectRatio: 1.9,
    name: 'IMAX numérique · xénon',
    text: 'La première génération de projection IMAX numérique utilise deux projecteurs 2K à lampes. La superposition de deux images 2K ne constitue pas une projection 4K native.',
    takeaway:
      'Cette génération est limitée au 1,90:1 et à une définition de 2K par projecteur.',
    sound:
      'Le système historique utilise moins de canaux que les installations à 12 canaux.',
  },
] as const
const selectedId = ref('laser')
const selected = computed(
  () => systems.find((system) => system.id === selectedId.value) ?? systems[0],
)
</script>

<template>
  <div class="border-2 border-ink bg-surface">
    <fieldset class="grid grid-cols-2 border-b-2 border-ink lg:grid-cols-4">
      <legend class="sr-only">Comparer les systèmes de projection IMAX</legend>
      <label
        v-for="system in systems"
        :key="system.id"
        class="flex min-h-14 cursor-pointer items-center justify-center border border-ink/20 p-3 text-center font-mono text-xs font-bold focus-within:ring-2 focus-within:ring-inset focus-within:ring-accent"
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
      class="grid lg:grid-cols-[1fr_1.15fr]"
      aria-live="polite"
      aria-atomic="true"
    >
      <div class="flex flex-col justify-center bg-[#f1f0e9] p-6 sm:p-8">
        <div class="flex items-center justify-center gap-4" aria-hidden="true">
          <div class="grid gap-2">
            <span
              v-for="number in selected.id === 'gt' || selected.id === 'xenon' ? 2 : 1"
              :key="number"
              class="grid h-10 w-14 place-items-center border-2 border-ink bg-ink font-mono text-xs text-white"
              >{{
                selected.id === 'film' ? '15/70' : selected.resolution
              }}</span
            >
          </div>
          <svg viewBox="0 0 100 90" class="w-20 text-accent">
            <path d="m0 40 100-35v80L0 50Z" fill="currentColor" opacity=".16" />
            <path
              d="m0 40 100-35M0 50l100 35"
              stroke="currentColor"
              stroke-width="2"
              stroke-dasharray="4 4"
            />
          </svg>
          <div class="grid h-36 w-32 place-items-center">
            <span
              class="grid w-full place-items-center border-2 border-ink bg-accent text-center font-mono text-sm font-bold text-white transition-[aspect-ratio] motion-reduce:transition-none"
              :style="{ aspectRatio: selected.aspectRatio }"
              >{{
                selected.ratio
              }}</span
            >
          </div>
        </div>
        <dl
          class="mt-6 grid grid-cols-2 gap-4 border-t border-ink/20 pt-4 font-mono text-xs"
        >
          <div>
            <dt class="text-muted">Projection</dt>
            <dd class="mt-1 font-bold">{{ selected.short }}</dd>
          </div>
          <div>
            <dt class="text-muted">Support / définition</dt>
            <dd class="mt-1 font-bold">{{ selected.resolution }}</dd>
          </div>
        </dl>
      </div>
      <div class="border-t-2 border-ink p-6 sm:p-8 lg:border-t-0 lg:border-l-2">
        <h3 class="editorial-heading">{{ selected.name }}</h3>
        <p class="mt-4 leading-relaxed">{{ selected.text }}</p>
        <p class="mt-4 text-sm leading-relaxed text-muted">
          {{ selected.sound }}
        </p>
        <p
          class="mt-5 border-l-4 border-accent pl-4 text-sm font-bold leading-relaxed"
        >
          {{ selected.takeaway }}
        </p>
      </div>
    </div>
  </div>
</template>
