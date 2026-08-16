<script setup lang="ts">
useHead({ title: 'Crédits — MovieFlow' })
</script>

<template>
  <main class="mx-auto max-w-[1100px] px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10">
    <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Crédits</h1>

    <section class="mt-8 border-t border-line pt-6" aria-labelledby="tmdb-heading">
      <h2 id="tmdb-heading" class="sr-only">The Movie Database</h2>
      <a href="https://www.themoviedb.org" class="inline-block rounded-sm focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-4" target="_blank" rel="noopener noreferrer">
        <img src="/tmdb-logo.svg" alt="The Movie Database (TMDB)" class="h-auto w-28" />
      </a>
      <p class="mt-5 max-w-xl text-sm leading-relaxed text-muted">This product uses the TMDB API but is not endorsed or certified by TMDB.</p>
    </section>
  </main>
</template>
