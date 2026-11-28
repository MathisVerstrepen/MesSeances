<script setup lang="ts">
import { ArrowRight, Check, Plus } from '@lucide/vue'
import type { Theater } from '~/types/api'
import { publicChain } from '~/utils/cinemaChains'
import { cinemaChainLabel } from '~/utils/cinemaDirectoryChains'
import { theaterTechnologies } from '~/utils/cinemaDirectoryIndex'
import { cinemaTechnologyOptions } from '~/utils/cinemaDirectoryTechnologies'
import { formatTheaterDistance } from '~/utils/theaterDistance'
import { theaterDisplayName } from '~/utils/theaterDisplayName'

const props = defineProps<{
  theater: Theater
  distanceKm?: number | null
  isNearest?: boolean
  selected: boolean
  disabled: boolean
}>()
const emit = defineEmits<{ toggle: [event: MouseEvent] }>()

const name = computed(() => theaterDisplayName(props.theater))
const distance = computed(() => formatTheaterDistance(props.distanceKm ?? null))
const chain = computed(() =>
  cinemaChainLabel(publicChain(props.theater.provider)),
)
const location = computed(() =>
  [props.theater.postal_code, props.theater.city].filter(Boolean).join(' '),
)
const technologies = computed(() =>
  theaterTechnologies(props.theater).map(
    (value) =>
      cinemaTechnologyOptions.find((option) => option.value === value)?.label ??
      value,
  ),
)
const toggleLabel = computed(() =>
  props.selected
    ? `Retirer ${name.value} de mes cinémas`
    : `Ajouter ${name.value} à mes cinémas`,
)
</script>

<template>
  <li
    class="flex flex-wrap items-center gap-x-4 gap-y-3 border-t-2 border-line bg-surface px-4 py-3.5 first:border-t-0 sm:px-5"
  >
    <div class="min-w-0 flex-[1_1_18rem]">
      <div class="flex flex-wrap items-center gap-2">
        <h3 class="text-base leading-snug font-black [overflow-wrap:anywhere]">
          <TheaterName :name="name" :provider="theater.provider" />
        </h3>
        <span
          v-if="isNearest"
          class="border-2 border-ink bg-[#ffcf3f] px-1.5 py-px font-mono text-[0.6rem] font-black tracking-[0.06em] uppercase"
          >Le plus proche</span
        >
      </div>
      <p class="mt-1 text-sm font-semibold text-muted [overflow-wrap:anywhere]">
        {{ chain
        }}<template v-if="theater.address"> · {{ theater.address }}</template
        ><template v-if="location"> · {{ location }}</template>
      </p>
    </div>
    <ul
      v-if="technologies.length"
      class="flex flex-wrap gap-1.5"
      aria-label="Technologies"
    >
      <li
        v-for="technology in technologies"
        :key="technology"
        class="border-2 border-ink bg-surface px-1.5 py-px font-mono text-[0.62rem] font-black tracking-[0.04em] uppercase"
      >
        {{ technology }}
      </li>
    </ul>
    <span
      v-if="distance"
      class="min-w-16 text-right font-mono text-xs font-black tracking-[0.04em]"
      >{{
        distance
      }}</span
    >
    <div class="flex items-center gap-2 max-sm:w-full">
      <button
        type="button"
        class="inline-flex min-h-11 items-center justify-center gap-1.5 border-2 px-3 text-sm font-extrabold whitespace-nowrap disabled:cursor-not-allowed disabled:opacity-60 max-sm:flex-1"
        :class="selected ? 'border-accent bg-accent text-white enabled:hover:bg-accent-hover' : 'border-ink bg-surface text-ink enabled:hover:bg-accent-soft'"
        :aria-pressed="selected"
        :aria-label="toggleLabel"
        :disabled="disabled"
        @click="emit('toggle', $event)"
      >
        <Check v-if="selected" :size="16" stroke-width="3" aria-hidden="true" />
        <Plus v-else :size="16" stroke-width="3" aria-hidden="true" />
        {{ selected ? 'Dans mes cinémas' : 'Mes cinémas' }}
      </button>
      <NuxtLink
        :to="`/cinema/${encodeURIComponent(theater.slug)}`"
        :aria-label="`Voir les séances : ${name}`"
        class="inline-flex min-h-11 items-center justify-center gap-1.5 border-2 border-ink bg-surface px-3 font-mono text-[0.68rem] font-black tracking-[0.08em] uppercase hover:bg-highlight max-sm:flex-1"
      >
        Séances <ArrowRight :size="16" aria-hidden="true" />
      </NuxtLink>
    </div>
  </li>
</template>
