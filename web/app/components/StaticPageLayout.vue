<script setup lang="ts">
defineProps<{
  eyebrow: string
  title: string
}>()
</script>

<template>
  <main class="bg-canvas">
    <header class="relative mx-auto grid max-w-[1440px] gap-8 overflow-hidden px-4 py-12 after:absolute after:top-8 after:right-[clamp(1.5rem,8vw,8rem)] after:aspect-square after:w-[clamp(3rem,6vw,5rem)] after:rotate-[7deg] after:border-2 after:border-ink after:bg-highlight after:shadow-[5px_5px_0_#27272a] after:content-[''] max-lg:after:top-9 max-lg:after:right-8 max-sm:after:top-6 max-sm:after:right-5 max-sm:after:w-10 max-sm:after:shadow-[3px_3px_0_#27272a] sm:px-6 sm:py-16 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end lg:px-10 lg:py-20">
      <div class="min-w-0">
        <p class="mb-5 font-mono text-[0.65rem] font-black uppercase tracking-[0.16em]">{{ eyebrow }}</p>
        <h1 class="relative z-[1] max-w-6xl text-[clamp(3.25rem,9vw,8.5rem)] leading-[0.82] font-black tracking-[-0.075em] text-ink uppercase [overflow-wrap:anywhere] max-sm:text-[clamp(3rem,17vw,5rem)]">{{ title }}</h1>
      </div>
      <div v-if="$slots['header-actions']" class="relative z-10 flex flex-wrap items-center justify-end gap-3 lg:justify-self-end">
        <slot name="header-actions" />
      </div>
    </header>

    <div class="border-y-2 border-ink bg-[#f8f7f2] [background-image:linear-gradient(rgba(39,39,42,0.075)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.075)_1px,transparent_1px)] [background-size:28px_28px]">
      <slot />
    </div>
  </main>
</template>
