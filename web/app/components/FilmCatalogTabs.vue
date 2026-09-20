<script setup lang="ts">
defineProps<{ active: 'showing' | 'upcoming' }>()
</script>

<template>
  <nav
    aria-label="Catalogue des films"
    class="border-b-2 border-ink bg-surface"
  >
    <div class="mx-auto flex max-w-[1440px] gap-6 px-4 sm:px-6 lg:px-10">
      <NuxtLink
        to="/films"
        :aria-current="active === 'showing' ? 'page' : undefined"
        class="inline-flex min-h-14 items-center border-b-4 border-transparent font-mono text-xs font-black uppercase tracking-wide hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink aria-[current=page]:border-primary"
        >En salles</NuxtLink
      >
      <NuxtLink
        to="/films/prochainement"
        :aria-current="active === 'upcoming' ? 'page' : undefined"
        class="inline-flex min-h-14 items-center border-b-4 border-transparent font-mono text-xs font-black uppercase tracking-wide hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink aria-[current=page]:border-primary"
        >Prochainement</NuxtLink
      >
    </div>
  </nav>
</template>
