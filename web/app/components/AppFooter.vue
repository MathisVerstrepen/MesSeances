<script setup lang="ts">
import { ArrowUp } from '@lucide/vue'

const currentYear = new Date().getFullYear()
const configuredVersion = useRuntimeConfig().public.appVersion.trim()
const appVersion = configuredVersion || 'dev'

const explorerLinks = [
  { to: '/recherche', label: 'Trouver une séance' },
  { to: '/planning', label: 'Planning' },
  { to: '/films', label: 'Films' },
  { to: '/cinemas', label: 'Cinémas' }
]

function scrollToTop() {
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  window.scrollTo({ top: 0, behavior: reducedMotion ? 'auto' : 'smooth' })
}
</script>

<template>
  <footer class="editorial-footer relative overflow-hidden border-t-2 border-ink bg-[#f8f7f2] text-ink">
    <div class="[background-image:linear-gradient(rgba(39,39,42,0.075)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.075)_1px,transparent_1px)] [background-size:32px_32px]">
      <div class="mx-auto grid max-w-[1440px] gap-12 px-4 py-12 sm:px-6 sm:py-16 md:grid-cols-[1.1fr_2fr] lg:gap-20 lg:px-10 lg:py-20">
        <div class="flex items-start">
          <NuxtLink to="/" class="inline-flex w-fit items-center gap-2 font-black tracking-[-0.04em]" aria-label="MesSeances, accueil">
            <span class="grid size-8 place-items-center border-2 border-ink bg-surface font-mono text-xs shadow-[3px_3px_0_#27272a]" aria-hidden="true">MS</span>
            <span>MesSeances<span class="text-primary">.</span></span>
          </NuxtLink>
        </div>

        <nav aria-label="Navigation de pied de page" class="grid grid-cols-2 gap-x-8 gap-y-10 sm:grid-cols-3">
          <div>
            <h2 class="font-mono text-[0.68rem] font-extrabold uppercase tracking-[0.16em]">Explorer</h2>
            <ul class="mt-5 space-y-3">
              <li v-for="link in explorerLinks" :key="link.to">
                <NuxtLink :to="link.to" class="inline-block border-b-2 border-transparent text-[0.95rem] font-extrabold leading-[1.35] [transition:border-color_150ms_ease,color_150ms_ease] hover:border-current hover:text-primary">{{ link.label }}</NuxtLink>
              </li>
            </ul>
          </div>

          <div>
            <h2 class="font-mono text-[0.68rem] font-extrabold uppercase tracking-[0.16em]">À propos</h2>
            <ul class="mt-5 space-y-3">
              <li><NuxtLink to="/credits" class="inline-block border-b-2 border-transparent text-[0.95rem] font-extrabold leading-[1.35] [transition:border-color_150ms_ease,color_150ms_ease] hover:border-current hover:text-primary">Crédits &amp; sources</NuxtLink></li>
              <li><NuxtLink to="/mentions-legales" class="inline-block border-b-2 border-transparent text-[0.95rem] font-extrabold leading-[1.35] [transition:border-color_150ms_ease,color_150ms_ease] hover:border-current hover:text-primary">Mentions légales</NuxtLink></li>
              <li><NuxtLink to="/confidentialite" class="inline-block border-b-2 border-transparent text-[0.95rem] font-extrabold leading-[1.35] [transition:border-color_150ms_ease,color_150ms_ease] hover:border-current hover:text-primary">Confidentialité</NuxtLink></li>
              <li>
                <a
                  href="https://github.com/MathisVerstrepen/MesSeances"
                  class="inline-block border-b-2 border-transparent text-[0.95rem] font-extrabold leading-[1.35] [transition:border-color_150ms_ease,color_150ms_ease] hover:border-current hover:text-primary"
                  target="_blank"
                  rel="noopener noreferrer"
                  aria-label="Voir le projet MesSeances sur GitHub (nouvel onglet)"
                >Projet GitHub</a>
              </li>
            </ul>
          </div>

          <div>
            <h2 class="font-mono text-[0.68rem] font-extrabold uppercase tracking-[0.16em]">Administration</h2>
            <ul class="mt-5 space-y-3">
              <li><NuxtLink to="/admin" class="inline-block border-b-2 border-transparent text-[0.95rem] font-extrabold leading-[1.35] [transition:border-color_150ms_ease,color_150ms_ease] hover:border-current hover:text-primary">Espace admin</NuxtLink></li>
            </ul>
          </div>
        </nav>
      </div>
    </div>

    <div class="border-y-2 border-ink bg-surface">
      <div class="mx-auto flex max-w-[1440px] flex-col gap-4 px-4 py-4 font-mono text-[10px] font-bold uppercase tracking-[0.1em] sm:flex-row sm:items-center sm:justify-between sm:px-6 lg:px-10">
        <div class="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-6">
          <p>© {{ currentYear }} MesSeances</p>
          <p>Version {{ appVersion }}</p>
          <p>
            Made with ❤️ by
            <a
              href="https://github.com/MathisVerstrepen"
              class="border-b border-current [transition:color_150ms_ease] hover:text-primary"
              target="_blank"
              rel="noopener noreferrer"
              aria-label="Profil GitHub de Mathis Verstrepen (nouvel onglet)"
            >Mathis Verstrepen</a>
          </p>
        </div>
        <button type="button" class="group inline-flex w-fit items-center gap-2 border-b-2 border-ink pb-1 [transition:color_150ms_ease] hover:text-primary" aria-label="Retourner en haut de la page" @click="scrollToTop">
          Retour en haut
          <ArrowUp :size="15" class="transition-transform group-hover:-translate-y-0.5 motion-reduce:transition-none" aria-hidden="true" />
        </button>
      </div>
    </div>

    <div class="relative mx-auto h-[clamp(6rem,13vw,11rem)] max-w-[1440px] overflow-hidden px-4 pt-10 sm:px-6 sm:pt-14 lg:px-10">
      <span class="absolute top-[1.2rem] right-[12%] z-[2] aspect-square w-[clamp(2.5rem,5vw,4.5rem)] rotate-[8deg] border-2 border-ink bg-highlight shadow-[5px_5px_0_#27272a]" aria-hidden="true"></span>
      <p class="relative z-[1] whitespace-nowrap text-[clamp(4.2rem,14.2vw,12.75rem)] leading-[0.7] font-[950] tracking-[-0.085em]" aria-hidden="true">MES SÉANCES</p>
    </div>
  </footer>
</template>
