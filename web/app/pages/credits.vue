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

    <div class="mt-8 grid gap-8 border-t border-line pt-8 sm:grid-cols-2">
      <section aria-labelledby="ugc-heading">
        <h2 id="ugc-heading" class="sr-only">UGC</h2>
        <a href="https://www.ugc.fr/" class="inline-flex min-h-24 items-center rounded-sm focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-4" target="_blank" rel="noopener noreferrer" aria-label="Site officiel UGC, ouverture dans un nouvel onglet">
          <BrandLogo brand="UGC" variant="display" decorative />
        </a>
        <p class="mt-4 max-w-md text-sm leading-relaxed text-muted"><BrandedText text="UGC détient la marque et le logo présentés ci-dessus. Leur présence n’implique ni affiliation avec MovieFlow, ni approbation de MovieFlow." /></p>
      </section>

      <section aria-labelledby="imax-heading">
        <h2 id="imax-heading" class="sr-only">IMAX</h2>
        <a href="https://www.imax.com/" class="inline-flex min-h-24 items-center rounded-sm focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-4" target="_blank" rel="noopener noreferrer" aria-label="Site officiel IMAX, ouverture dans un nouvel onglet">
          <BrandLogo brand="IMAX" variant="display" decorative />
        </a>
        <p class="mt-4 max-w-md text-sm leading-relaxed text-muted"><BrandedText text="IMAX Corporation détient la marque et le logo présentés ci-dessus. Leur présence n’implique ni affiliation avec MovieFlow, ni approbation de MovieFlow." /></p>
      </section>
    </div>
  </main>
</template>
