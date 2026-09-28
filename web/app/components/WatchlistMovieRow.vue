<script setup lang="ts">
import { Film } from '@lucide/vue'
import { safePosterUrl } from '~/utils/safeImageUrl'
const props = defineProps<{
  title: string
  posterUrl?: string | null
  releaseDate?: string | null
  slug?: string
}>()
const poster = computed(() => safePosterUrl(props.posterUrl))
</script>

<template>
  <li class="flex items-center gap-4 border-b border-ink/20 py-4">
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
      <p v-else class="break-words font-bold">{{ title }}</p>
      <p v-if="releaseDate" class="mt-1 text-sm text-muted">
        {{ releaseDate.slice(0, 4) }}
      </p>
    </div>
    <slot />
  </li>
</template>
