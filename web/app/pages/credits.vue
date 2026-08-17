<script setup lang="ts">
type CreditBrand = 'UGC' | 'IMAX' | 'KINEPOLIS' | '3D' | 'DOLBY' | 'SCREENX' | 'LASER_ULTRA' | '4DX'

const credits: Array<{ brand: CreditBrand; name: string; url: string }> = [
  { brand: 'UGC', name: 'UGC', url: 'https://www.ugc.fr/' },
  { brand: 'KINEPOLIS', name: 'Kinepolis', url: 'https://kinepolis.fr/' },
  { brand: 'IMAX', name: 'IMAX', url: 'https://www.imax.com/' },
  { brand: '3D', name: '3D', url: 'https://kinepolis.fr/3d/' },
  { brand: 'DOLBY', name: 'Dolby', url: 'https://www.dolby.com/' },
  { brand: 'SCREENX', name: 'ScreenX', url: 'https://kinepolis.fr/screenx/' },
  { brand: 'LASER_ULTRA', name: 'Laser ULTRA by Kinepolis', url: 'https://kinepolis.fr/laser-ultra/' },
  { brand: '4DX', name: '4DX', url: 'https://kinepolis.fr/4dx/' }
]

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

    <div class="mt-8 grid gap-x-10 gap-y-8 border-t border-line pt-8 sm:grid-cols-2">
      <section v-for="credit in credits" :key="credit.brand" :aria-labelledby="`credit-${credit.brand}`">
        <h2 :id="`credit-${credit.brand}`" class="sr-only">{{ credit.name }}</h2>
        <a :href="credit.url" class="inline-flex min-h-24 items-center rounded-sm focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-4" target="_blank" rel="noopener noreferrer" :aria-label="`Site officiel ${credit.name}, ouverture dans un nouvel onglet`">
          <BrandLogo :brand="credit.brand" variant="display" decorative />
        </a>
        <p class="mt-4 max-w-md text-sm leading-relaxed text-muted">La marque et le logo {{ credit.name }} appartiennent à leurs propriétaires respectifs. Leur présence n’implique ni affiliation avec MovieFlow, ni approbation de MovieFlow.</p>
      </section>
    </div>
  </main>
</template>
