<script setup lang="ts">
import { ArrowRight } from '@lucide/vue'

defineProps<{
  title: string
  loading: boolean
  action?: { to: string; label: string }
}>()

const headingId = useId()
</script>

<template>
  <section
    class="border-b-2 border-ink text-ink"
    :aria-labelledby="headingId"
    :aria-busy="loading"
  >
    <div class="mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10">
      <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <h2
          :id="headingId"
          class="editorial-title text-[clamp(1.75rem,4vw,2.75rem)]"
        >
          {{ title }}<span class="text-primary">.</span>
        </h2>
        <NuxtLink
          v-if="action"
          :to="action.to"
          class="inline-flex min-h-11 items-center font-mono text-xs font-bold tracking-wider uppercase hover:text-primary"
        >
          <span
            class="inline-flex items-center gap-1.5 border-b-2 border-current pb-1"
          >
            {{ action.label }} <ArrowRight :size="14" aria-hidden="true" />
          </span>
        </NuxtLink>
      </div>
      <div class="mt-5">
        <div v-if="loading">
          <p class="sr-only" role="status">Chargement…</p>
          <div aria-hidden="true" class="grid gap-4 lg:grid-cols-2">
            <div
              v-for="index in 2"
              :key="index"
              class="h-28 rounded-[0.4rem] border-2 border-ink/25 bg-[#e8e6de]"
            />
          </div>
        </div>
        <slot v-else />
      </div>
    </div>
  </section>
</template>
