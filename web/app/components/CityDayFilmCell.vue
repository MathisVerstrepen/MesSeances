<script setup lang="ts">
import type { CityDayMovieMedia } from '~/utils/cityDay'
import { formatRuntime } from '~/utils/formats'

const props = defineProps<{
  slug: string
  title: string
  runtimeMinutes: number
  href: string
  media: CityDayMovieMedia | undefined
  showtimeCount: number
}>()

// Same fallback as the film groups of the cinema and search pages: the
// backdrop, else the blurred poster, else a plain dark band.
const backdropFailed = ref(false)
const posterBackdropFailed = ref(false)
const backdropImage = ref<HTMLImageElement | null>(null)
const usePosterBackdrop = computed(
  () => !props.media?.backdrop || backdropFailed.value,
)
const backgroundUrl = computed(() =>
  usePosterBackdrop.value
    ? posterBackdropFailed.value
      ? null
      : (props.media?.poster ?? null)
    : (props.media?.backdrop ?? null),
)

function handleBackdropError() {
  if (usePosterBackdrop.value) posterBackdropFailed.value = true
  else backdropFailed.value = true
}

watch(
  () => [props.media?.poster, props.media?.backdrop],
  () => {
    backdropFailed.value = false
    posterBackdropFailed.value = false
  },
)

onMounted(() =>
  nextTick(() => {
    if (backdropImage.value?.complete && backdropImage.value.naturalWidth === 0)
      handleBackdropError()
  }),
)
</script>

<template>
  <div
    class="relative isolate flex items-center gap-4 overflow-hidden bg-ink p-4 text-white sm:p-5"
  >
    <img
      v-if="backgroundUrl"
      ref="backdropImage"
      :key="backgroundUrl"
      :src="backgroundUrl"
      alt=""
      width="780"
      height="439"
      loading="lazy"
      decoding="async"
      aria-hidden="true"
      class="absolute inset-0 -z-20 size-full object-cover"
      :class="usePosterBackdrop ? 'scale-110 blur-xl' : undefined"
      @error="handleBackdropError"
    >
    <div class="absolute inset-0 -z-10 bg-black/75" aria-hidden="true" />
    <div
      class="relative flex aspect-[2/3] w-16 shrink-0 items-center justify-center overflow-hidden border-2 border-ink bg-[#e8e6de] shadow-[4px_4px_0_#27272a] sm:w-[4.5rem]"
    >
      <PosterImage
        :src="media?.poster"
        :alt="`Affiche de ${title}`"
        sizes="(min-width: 640px) 72px, 64px"
        class="size-full"
        image-class="size-full object-cover"
        fallback-variant="icon-only"
        fallback-class="text-muted/60"
        :fallback-icon-size="24"
        :fallback-text="null"
      />
      <WatchedBadge :slug="slug" />
    </div>
    <div class="min-w-0">
      <h3 class="break-words text-lg font-black leading-tight sm:text-xl">
        <NuxtLink
          :to="href"
          class="underline decoration-2 underline-offset-4 hover:text-highlight focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-ink"
          >{{
            title
          }}</NuxtLink
        >
      </h3>
      <p
        class="mt-2 font-mono text-[10px] font-bold uppercase tracking-[0.1em] text-white"
      >
        <template v-if="runtimeMinutes > 0"
          >{{ formatRuntime(runtimeMinutes) }}
          ·
        </template>
        {{ showtimeCount }} séance{{ showtimeCount > 1 ? 's' : '' }}
      </p>
    </div>
  </div>
</template>
