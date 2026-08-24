<script setup lang="ts">
import { AlertTriangle, Building2, Film, LoaderCircle, MapPin, RefreshCw } from '@lucide/vue'
import type { CityDetailResponse } from '~/types/api'
import { cityDescription } from '~/utils/entityDescriptions'
import { serializeJsonLd, type JsonLdNode } from '~/utils/jsonLd'
import { absoluteSiteUrl } from '~/utils/siteUrl'

const route = useRoute()
const api = useMesSeancesApi()
const detail = ref<CityDetailResponse | null>(null)
const pending = ref(true)
const errorMessage = ref('')
const notFound = ref(false)
const posterVersion = ref(0)
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
  if (state.kind === 'success') posterVersion.value += 1
  detail.value = state.detail
  notFound.value = state.kind === 'not-found'
  errorMessage.value = state.errorMessage
  pending.value = false
}

watch(slug, () => void loadCity())

const config = useRuntimeConfig()
const canonicalUrl = computed(() => absoluteSiteUrl(config.public.siteUrl, `/ville/${encodeURIComponent(slug.value)}/cinemas`))
const pageTitle = computed(() => detail.value ? `Cinémas à ${detail.value.city.name} - MesSeances` : 'Cinémas par ville - MesSeances')
const pageDescription = computed(() => detail.value
  ? cityDescription(detail.value.city.name, detail.value.theaters.length, detail.value.movies.length)
  : 'Découvrez les cinémas et films actuellement programmés dans cette ville.')
const robots = computed(() => detail.value && !pending.value && !errorMessage.value && !notFound.value && Object.keys(route.query).length === 0 ? 'index,follow' : 'noindex,follow')
const cityJsonLd = computed(() => {
  const current = detail.value
  if (!current || pending.value || errorMessage.value || notFound.value) return null
  const cityUrl = canonicalUrl.value
  const graph: JsonLdNode[] = [{
    '@type': 'BreadcrumbList',
    '@id': `${cityUrl}#breadcrumb`,
    itemListElement: [
      { '@type': 'ListItem', position: 1, name: 'Accueil', item: absoluteSiteUrl(config.public.siteUrl, '/') },
      { '@type': 'ListItem', position: 2, name: 'Cinémas', item: absoluteSiteUrl(config.public.siteUrl, '/cinemas') },
      { '@type': 'ListItem', position: 3, name: current.city.name, item: cityUrl }
    ]
  }]
  if (Object.keys(route.query).length === 0 && current.theaters.length > 0) {
    graph.push({
      '@type': 'ItemList',
      '@id': `${cityUrl}#cinema-list`,
      itemListElement: current.theaters.map((theater, index) => ({
        '@type': 'ListItem',
        position: index + 1,
        url: absoluteSiteUrl(config.public.siteUrl, `/cinema/${encodeURIComponent(theater.slug)}`)
      }))
    })
  }
  if (Object.keys(route.query).length === 0 && current.movies.length > 0) {
    graph.push({
      '@type': 'ItemList',
      '@id': `${cityUrl}#film-list`,
      itemListElement: current.movies.map((movie, index) => ({
        '@type': 'ListItem',
        position: index + 1,
        url: absoluteSiteUrl(config.public.siteUrl, `/film/${encodeURIComponent(movie.slug)}`)
      }))
    })
  }
  return serializeJsonLd({ '@context': 'https://schema.org', '@graph': graph })
})

useSeoMeta({
  robots,
  title: pageTitle,
  description: pageDescription,
  ogTitle: pageTitle,
  ogDescription: pageDescription,
  ogUrl: canonicalUrl,
  ogType: 'website'
})
useHead(() => ({
  link: [{ rel: 'canonical', href: canonicalUrl.value }],
  script: cityJsonLd.value ? [{ key: 'city-jsonld', type: 'application/ld+json', innerHTML: cityJsonLd.value }] : []
}))
</script>

<template>
  <main class="city-page mx-auto max-w-[1440px] px-4 py-8 sm:px-6 sm:py-10 lg:px-10 lg:py-14">
    <EditorialStatePanel v-if="pending && !detail" semantic="status" live="polite" size="standard" shadow="large" class="city-state mx-auto max-w-3xl font-bold"><template #icon><LoaderCircle :size="34" class="animate-spin" aria-hidden="true" /></template><p>Chargement de la ville…</p></EditorialStatePanel>
    <EditorialStatePanel v-else-if="notFound" semantic="alert" size="standard" shadow="large" class="city-state mx-auto max-w-3xl font-bold"><template #icon><MapPin :size="36" aria-hidden="true" /></template><template #heading><h1 class="text-2xl font-black">Ville introuvable</h1></template><p>Cette ville n’est pas disponible dans la programmation actuelle.</p><template #actions><NuxtLink to="/cinemas" class="city-action">Voir les cinémas</NuxtLink></template></EditorialStatePanel>
    <EditorialStatePanel v-else-if="errorMessage && !detail" semantic="alert" size="standard" shadow="large" class="city-state mx-auto max-w-3xl font-bold"><template #icon><AlertTriangle :size="34" class="text-primary" aria-hidden="true" /></template><template #heading><h1 class="text-2xl font-black">Impossible de charger cette ville</h1></template><p>{{ errorMessage }}</p><template #actions><button type="button" class="city-action" @click="loadCity"><RefreshCw :size="17" aria-hidden="true" /> Réessayer</button></template></EditorialStatePanel>

    <template v-else-if="detail">
      <Breadcrumbs
        :items="[
          { label: 'Accueil', to: '/' },
          { label: 'Cinémas', to: '/cinemas' },
          { label: detail.city.name }
        ]"
      />
      <header class="border-2 border-ink bg-surface shadow-[8px_8px_0_#27272a]">
        <div class="grid lg:grid-cols-[minmax(0,1.55fr)_minmax(17rem,0.65fr)]">
          <div class="min-w-0 p-5 sm:p-8 lg:p-10">
            <p class="utility-label">Cinémas · ville</p>
            <h1 class="mt-4 break-words text-[clamp(2.5rem,5.5vw,5rem)] font-black uppercase leading-[0.9] tracking-[-0.065em]">{{ detail.city.name }}<span class="text-primary">.</span></h1>
          </div>

          <dl class="grid border-t-2 border-ink sm:grid-cols-2 lg:grid-cols-1 lg:border-l-2 lg:border-t-0">
            <div class="min-w-0 p-5 sm:p-6">
              <dt class="utility-label flex items-center gap-3 text-muted"><Building2 :size="20" class="shrink-0 text-primary" aria-hidden="true" /> Cinémas</dt>
              <dd class="mt-2 pl-8 text-2xl font-black leading-none">{{ detail.theaters.length }}</dd>
            </div>
            <div class="min-w-0 border-t-2 border-ink p-5 sm:border-l-2 sm:border-t-0 sm:p-6 lg:border-l-0 lg:border-t-2">
              <dt class="utility-label flex items-center gap-3 text-muted"><Film :size="20" class="shrink-0 text-primary" aria-hidden="true" /> Films</dt>
              <dd class="mt-2 pl-8 text-2xl font-black leading-none">{{ detail.movies.length }}</dd>
            </div>
          </dl>
        </div>

        <div class="border-t-2 border-ink bg-[#f1efe8] px-5 py-4 sm:px-8 sm:py-5 lg:px-10">
          <p class="max-w-4xl text-sm font-semibold leading-6 sm:text-base sm:leading-7">{{ pageDescription }}</p>
        </div>
      </header>

      <section class="mt-12" aria-labelledby="city-cinemas-heading">
        <div class="flex items-end justify-between gap-4 border-b-2 border-ink pb-5">
          <h2 id="city-cinemas-heading" class="text-4xl font-black tracking-[-0.05em] sm:text-5xl">Cinémas</h2>
          <ShareButton class="shrink-0" />
        </div>
        <EditorialStatePanel v-if="detail.theaters.length === 0" size="standard" shadow="large" class="city-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><Building2 :size="36" aria-hidden="true" /></template><template #heading><h3 class="text-2xl font-black">Aucun cinéma disponible</h3></template><p>La programmation actuelle ne contient aucun cinéma dans cette ville.</p></EditorialStatePanel>
        <ul v-else class="mt-8 grid gap-5 md:grid-cols-2">
          <li v-for="theater in detail.theaters" :key="theater.id" class="border-2 border-ink bg-surface p-5 shadow-[6px_6px_0_#27272a]">
            <h3 class="text-2xl font-black tracking-[-0.04em]"><NuxtLink :to="`/cinema/${encodeURIComponent(theater.slug)}`" class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary"><BrandedText :text="theater.name" /></NuxtLink></h3>
            <p v-if="theater.address || theater.postal_code" class="mt-3 break-words text-sm font-semibold leading-6"><span v-if="theater.address">{{ theater.address }}<br /></span>{{ theater.postal_code }} {{ theater.city }}</p>
          </li>
        </ul>
      </section>

      <section class="mt-14" aria-labelledby="city-films-heading">
        <div class="border-b-2 border-ink pb-4"><h2 id="city-films-heading" class="text-4xl font-black tracking-[-0.05em] sm:text-5xl">Films à l’affiche</h2></div>
        <EditorialStatePanel v-if="detail.movies.length === 0" size="standard" shadow="large" class="city-state mx-auto mt-8 max-w-3xl font-bold"><template #icon><Film :size="36" aria-hidden="true" /></template><template #heading><h3 class="text-2xl font-black">Aucun film disponible</h3></template><p>Aucun film n’est programmé dans cette ville sur la période actuelle.</p></EditorialStatePanel>
        <ul v-else class="mt-8 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
          <li v-for="movie in detail.movies" :key="movie.slug" class="min-w-0">
            <NuxtLink :to="`/film/${encodeURIComponent(movie.slug)}`" class="group block h-full border-2 border-ink bg-surface shadow-[5px_5px_0_#27272a] transition-transform hover:-translate-y-1">
              <div class="aspect-[2/3] overflow-hidden border-b-2 border-ink bg-[#e8e6de]">
                 <PosterImage :src="movie.poster_url" :alt="`Affiche de ${movie.title}`" :reset-key="posterVersion" :data-poster-slug="movie.slug" class="size-full" image-class="size-full object-cover" fallback-variant="icon-only" fallback-class="text-muted" :fallback-icon-size="32" :fallback-text="null" />
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
.city-action { display: inline-flex; min-height: 2.75rem; align-items: center; justify-content: center; gap: .5rem; border: 2px solid #27272a; background: #27272a; padding: .65rem .9rem; color: #fff; font-family: ui-monospace,monospace; font-size: .7rem; font-weight: 900; text-transform: uppercase; }
.utility-label { font-family: ui-monospace,monospace; font-size: .68rem; font-weight: 900; letter-spacing: .1em; text-transform: uppercase; }
</style>
