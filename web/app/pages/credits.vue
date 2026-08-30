<script setup lang="ts">
type CreditBrand = 'UGC' | 'CGR' | 'IMAX' | 'KINEPOLIS' | 'PATHE' | 'DOLBY' | 'SCREENX' | 'LASER_ULTRA' | '4DX'

interface Credit {
  brand: CreditBrand
  name: string
  url: string
}

interface CartographyCredit {
  name: string
  role: string
  url: string
}

const creditSections: Array<{ id: 'operators' | 'technologies'; title: string; credits: Credit[] }> = [
  {
    id: 'operators',
    title: 'Exploitants',
    credits: [
      { brand: 'UGC', name: 'UGC', url: 'https://www.ugc.fr/' },
      { brand: 'KINEPOLIS', name: 'Kinepolis', url: 'https://kinepolis.fr/' },
      { brand: 'PATHE', name: 'Pathé', url: 'https://www.pathe.fr/' },
      { brand: 'CGR', name: 'CGR Cinémas', url: 'https://www.cgrcinemas.fr/' }
    ]
  },
  {
    id: 'technologies',
    title: 'Technologies',
    credits: [
      { brand: 'IMAX', name: 'IMAX', url: 'https://www.imax.com/' },
      { brand: 'DOLBY', name: 'Dolby', url: 'https://www.dolby.com/' },
      { brand: 'SCREENX', name: 'ScreenX', url: 'https://kinepolis.fr/screenx/' },
      { brand: 'LASER_ULTRA', name: 'Laser ULTRA by Kinepolis', url: 'https://kinepolis.fr/laser-ultra/' },
      { brand: '4DX', name: '4DX', url: 'https://kinepolis.fr/4dx/' }
    ]
  }
]

const cartographyCredits: CartographyCredit[] = [
  { name: 'OpenFreeMap', role: 'Styles et ressources cartographiques', url: 'https://openfreemap.org/' },
  { name: 'OpenMapTiles', role: 'Technologie cartographique', url: 'https://openmaptiles.org/' },
  { name: 'OpenStreetMap contributors', role: 'Données cartographiques', url: 'https://www.openstreetmap.org/copyright' }
]

useHead({ title: 'Crédits - MesSeances' })
</script>

<template>
  <main class="bg-canvas">
    <header class="relative mx-auto grid max-w-[1440px] gap-8 overflow-hidden px-4 py-12 after:absolute after:top-8 after:right-[clamp(1.5rem,8vw,8rem)] after:aspect-square after:w-[clamp(3rem,6vw,5rem)] after:rotate-[7deg] after:border-2 after:border-ink after:bg-highlight after:shadow-[5px_5px_0_#27272a] after:content-[''] max-lg:after:top-9 max-lg:after:right-8 max-sm:after:top-6 max-sm:after:right-5 max-sm:after:w-10 max-sm:after:shadow-[3px_3px_0_#27272a] sm:px-6 sm:py-16 lg:grid-cols-[minmax(0,1fr)_18rem] lg:items-end lg:px-10 lg:py-20">
      <div>
        <p class="mb-5 font-mono text-[0.65rem] font-black uppercase tracking-[0.16em]">Attributions · Sources</p>
        <h1 class="text-[clamp(4.8rem,14vw,12rem)] leading-[0.72] font-black tracking-[-0.085em] text-ink uppercase max-sm:text-[clamp(4.4rem,25vw,6rem)]">Crédits</h1>
      </div>
      <ShareButton class="relative z-10 lg:justify-self-end" />
    </header>

    <div class="border-y-2 border-ink bg-[#f8f7f2] [background-image:linear-gradient(rgba(39,39,42,0.075)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.075)_1px,transparent_1px)] [background-size:28px_28px]">
      <div class="mx-auto max-w-[1440px] px-4 py-10 sm:px-6 sm:py-14 lg:px-10 lg:py-16">
        <section class="tmdb-panel grid border-2 border-ink bg-surface shadow-[7px_7px_0_#27272a] lg:grid-cols-[minmax(18rem,0.8fr)_minmax(0,1.2fr)]" aria-labelledby="tmdb-heading">
          <a
            href="https://www.themoviedb.org"
            class="flex min-h-48 items-center justify-center border-b-2 border-ink p-8 hover:bg-[#f1efe8] focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-accent focus-visible:ring-inset lg:border-b-0 lg:border-r-2"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="Site officiel The Movie Database (TMDB), ouverture dans un nouvel onglet"
          >
            <img src="/tmdb-logo.svg" alt="The Movie Database (TMDB)" class="h-auto w-40 sm:w-52" />
          </a>
          <div class="relative flex min-h-48 flex-col justify-between overflow-hidden bg-ink p-6 text-white sm:p-8 lg:p-10">
            <span class="absolute -top-8 -right-8 aspect-square w-32 rotate-12 border-2 border-ink bg-highlight" aria-hidden="true" />
            <div class="relative z-10 mt-5">
              <h2 id="tmdb-heading" class="text-2xl font-black uppercase tracking-[-0.03em] sm:text-3xl">The Movie Database</h2>
              <p class="mt-4 max-w-2xl font-mono text-xs leading-6 text-white">This product uses the TMDB API but is not endorsed or certified by TMDB.</p>
            </div>
          </div>
        </section>

        <section v-for="section in creditSections" :key="section.id" class="mt-14 sm:mt-20" :aria-labelledby="`${section.id}-heading`">
          <header class="mb-5 flex items-end justify-between gap-5 border-b-2 border-ink pb-4">
            <h2 :id="`${section.id}-heading`" class="text-2xl font-black uppercase tracking-[-0.03em] text-ink sm:text-4xl">{{ section.title }}</h2>
          </header>

          <div
            class="provider-grid grid border-l-2 border-t-2 border-ink sm:grid-cols-2"
            :class="section.id === 'technologies' ? 'xl:grid-cols-5' : 'xl:grid-cols-4'"
          >
            <section v-for="credit in section.credits" :key="credit.brand" class="flex min-w-0 flex-col border-r-2 border-b-2 border-ink bg-surface [&:nth-child(3n+1)_.provider-link]:shadow-[inset_0_-4px_0_var(--color-highlight)] [&:nth-child(3n+2)_.provider-link]:shadow-[inset_0_-4px_0_#ffcf3f] [&:nth-child(3n)_.provider-link]:shadow-[inset_0_-4px_0_var(--color-accent)]" :aria-labelledby="`credit-${credit.brand}`">
              <div class="flex items-center justify-between border-b-2 border-ink bg-[#f1efe8] px-4 py-3">
                <h3 :id="`credit-${credit.brand}`" class="font-mono text-[0.65rem] font-black uppercase tracking-[0.12em] text-ink">{{ credit.name }}</h3>
              </div>
              <a
                :href="credit.url"
                class="provider-link flex min-h-40 items-center justify-center px-6 py-8 hover:bg-[#f1efe8] focus-visible:z-10 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-accent focus-visible:ring-inset"
                target="_blank"
                rel="noopener noreferrer"
                :aria-label="`Site officiel ${credit.name}, ouverture dans un nouvel onglet`"
              >
                <span class="provider-logo flex h-16 w-full items-center justify-center">
                  <BrandLogo :brand="credit.brand" variant="display" decorative />
                </span>
              </a>
              <p class="mt-auto border-t-2 border-ink px-4 py-5 text-xs leading-5 text-ink">La marque et le logo {{ credit.name }} appartiennent à leurs propriétaires respectifs. Leur présence n’implique ni affiliation avec MesSeances, ni approbation de MesSeances.</p>
            </section>
          </div>
        </section>

        <section class="mt-14 sm:mt-20" aria-labelledby="cartography-heading">
          <header class="mb-5 flex items-end justify-between gap-5 border-b-2 border-ink pb-4">
            <h2 id="cartography-heading" class="text-2xl font-black uppercase tracking-[-0.03em] text-ink sm:text-4xl">Cartographie</h2>
          </header>

          <div class="grid border-l-2 border-t-2 border-ink sm:grid-cols-2 xl:grid-cols-3">
            <a
              v-for="credit in cartographyCredits"
              :key="credit.name"
              :href="credit.url"
              class="flex min-h-40 min-w-0 flex-col border-r-2 border-b-2 border-ink bg-surface p-5 hover:bg-[#f1efe8] focus-visible:z-10 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-accent focus-visible:ring-inset [&:nth-child(3n+1)]:shadow-[inset_0_-4px_0_var(--color-highlight)] [&:nth-child(3n+2)]:shadow-[inset_0_-4px_0_#ffcf3f] [&:nth-child(3n)]:shadow-[inset_0_-4px_0_var(--color-accent)] sm:last:col-span-2 sm:p-6 xl:last:col-span-1"
              target="_blank"
              rel="noopener noreferrer"
            >
              <span class="font-mono text-[0.65rem] font-black uppercase tracking-[0.12em] text-ink">Attribution</span>
              <span class="mt-8 text-xl font-black uppercase leading-tight tracking-[-0.03em] text-ink sm:text-2xl">{{ credit.name }}</span>
              <span class="mt-auto pt-6 text-xs leading-5 text-ink">{{ credit.role }}<span class="sr-only">, ouverture dans un nouvel onglet</span></span>
            </a>
          </div>
        </section>
      </div>
    </div>
  </main>
</template>

<style scoped>
.provider-logo :deep(img) {
  width: auto !important;
  max-width: min(100%, 13rem) !important;
  height: 4rem !important;
  object-fit: contain;
}

</style>
