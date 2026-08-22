<script setup lang="ts">
import { AlertTriangle, Building2, Film, LoaderCircle, MapPin, RefreshCw } from '@lucide/vue'
import type { CityDetailResponse } from '~/types/api'
import { safePosterUrl } from '~/utils/safeImageUrl'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const route = useRoute()
const api = useMesSeancesApi()
const detail = ref<CityDetailResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
const notFound = ref(false)
const failedPosterSlugs = ref(new Set<string>())
const posterList = ref<HTMLElement | null>(null)
let requestId = 0

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? value[0] ?? '' : value ?? ''
})

function isNotFoundError(cause: unknown): boolean {
  return getApiErrorStatus(cause) === 404 || getApiErrorCode(cause) === 'not_found'
}

async function fetchCity() {
  try {
    return { kind: 'success' as const, detail: await api.city(slug.value), errorMessage: '' }
  } catch (error) {
    if (isNotFoundError(error)) return { kind: 'not-found' as const, detail: null, errorMessage: '' }
    return { kind: 'upstream-error' as const, detail: null, errorMessage: getFrenchApiError(error) }
  }
}

const initial = await useAsyncData(`city:${slug.value}`, fetchCity)
const initialState = initial.data.value
detail.value = initialState?.detail ?? null
notFound.value = initialState?.kind === 'not-found'
errorMessage.value = initialState?.errorMessage ?? ''
pending.value = false
if (import.meta.server && initialState?.kind !== 'success') {
  const event = useRequestEvent()
  if (event) setResponseStatus(event, initialState?.kind === 'not-found' ? 404 : 502)
}

async function loadCity() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  notFound.value = false
  const state = await fetchCity()
  if (currentRequest !== requestId) return
  failedPosterSlugs.value = new Set()
  detail.value = state.detail
  notFound.value = state.kind === 'not-found'
  errorMessage.value = state.errorMessage
  pending.value = false
  await nextTick()
  detectFailedPosters()
}

function posterAvailable(slug: string, url: string | null): boolean {
  return !failedPosterSlugs.value.has(slug) && Boolean(safePosterUrl(url))
}

function markPosterFailed(slug: string) {
  failedPosterSlugs.value = new Set([...failedPosterSlugs.value, slug])
}

function detectFailedPosters() {
  for (const image of posterList.value?.querySelectorAll<HTMLImageElement>('img[data-poster-slug]') ?? []) {
    if (image.complete && image.naturalWidth === 0 && image.dataset.posterSlug) markPosterFailed(image.dataset.posterSlug)
  }
}

watch(slug, () => void loadCity())
onMounted(() => nextTick(detectFailedPosters))

const config = useRuntimeConfig()
const canonicalUrl = computed(() => absoluteSiteUrl(config.public.siteUrl, `/ville/${encodeURIComponent(slug.value)}/cinemas`))
const pageTitle = computed(() => detail.value ? `Cinémas à ${detail.value.city.name} - MesSeances` : 'Cinémas par ville - MesSeances')
const pageDescription = computed(() => detail.value
  ? `Découvrez les cinémas et films actuellement programmés à ${detail.value.city.name}.`
  : 'Découvrez les cinémas et films actuellement programmés dans cette ville.')
const robots = computed(() => detail.value && !pending.value && !errorMessage.value && !notFound.value && Object.keys(route.query).length === 0 ? 'index,follow' : 'noindex,follow')

useSeoMeta({
  robots,
  title: pageTitle,
  description: pageDescription,
  ogTitle: pageTitle,
  ogDescription: pageDescription,
  ogUrl: canonicalUrl,
  ogType: 'website'
})
useHead(() => ({ link: [{ rel: 'canonical', href: canonicalUrl.value }] }))
</script>

<template>
  <main class="city-page mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-14">
    <div v-if="pending && !detail" class="city-state" role="status" aria-live="polite"><LoaderCircle :size="34" class="animate-spin" aria-hidden="true" /><p>Chargement de la ville…</p></div>
    <div v-else-if="notFound" class="city-state" role="alert"><MapPin :size="36" aria-hidden="true" /><h1>Ville introuvable</h1><p>Cette ville n’est pas disponible dans la programmation actuelle.</p><NuxtLink to="/cinemas" class="city-action">Voir les cinémas</NuxtLink></div>
    <div v-else-if="errorMessage && !detail" class="city-state" role="alert"><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /><h1>Impossible de charger cette ville</h1><p>{{ errorMessage }}</p><button type="button" class="city-action" @click="loadCity"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></div>

    <template v-else-if="detail">
      <nav class="breadcrumb" aria-label="Fil d’Ariane"><ol class="flex flex-wrap items-center gap-2"><li><NuxtLink to="/">Accueil</NuxtLink></li><li aria-hidden="true">/</li><li><NuxtLink to="/cinemas">Cinémas</NuxtLink></li><li aria-hidden="true">/</li><li aria-current="page">{{ detail.city.name }}</li></ol></nav>
      <header class="border-2 border-ink bg-surface p-5 shadow-[8px_8px_0_#27272a] sm:p-8">
        <div class="flex items-start justify-between gap-4">
          <p class="utility-label pt-1">Cinémas · ville</p>
          <ShareButton class="shrink-0" />
        </div>
        <h1 class="mt-4 break-words text-[clamp(3rem,10vw,8rem)] font-black uppercase leading-[0.8] tracking-[-0.08em]">{{ detail.city.name }}<span class="text-primary">.</span></h1>
        <p class="utility-label mt-6">{{ detail.theaters.length }} cinéma{{ detail.theaters.length === 1 ? '' : 's' }} · {{ detail.movies.length }} film{{ detail.movies.length === 1 ? '' : 's' }}</p>
      </header>

      <section class="mt-12" aria-labelledby="city-cinemas-heading">
        <div class="border-b-2 border-ink pb-4"><h2 id="city-cinemas-heading" class="text-4xl font-black tracking-[-0.05em] sm:text-5xl">Cinémas</h2></div>
        <div v-if="detail.theaters.length === 0" class="city-state mt-8"><Building2 :size="36" aria-hidden="true" /><h3>Aucun cinéma disponible</h3><p>La programmation actuelle ne contient aucun cinéma dans cette ville.</p></div>
        <ul v-else class="mt-8 grid gap-5 md:grid-cols-2">
          <li v-for="theater in detail.theaters" :key="theater.id" class="border-2 border-ink bg-surface p-5 shadow-[6px_6px_0_#27272a]">
            <h3 class="text-2xl font-black tracking-[-0.04em]"><NuxtLink :to="`/cinema/${encodeURIComponent(theater.slug)}`" class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary"><BrandedText :text="theater.name" /></NuxtLink></h3>
            <p v-if="theater.address || theater.postal_code" class="mt-3 break-words text-sm font-semibold leading-6"><span v-if="theater.address">{{ theater.address }}<br /></span>{{ theater.postal_code }} {{ theater.city }}</p>
          </li>
        </ul>
      </section>

      <section class="mt-14" aria-labelledby="city-films-heading">
        <div class="border-b-2 border-ink pb-4"><h2 id="city-films-heading" class="text-4xl font-black tracking-[-0.05em] sm:text-5xl">Films à l’affiche</h2></div>
        <div v-if="detail.movies.length === 0" class="city-state mt-8"><Film :size="36" aria-hidden="true" /><h3>Aucun film disponible</h3><p>Aucun film n’est programmé dans cette ville sur la période actuelle.</p></div>
        <ul v-else ref="posterList" class="mt-8 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
          <li v-for="movie in detail.movies" :key="movie.slug" class="min-w-0">
            <NuxtLink :to="`/film/${encodeURIComponent(movie.slug)}`" class="group block h-full border-2 border-ink bg-surface shadow-[5px_5px_0_#27272a] transition-transform hover:-translate-y-1">
              <div class="aspect-[2/3] overflow-hidden border-b-2 border-ink bg-[#e8e6de]">
                <img v-if="posterAvailable(movie.slug, movie.poster_url)" :src="safePosterUrl(movie.poster_url)!" :alt="`Affiche de ${movie.title}`" :data-poster-slug="movie.slug" class="size-full object-cover" @error="markPosterFailed(movie.slug)" />
                <div v-else class="grid size-full place-items-center text-muted"><Film :size="32" aria-hidden="true" /></div>
              </div>
              <div class="p-3"><h3 class="break-words text-sm font-black leading-tight group-hover:text-primary sm:text-base">{{ movie.title }}</h3><p class="utility-label mt-2">{{ movie.runtime_minutes }} min</p></div>
            </NuxtLink>
          </li>
        </ul>
      </section>
    </template>
  </main>
</template>

<style scoped>
.city-page { min-height: 70vh; background-color: #f8f7f2; background-image: linear-gradient(rgba(39,39,42,.07) 1px,transparent 1px),linear-gradient(90deg,rgba(39,39,42,.07) 1px,transparent 1px); background-size: 28px 28px; }
.city-state { margin-inline: auto; display: flex; min-height: 20rem; max-width: 48rem; flex-direction: column; align-items: center; justify-content: center; gap: 1rem; border: 2px solid #27272a; background: #fff; padding: 2rem; text-align: center; font-weight: 700; box-shadow: 8px 8px 0 #27272a; }
.city-state h1,.city-state h3 { font-size: 1.5rem; font-weight: 900; }
.city-action { display: inline-flex; min-height: 2.75rem; align-items: center; justify-content: center; gap: .5rem; border: 2px solid #27272a; background: #27272a; padding: .65rem .9rem; color: #fff; font-family: ui-monospace,monospace; font-size: .7rem; font-weight: 900; text-transform: uppercase; }
.breadcrumb,.utility-label { font-family: ui-monospace,monospace; font-size: .68rem; font-weight: 900; letter-spacing: .1em; text-transform: uppercase; }
.breadcrumb { margin-bottom: 1.5rem; color: var(--color-muted); }
.breadcrumb a:hover { color: var(--color-primary); }
</style>
