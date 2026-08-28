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
    <div class="footer-grid">
      <div class="mx-auto grid max-w-[1440px] gap-12 px-4 py-12 sm:px-6 sm:py-16 md:grid-cols-[1.1fr_2fr] lg:gap-20 lg:px-10 lg:py-20">
        <div class="flex items-start">
          <NuxtLink to="/" class="inline-flex w-fit items-center gap-2 font-black tracking-[-0.04em]" aria-label="MesSeances, accueil">
            <span class="grid size-8 place-items-center border-2 border-ink bg-surface font-mono text-xs shadow-[3px_3px_0_#27272a]" aria-hidden="true">MS</span>
            <span>MesSeances<span class="text-primary">.</span></span>
          </NuxtLink>
        </div>

        <nav aria-label="Navigation de pied de page" class="grid grid-cols-2 gap-x-8 gap-y-10 sm:grid-cols-3">
          <div>
            <h2 class="footer-heading">Explorer</h2>
            <ul class="mt-5 space-y-3">
              <li v-for="link in explorerLinks" :key="link.to">
                <NuxtLink :to="link.to" class="footer-link">{{ link.label }}</NuxtLink>
              </li>
            </ul>
          </div>

          <div>
            <h2 class="footer-heading">À propos</h2>
            <ul class="mt-5 space-y-3">
              <li><NuxtLink to="/credits" class="footer-link">Crédits &amp; sources</NuxtLink></li>
              <li><NuxtLink to="/mentions-legales" class="footer-link">Mentions légales</NuxtLink></li>
              <li><NuxtLink to="/confidentialite" class="footer-link">Confidentialité</NuxtLink></li>
              <li>
                <a
                  href="https://github.com/MathisVerstrepen/MesSeances"
                  class="footer-link"
                  target="_blank"
                  rel="noopener noreferrer"
                  aria-label="Voir le projet MesSeances sur GitHub (nouvel onglet)"
                >Projet GitHub</a>
              </li>
            </ul>
          </div>

          <div>
            <h2 class="footer-heading">Administration</h2>
            <ul class="mt-5 space-y-3">
              <li><NuxtLink to="/admin" class="footer-link">Espace admin</NuxtLink></li>
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
              class="meta-link"
              target="_blank"
              rel="noopener noreferrer"
              aria-label="Profil GitHub de Mathis Verstrepen (nouvel onglet)"
            >Mathis Verstrepen</a>
          </p>
        </div>
        <button type="button" class="return-top group inline-flex w-fit items-center gap-2 border-b-2 border-ink pb-1" aria-label="Retourner en haut de la page" @click="scrollToTop">
          Retour en haut
          <ArrowUp :size="15" class="transition-transform group-hover:-translate-y-0.5" aria-hidden="true" />
        </button>
      </div>
    </div>

    <div class="wordmark-wrap relative mx-auto max-w-[1440px] px-4 pt-10 sm:px-6 sm:pt-14 lg:px-10">
      <span class="accent-shape" aria-hidden="true"></span>
      <p class="closing-wordmark" aria-hidden="true">MES SÉANCES</p>
    </div>
  </footer>
</template>

<style scoped>
.footer-grid {
  background-image:
    linear-gradient(rgba(39, 39, 42, 0.075) 1px, transparent 1px),
    linear-gradient(90deg, rgba(39, 39, 42, 0.075) 1px, transparent 1px);
  background-size: 32px 32px;
}

.footer-heading {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 800;
  letter-spacing: 0.16em;
  text-transform: uppercase;
}

.footer-link {
  display: inline-block;
  border-bottom: 2px solid transparent;
  font-size: 0.95rem;
  font-weight: 800;
  line-height: 1.35;
  transition: border-color 150ms ease, color 150ms ease;
}

.footer-link:hover {
  border-color: currentColor;
  color: #991b1b;
}

.meta-link {
  border-bottom: 1px solid currentColor;
  transition: color 150ms ease;
}

.meta-link:hover {
  color: #991b1b;
}

.return-top {
  transition: color 150ms ease;
}

.return-top:hover {
  color: #991b1b;
}

.wordmark-wrap {
  height: clamp(6rem, 13vw, 11rem);
  overflow: hidden;
}

.closing-wordmark {
  position: relative;
  z-index: 1;
  white-space: nowrap;
  font-size: clamp(4.2rem, 14.2vw, 12.75rem);
  font-weight: 950;
  line-height: 0.7;
  letter-spacing: -0.085em;
}

.accent-shape {
  position: absolute;
  z-index: 2;
  top: 1.2rem;
  right: 12%;
  width: clamp(2.5rem, 5vw, 4.5rem);
  aspect-ratio: 1;
  transform: rotate(8deg);
  border: 2px solid #27272a;
  background: var(--color-highlight);
  box-shadow: 5px 5px 0 #27272a;
}

@media (prefers-reduced-motion: reduce) {
  .return-top svg {
    transition: none;
  }
}
</style>
