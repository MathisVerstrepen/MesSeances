<script setup lang="ts">
import { ArrowRight } from '@lucide/vue'

defineProps<{
  title: string
  poster?: string | null
  to: string
}>()
</script>

<template>
  <article
    class="flex h-full gap-3 rounded-[0.4rem] border-2 border-ink bg-surface p-3 shadow-[4px_4px_0_var(--color-ink)] sm:gap-4"
  >
    <PosterImage
      :src="poster"
      alt=""
      sizes="64px"
      fallback-variant="icon-only"
      :fallback-icon-size="22"
      class="h-24 w-16 shrink-0 overflow-hidden rounded-sm border border-ink/25 bg-[#e8e6de]"
      image-class="size-full object-cover"
      fallback-class="text-muted"
    />
    <div class="min-w-0 flex-1 self-center">
      <h3 class="editorial-heading text-lg [overflow-wrap:anywhere] sm:text-xl">
        <NuxtLink
          :to="to"
          class="inline-flex min-h-8 items-center gap-2 hover:text-primary hover:underline"
        >
          {{ title }}
          <ArrowRight :size="16" class="shrink-0" aria-hidden="true" />
        </NuxtLink>
      </h3>
      <slot />
    </div>
  </article>
</template>
