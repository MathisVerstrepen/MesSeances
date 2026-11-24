<script setup lang="ts">
import { ArrowRight, Clapperboard, MapPin } from '@lucide/vue'
import type { Theater } from '~/types/api'
import { publicCinemaImageUrl } from '~/utils/cinemaImage'
import { theaterDisplayName } from '~/utils/theaterDisplayName'

const props = defineProps<{ theater: Theater }>()
const config = useRuntimeConfig()
const name = computed(() => theaterDisplayName(props.theater))
const photoUrl = computed(() =>
  publicCinemaImageUrl(props.theater.image ?? null, config.public.apiBase),
)
const photoLoaded = ref(false)
watch(photoUrl, () => {
  photoLoaded.value = false
})
</script>

<template>
  <NuxtLink
    :to="`/cinema/${encodeURIComponent(theater.slug)}`"
    :aria-label="`Voir les séances : ${name}`"
    class="group flex h-full flex-col overflow-hidden rounded-[0.4rem] border-2 border-ink bg-surface shadow-[4px_4px_0_var(--color-ink)] motion-safe:transition-transform motion-safe:hover:-translate-y-0.5"
  >
    <div
      class="relative flex min-h-28 items-end overflow-hidden border-b-2 border-ink bg-[#e8e6de] px-3 py-2.5 sm:min-h-32"
    >
      <div
        v-if="!photoLoaded"
        class="pointer-events-none absolute inset-0"
        aria-hidden="true"
      >
        <div
          class="absolute inset-0 bg-[linear-gradient(var(--color-ink)_1px,transparent_1px),linear-gradient(90deg,var(--color-ink)_1px,transparent_1px)] bg-[size:24px_24px] opacity-[0.08] mask-[linear-gradient(to_right,black_35%,transparent_85%)]"
        />
        <Clapperboard
          :stroke-width="1.5"
          class="absolute top-1/2 right-6 size-14 -translate-y-1/2 -rotate-8 text-ink opacity-[0.35]"
        />
      </div>
      <img
        v-if="photoUrl"
        :key="photoUrl"
        :src="photoUrl"
        :width="theater.image?.width"
        :height="theater.image?.height"
        alt=""
        loading="lazy"
        decoding="async"
        referrerpolicy="no-referrer"
        class="absolute inset-0 size-full object-cover object-[center_40%] grayscale-[15%] group-hover:grayscale-0 motion-safe:transition-[filter] motion-safe:duration-300"
        :class="{ 'opacity-0': !photoLoaded }"
        @load="photoLoaded = true"
        @error="photoLoaded = false"
      >
      <p
        v-if="theater.city"
        class="relative inline-flex max-w-full items-center gap-1.5 rounded-full border-2 border-ink bg-highlight px-2.5 py-1 font-mono text-[0.6875rem] leading-tight font-bold tracking-wider uppercase"
      >
        <MapPin :size="13" class="shrink-0" aria-hidden="true" />
        <span class="min-w-0 [overflow-wrap:anywhere]">{{ theater.city }}</span>
      </p>
    </div>
    <div
      class="flex flex-1 items-end justify-between gap-2.5 px-3 py-3 sm:px-4"
    >
      <div class="min-w-0">
        <h3
          class="editorial-heading text-lg [overflow-wrap:anywhere] sm:text-xl"
        >
          <TheaterName :name="name" :provider="theater.provider" />
        </h3>
        <p
          v-if="theater.address"
          class="mt-1 text-sm leading-snug text-muted [overflow-wrap:anywhere]"
        >
          {{ theater.address }}
        </p>
      </div>
      <span
        class="inline-flex shrink-0 items-center gap-1.5 font-mono text-[0.6875rem] font-bold tracking-wide uppercase group-hover:text-primary"
        aria-hidden="true"
      >
        <span class="hidden lg:inline">Séances</span>
        <ArrowRight
          :size="17"
          class="motion-safe:transition-transform motion-safe:group-hover:translate-x-0.5"
        />
      </span>
    </div>
  </NuxtLink>
</template>
