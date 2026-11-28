<script setup lang="ts">
import { Check } from '@lucide/vue'
import type { RouteLocationRaw } from 'vue-router'
import type { CinemaChain } from '~/types/api'
import { chainDisplayProvider } from '~/utils/cinemaChains'
import type {
  DirectoryFacets,
  DirectoryRadius,
} from '~/utils/cinemaDirectoryIndex'
import type { FormatBrand } from '~/utils/formats'

export interface CinemaDirectoryDrill {
  title: string
  items: {
    key: string
    label: string
    count: number
    to: RouteLocationRaw
    current?: boolean
  }[]
}

const props = defineProps<{
  variant: 'inline' | 'sheet'
  drill: CinemaDirectoryDrill | null
  radiusOptions: { radius: DirectoryRadius; count: number }[] | null
  radius: DirectoryRadius
  showRadiusCounts: boolean
  facets: DirectoryFacets
  showChains: boolean
}>()
const emit = defineEmits<{
  radius: [radius: DirectoryRadius]
  technology: [technology: FormatBrand]
  chain: [chain: CinemaChain]
  navigate: []
}>()

const id = (name: string) => `cinema-${props.variant}-${name}-label`
const sheet = computed(() => props.variant === 'sheet')
const hasDrill = computed(() => (props.drill?.items.length ?? 0) > 1)
const hasFilters = computed(
  () =>
    !!props.radiusOptions ||
    props.facets.technologies.length > 0 ||
    props.showChains,
)

function countLabel(count: number) {
  return `${count} cinéma${count > 1 ? 's' : ''}`
}

const chipClass =
  'inline-flex min-h-11 items-center gap-2 border-2 border-ink px-3 text-sm font-extrabold'
const selectedClass = 'bg-[#fff3c4] shadow-[inset_0_0_0_2px_var(--color-ink)]'
const idleClass = 'bg-surface hover:bg-[#fff3c4]'
const labelClass =
  'font-mono text-[0.62rem] font-black tracking-[0.14em] uppercase'
</script>

<template>
  <div :class="sheet ? 'divide-y-2 divide-ink/15' : ''">
    <section
      v-if="hasDrill && drill"
      :aria-labelledby="id('drill')"
      :class="sheet ? 'pb-5' : 'mt-6'"
    >
      <h3
        :id="id('drill')"
        :class="[labelClass, sheet ? 'mb-3' : 'text-muted']"
      >
        {{ drill.title }}
      </h3>
      <ul class="flex flex-wrap gap-2" :class="sheet ? '' : 'mt-2'">
        <li v-for="item in drill.items" :key="item.key">
          <NuxtLink
            :to="item.to"
            :class="[chipClass, item.current ? 'bg-ink text-white' : idleClass]"
            :aria-current="item.current ? 'page' : undefined"
            @click="emit('navigate')"
          >
            {{ item.label }}
            <span
              class="font-mono text-[0.65rem] font-black opacity-75"
              aria-hidden="true"
              >{{
                item.count
              }}</span
            >
            <span class="sr-only">, {{ countLabel(item.count) }}</span>
          </NuxtLink>
        </li>
      </ul>
    </section>

    <div
      v-if="hasFilters"
      :class="sheet ? ['space-y-6', hasDrill ? 'pt-5' : ''] : 'mt-6 flex flex-wrap gap-x-8 gap-y-4 border-2 border-dashed border-ink bg-surface px-4 py-3.5'"
    >
      <div
        v-if="radiusOptions"
        role="group"
        :aria-labelledby="id('radius')"
        :class="sheet ? '' : 'flex flex-wrap items-center gap-1.5'"
      >
        <span
          :id="id('radius')"
          :class="[labelClass, sheet ? 'mb-3 block' : 'mr-1']"
          >Rayon</span
        >
        <div :class="sheet ? 'flex flex-wrap gap-2' : 'contents'">
          <button
            v-for="option in radiusOptions"
            :key="option.radius"
            type="button"
            :class="[chipClass, option.radius === radius ? 'bg-ink text-white' : idleClass]"
            :aria-pressed="option.radius === radius"
            @click="emit('radius', option.radius)"
          >
            {{ option.radius }} km
            <span
              v-if="showRadiusCounts"
              class="font-mono text-[0.65rem] font-black opacity-75"
              >{{
                option.count
              }}</span
            >
          </button>
        </div>
      </div>

      <div
        v-if="facets.technologies.length"
        role="group"
        :aria-labelledby="id('format')"
        :class="sheet ? '' : 'flex flex-wrap items-center gap-1.5'"
      >
        <span
          :id="id('format')"
          :class="[labelClass, sheet ? 'mb-3 block' : 'mr-1']"
          >Format</span
        >
        <div :class="sheet ? 'grid grid-cols-2 gap-2' : 'contents'">
          <button
            v-for="facet in facets.technologies"
            :key="facet.value"
            type="button"
            :class="[chipClass, sheet ? 'justify-between' : '', facet.selected ? selectedClass : idleClass]"
            :aria-pressed="facet.selected"
            :aria-label="`${facet.label}, ${countLabel(facet.count)}`"
            :title="facet.label"
            @click="emit('technology', facet.value)"
          >
            <span class="inline-flex min-w-0 items-center gap-2">
              <Check
                v-if="facet.selected"
                :size="15"
                stroke-width="3"
                class="shrink-0"
                aria-hidden="true"
              />
              <BrandLogo
                :brand="facet.value"
                decorative
                class="!h-6 !w-auto !max-w-24 object-contain"
                :class="facet.value === '3D' ? 'text-base font-black' : ''"
              />
            </span>
            <span
              class="font-mono text-[0.65rem] font-black opacity-75"
              aria-hidden="true"
              >{{
                facet.count
              }}</span
            >
          </button>
        </div>
      </div>

      <div
        v-if="showChains"
        role="group"
        :aria-labelledby="id('chain')"
        :class="sheet ? '' : 'flex flex-wrap items-center gap-1.5'"
      >
        <span
          :id="id('chain')"
          :class="[labelClass, sheet ? 'mb-3 block' : 'mr-1']"
          >Enseigne</span
        >
        <div :class="sheet ? 'grid gap-2' : 'contents'">
          <button
            v-for="facet in facets.chains"
            :key="facet.value"
            type="button"
            :class="[chipClass, sheet ? 'justify-between text-left' : '', facet.selected ? selectedClass : idleClass]"
            :aria-pressed="facet.selected"
            :aria-label="`${facet.label}, ${countLabel(facet.count)}`"
            :title="facet.label"
            @click="emit('chain', facet.value)"
          >
            <span class="inline-flex min-w-0 items-center gap-2">
              <Check
                v-if="facet.selected"
                :size="15"
                stroke-width="3"
                class="shrink-0"
                aria-hidden="true"
              />
              <TheaterName
                :name="facet.label"
                :provider="chainDisplayProvider(facet.value)"
                decorative
                logo-class="!h-5 !w-auto !max-w-16 object-contain"
              />
            </span>
            <span
              class="font-mono text-[0.65rem] font-black opacity-75"
              aria-hidden="true"
              >{{
                facet.count
              }}</span
            >
          </button>
        </div>
      </div>

      <slot />
    </div>
  </div>
</template>
