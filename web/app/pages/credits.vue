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

useHead({ title: 'Crédits — MesSeances' })
</script>

<template>
  <main class="credits-page">
    <header class="credits-hero mx-auto grid max-w-[1440px] gap-8 px-4 py-12 sm:px-6 sm:py-16 lg:grid-cols-[minmax(0,1fr)_18rem] lg:items-end lg:px-10 lg:py-20">
      <div>
        <p class="utility-label mb-5">Attributions · Sources</p>
        <h1 class="credits-title text-ink">Crédits</h1>
      </div>
    </header>

    <div class="credits-canvas border-y-2 border-ink">
      <div class="mx-auto max-w-[1440px] px-4 py-10 sm:px-6 sm:py-14 lg:px-10 lg:py-16">
        <section class="tmdb-panel grid border-2 border-ink bg-surface shadow-[7px_7px_0_#27272a] lg:grid-cols-[minmax(18rem,0.8fr)_minmax(0,1.2fr)]" aria-labelledby="tmdb-heading">
          <a
            href="https://www.themoviedb.org"
            class="tmdb-link flex min-h-48 items-center justify-center border-b-2 border-ink p-8 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-accent focus-visible:ring-inset lg:border-b-0 lg:border-r-2"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="Site officiel The Movie Database (TMDB), ouverture dans un nouvel onglet"
          >
            <img src="/tmdb-logo.svg" alt="The Movie Database (TMDB)" class="h-auto w-40 sm:w-52" />
          </a>
          <div class="relative flex min-h-48 flex-col justify-between overflow-hidden bg-ink p-6 text-white sm:p-8 lg:p-10">
            <span class="tmdb-accent" aria-hidden="true" />
            <div class="relative z-10 mt-5">
              <h2 id="tmdb-heading" class="text-2xl font-black uppercase tracking-[-0.03em] sm:text-3xl">The Movie Database</h2>
              <p class="mt-4 max-w-2xl font-mono text-xs leading-6 text-white">This product uses the TMDB API but is not endorsed or certified by TMDB.</p>
            </div>
          </div>
        </section>

        <section class="mt-14 sm:mt-20" aria-labelledby="providers-heading">
          <header class="mb-5 flex items-end justify-between gap-5 border-b-2 border-ink pb-4">
            <h2 id="providers-heading" class="text-2xl font-black uppercase tracking-[-0.03em] text-ink sm:text-4xl">Exploitants & technologies</h2>
          </header>

          <div class="provider-grid grid border-l-2 border-t-2 border-ink sm:grid-cols-2 xl:grid-cols-4">
            <section v-for="credit in credits" :key="credit.brand" class="provider-item flex min-w-0 flex-col border-b-2 border-r-2 border-ink bg-surface" :aria-labelledby="`credit-${credit.brand}`">
              <div class="flex items-center justify-between border-b-2 border-ink bg-[#f1efe8] px-4 py-3">
                <h3 :id="`credit-${credit.brand}`" class="font-mono text-[0.65rem] font-black uppercase tracking-[0.12em] text-ink">{{ credit.name }}</h3>
              </div>
              <a
                :href="credit.url"
                class="provider-link flex min-h-40 items-center justify-center px-6 py-8 focus-visible:z-10 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-accent focus-visible:ring-inset"
                target="_blank"
                rel="noopener noreferrer"
                :aria-label="`Site officiel ${credit.name}, ouverture dans un nouvel onglet`"
              >
                <BrandLogo :brand="credit.brand" variant="display" decorative />
              </a>
              <p class="mt-auto border-t-2 border-ink px-4 py-5 text-xs leading-5 text-ink">La marque et le logo {{ credit.name }} appartiennent à leurs propriétaires respectifs. Leur présence n’implique ni affiliation avec MesSeances, ni approbation de MesSeances.</p>
            </section>
          </div>
        </section>
      </div>
    </div>
  </main>
</template>

<style scoped>
.credits-page {
  background: #fcfaf8;
}

.credits-hero {
  position: relative;
  overflow: hidden;
}

.credits-hero::after {
  position: absolute;
  right: clamp(1.5rem, 8vw, 8rem);
  top: 2rem;
  width: clamp(3rem, 6vw, 5rem);
  aspect-ratio: 1;
  border: 2px solid #27272a;
  background: var(--color-highlight);
  box-shadow: 5px 5px 0 #27272a;
  content: '';
  transform: rotate(7deg);
}

.utility-label {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.16em;
  text-transform: uppercase;
}

.credits-title {
  font-size: clamp(4.8rem, 14vw, 12rem);
  font-weight: 900;
  letter-spacing: -0.085em;
  line-height: 0.72;
  text-transform: uppercase;
}

.hero-aside {
  position: relative;
  z-index: 1;
}

.credits-canvas {
  background-color: #f8f7f2;
  background-image:
    linear-gradient(rgba(39, 39, 42, 0.075) 1px, transparent 1px),
    linear-gradient(90deg, rgba(39, 39, 42, 0.075) 1px, transparent 1px);
  background-size: 28px 28px;
}

.tmdb-accent {
  position: absolute;
  right: -2rem;
  top: -2rem;
  width: 8rem;
  aspect-ratio: 1;
  border: 2px solid #27272a;
  background: var(--color-highlight);
  transform: rotate(12deg);
}

.tmdb-link:hover,
.provider-link:hover {
  background: #f1efe8;
}

.provider-item:nth-child(3n + 1) .provider-link {
  box-shadow: inset 0 -4px 0 var(--color-highlight);
}

.provider-item:nth-child(3n + 2) .provider-link {
  box-shadow: inset 0 -4px 0 #ffcf3f;
}

.provider-item:nth-child(3n) .provider-link {
  box-shadow: inset 0 -4px 0 var(--color-accent);
}

@media (max-width: 1023px) {
  .credits-hero::after {
    right: 2rem;
    top: 2.25rem;
  }
}

@media (max-width: 639px) {
  .credits-hero::after {
    width: 2.5rem;
    right: 1.25rem;
    top: 1.5rem;
    box-shadow: 3px 3px 0 #27272a;
  }

  .credits-title {
    font-size: clamp(4.4rem, 25vw, 6rem);
  }
}
</style>
