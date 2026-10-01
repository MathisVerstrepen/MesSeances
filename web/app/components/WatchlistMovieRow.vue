<script setup lang="ts">
import { Film } from '@lucide/vue'
import { isCalendarDate } from '~/utils/date'
import { buildMovieExternalLinks } from '~/utils/movieExternalLinks'
import { safePosterUrl } from '~/utils/safeImageUrl'
import { formatFrenchReleaseDate } from '~/utils/upcomingMovies'
const props = defineProps<{
  title: string
  posterUrl?: string | null
  releaseDate?: string | null
  frenchReleaseDate?: string | null
  slug?: string
  tmdbId?: string
}>()
const poster = computed(() => safePosterUrl(props.posterUrl))
const tmdbLink = computed(() => {
  const id = Number(props.tmdbId)
  if (String(id) !== props.tmdbId) return undefined
  return buildMovieExternalLinks(id, null).find(
    (link) => link.destination === 'tmdb',
  )?.url
})
const frenchReleaseLabel = computed(() =>
  props.frenchReleaseDate && isCalendarDate(props.frenchReleaseDate)
    ? formatFrenchReleaseDate(props.frenchReleaseDate)
    : '',
)
</script>

<template>
  <li
    class="flex gap-3 border-b border-ink/20 py-4 sm:gap-4"
    :class="$slots.content ? 'items-start' : 'items-center'"
  >
    <img
      v-if="poster"
      :src="poster"
      alt=""
      loading="lazy"
      referrerpolicy="no-referrer"
      class="h-24 w-16 shrink-0 object-cover"
    >
    <div
      v-else
      class="flex h-24 w-16 shrink-0 items-center justify-center bg-subtle"
      aria-hidden="true"
    >
      <Film :size="24" />
    </div>
    <div class="min-w-0 flex-1">
      <NuxtLink
        v-if="slug"
        :to="`/film/${slug}`"
        :prefetch="false"
        class="break-words font-bold underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-4"
        >{{
          title
        }}</NuxtLink
      >
      <a
        v-else-if="tmdbLink"
        :href="tmdbLink"
        target="_blank"
        rel="noopener noreferrer"
        referrerpolicy="no-referrer"
        class="break-words font-bold underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-4"
        >{{
          title
        }}</a
      >
      <p v-else class="break-words font-bold">{{ title }}</p>
      <time
        v-if="frenchReleaseLabel"
        :datetime="frenchReleaseDate ?? undefined"
        class="mt-1 block text-sm text-muted"
        >{{
          frenchReleaseLabel
        }}</time
      >
      <p v-else-if="releaseDate" class="mt-1 text-sm text-muted">
        {{ releaseDate.slice(0, 4) }}
      </p>
      <slot name="content" />
    </div>
    <slot />
  </li>
</template>
