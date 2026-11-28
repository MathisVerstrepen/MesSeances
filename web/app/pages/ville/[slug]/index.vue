<script setup lang="ts">
import {
  AlertTriangle,
  Building2,
  LoaderCircle,
  MapPin,
  RefreshCw,
} from '@lucide/vue'
import type { CityDetailResponse, TimelineResponse } from '~/types/api'
import {
  type CityDayMovieGroup,
  type CityDayShowtime,
  type CityDayTheater,
  cityAvailableDates,
  compactCityShowtime,
  orderCityTheaters,
  cityDayPageCount,
  cityDayTheaters,
  groupShowtimesByMovie,
  resolveCityDate,
  titlesByShowtimeCount,
} from '~/utils/cityDay'
import {
  formatLongDateWithYear,
  formatParisTime,
  formatParisUpdate,
  todayInParis,
} from '~/utils/date'
import { cityShowtimesDescription } from '~/utils/entityDescriptions'
import { formatLabel, formatRuntime } from '~/utils/formats'
import { serializeJsonLd, type JsonLdNode } from '~/utils/jsonLd'
import {
  calendarDate,
  positiveSafeInteger,
  singularQueryValue,
} from '~/utils/routeQuery'
import { resolveShowtimeEnd } from '~/utils/showtimeEnd'
import { languageLabel } from '~/utils/showtimeFilters'
import { absoluteSiteUrl } from '~/utils/siteUrl'

interface CityDaySummary {
  city: CityDetailResponse['city']
  generated_at: string
  theaters: CityDayTheater[]
}

interface CityDayTimelineTheater {
  id: string
  showtimes: CityDayShowtime[]
}

type CityDayState =
  | {
      kind: 'success'
      detail: CityDaySummary
      dates: string[]
      date: string
      page: number
      pageCount: number
      timeline: { theaters: CityDayTimelineTheater[] } | null
      errorMessage: string
    }
  | { kind: 'not-found' }
  | { kind: 'upstream-error'; errorMessage: string }

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const config = useRuntimeConfig()

const slug = computed(() => {
  const value = route.params.slug
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '')
})
const requestedDate = computed(() =>
  calendarDate(singularQueryValue(route.query.date)),
)
const requestedPage = computed(
  () => positiveSafeInteger(singularQueryValue(route.query.page)) ?? 1,
)

async function loadCityDay(): Promise<CityDayState> {
  let response: CityDetailResponse
  try {
    response = await api.city(slug.value)
  } catch (error) {
    if (isNotFoundError(error)) return { kind: 'not-found' }
    return { kind: 'upstream-error', errorMessage: getFrenchApiError(error) }
  }
  const detail: CityDaySummary = {
    city: response.city,
    generated_at: response.generated_at,
    theaters: orderCityTheaters(response),
  }
  const currentDay = todayInParis()
  const dates = cityAvailableDates(detail.theaters, currentDay)
  const date = resolveCityDate(requestedDate.value, dates, currentDay)
  const pageCount = cityDayPageCount(detail.theaters.length)
  const page = Math.min(requestedPage.value, pageCount)
  const theaters = cityDayTheaters(detail.theaters, page)
  const base = {
    kind: 'success' as const,
    detail,
    dates,
    date,
    page,
    pageCount,
  }
  if (theaters.length === 0)
    return { ...base, timeline: null, errorMessage: '' }
  try {
    const response: TimelineResponse = await api.timeline({
      date,
      theaters: theaters.map((theater) => theater.id).join(','),
    })
    const timeline = {
      theaters: response.theaters.map((theater) => ({
        id: theater.id,
        showtimes: theater.showtimes.map(compactCityShowtime),
      })),
    }
    return { ...base, timeline, errorMessage: '' }
  } catch (error) {
    return { ...base, timeline: null, errorMessage: getFrenchApiError(error) }
  }
}

const { data, pending, refresh } = await useAsyncData(
  () =>
    `city-day:${slug.value}:${requestedDate.value ?? ''}:${requestedPage.value}`,
  loadCityDay,
)

const state = computed(() => data.value)
const success = computed(() =>
  state.value?.kind === 'success' ? state.value : null,
)
const notFound = computed(() => state.value?.kind === 'not-found')
const upstreamError = computed(() =>
  state.value?.kind === 'upstream-error' ? state.value.errorMessage : '',
)

if (import.meta.server && state.value && state.value.kind !== 'success') {
  const event = useRequestEvent()
  if (event)
    setResponseStatus(event, state.value.kind === 'not-found' ? 404 : 502)
}

const today = computed(() => todayInParis())
const defaultDate = computed(() =>
  success.value
    ? resolveCityDate(undefined, success.value.dates, today.value)
    : today.value,
)
const cityPath = computed(() => `/ville/${encodeURIComponent(slug.value)}`)

// The default date lives at the canonical URL.
function cityDateHref(date: string): string {
  return date === defaultDate.value
    ? cityPath.value
    : `${cityPath.value}?date=${date}`
}

function selectDate(date: string) {
  void router.push(cityDateHref(date))
}

function pageHref(page: number): string {
  const query = new URLSearchParams()
  if (success.value && success.value.date !== defaultDate.value)
    query.set('date', success.value.date)
  if (page > 1) query.set('page', String(page))
  const search = query.toString()
  return search ? `${cityPath.value}?${search}` : cityPath.value
}

function cinemaHref(cinemaSlug: string): string {
  const path = `/cinema/${encodeURIComponent(cinemaSlug)}`
  const date = success.value?.date
  return date && date !== today.value ? `${path}?date=${date}` : path
}

function filmHref(filmSlug: string): string {
  const path = `/film/${encodeURIComponent(filmSlug)}`
  const date = success.value?.date
  return date ? `${path}?date=${date}` : path
}

// Page theaters in directory order, with their sessions grouped by film.
const theaterSections = computed(() => {
  const current = success.value
  if (!current) return []
  const timeline = new Map(
    current.timeline?.theaters.map((theater) => [theater.id, theater]) ?? [],
  )
  return cityDayTheaters(current.detail.theaters, current.page).map(
    (theater) => ({
      theater,
      movies: groupShowtimesByMovie(timeline.get(theater.id)?.showtimes ?? []),
    }),
  )
})
const showtimeCount = computed(() =>
  theaterSections.value.reduce(
    (total, section) =>
      total +
      section.movies.reduce((sum, group) => sum + group.showtimes.length, 0),
    0,
  ),
)

function showtimeLabel(group: CityDayMovieGroup, index: number) {
  const showtime = group.showtimes[index]!
  const end = resolveShowtimeEnd(showtime)
  const details = [
    showtime.language
      ? languageLabel(showtime.language, group.movie.original_language)
      : '',
    showtime.format ? formatLabel(showtime.format) : '',
  ].filter(Boolean)
  return {
    start: formatParisTime(showtime.start_time),
    end: end ? `${end.estimated ? '~' : ''}${formatParisTime(end.time)}` : '',
    details: details.join(' · '),
  }
}

const canonicalUrl = computed(() =>
  absoluteSiteUrl(config.public.siteUrl, cityPath.value),
)
const cityName = computed(() => success.value?.detail.city.name ?? '')
const isDefaultView = computed(() => Object.keys(route.query).length === 0)
const pageTitle = computed(() => {
  if (!success.value) return 'Séances par ville - MesSeances'
  return isDefaultView.value
    ? `Séances de cinéma à ${cityName.value} aujourd’hui - MesSeances`
    : `Séances de cinéma à ${cityName.value}, ${formatLongDateWithYear(success.value.date)} - MesSeances`
})
const pageDescription = computed(() =>
  success.value
    ? cityShowtimesDescription(
        cityName.value,
        success.value.detail.theaters.length,
        titlesByShowtimeCount(success.value.timeline?.theaters ?? []),
      )
    : 'Les séances de cinéma du jour, ville par ville.',
)
const robots = computed(() =>
  success.value && !success.value.errorMessage && isDefaultView.value
    ? 'index,follow'
    : 'noindex,follow',
)
const cityJsonLd = computed(() => {
  const current = success.value
  if (!current) return null
  const graph: JsonLdNode[] = [
    {
      '@type': 'BreadcrumbList',
      '@id': `${canonicalUrl.value}#breadcrumb`,
      itemListElement: [
        {
          '@type': 'ListItem',
          position: 1,
          name: 'Accueil',
          item: absoluteSiteUrl(config.public.siteUrl, '/'),
        },
        {
          '@type': 'ListItem',
          position: 2,
          name: current.detail.city.name,
          item: absoluteSiteUrl(
            config.public.siteUrl,
            `${cityPath.value}/cinemas`,
          ),
        },
        {
          '@type': 'ListItem',
          position: 3,
          name: 'Séances',
          item: canonicalUrl.value,
        },
      ],
    },
  ]
  if (theaterSections.value.length)
    graph.push({
      '@type': 'ItemList',
      '@id': `${canonicalUrl.value}#cinema-list`,
      itemListElement: theaterSections.value.map(({ theater }, index) => ({
        '@type': 'ListItem',
        position: index + 1,
        url: absoluteSiteUrl(
          config.public.siteUrl,
          `/cinema/${encodeURIComponent(theater.slug)}`,
        ),
      })),
    })
  return serializeJsonLd({ '@context': 'https://schema.org', '@graph': graph })
})

useSeoMeta({
  robots,
  title: pageTitle,
  description: pageDescription,
  ogTitle: pageTitle,
  ogDescription: pageDescription,
  ogUrl: canonicalUrl,
  ogType: 'website',
})
useHead(() => ({
  link: notFound.value
    ? [{ rel: 'canonical', href: canonicalUrl.value }]
    : [
        { rel: 'canonical', href: canonicalUrl.value },
        {
          rel: 'alternate',
          type: 'text/markdown',
          href: `${canonicalUrl.value}.md`,
          title: 'Version Markdown pour agents IA',
        },
      ],
  script: cityJsonLd.value
    ? [
        {
          key: 'city-day-jsonld',
          type: 'application/ld+json',
          innerHTML: cityJsonLd.value,
        },
      ]
    : [],
}))
</script>

<template>
  <main
    id="city-day"
    :aria-busy="pending"
    class="mx-auto min-h-[70vh] max-w-[1440px] bg-[#f8f7f2] px-4 py-8 [background-image:linear-gradient(rgba(39,39,42,0.07)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.07)_1px,transparent_1px)] [background-size:28px_28px] sm:px-6 sm:py-10 lg:px-10 lg:py-14"
  >
    <EditorialStatePanel
      v-if="pending && !state"
      semantic="status"
      live="polite"
      size="standard"
      shadow="large"
      class="mx-auto max-w-3xl font-bold"
      ><template #icon
        ><LoaderCircle
          :size="34"
          class="animate-spin"
          aria-hidden="true"
        /></template
      >
      <p>Chargement des séances…</p></EditorialStatePanel
    >
    <EditorialStatePanel
      v-else-if="notFound"
      semantic="alert"
      size="standard"
      shadow="large"
      class="mx-auto max-w-3xl font-bold"
      ><template #icon><MapPin :size="36" aria-hidden="true" /></template
      ><template #heading
        ><h1 class="text-2xl font-black">Ville introuvable</h1></template
      >
      <p>Cette ville n’est pas disponible dans la programmation actuelle.</p>
      <template #actions
        ><NuxtLink
          to="/cinemas"
          class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase"
          >Voir les cinémas</NuxtLink
        ></template
      ></EditorialStatePanel
    >
    <EditorialStatePanel
      v-else-if="upstreamError"
      semantic="alert"
      size="standard"
      shadow="large"
      class="mx-auto max-w-3xl font-bold"
      ><template #icon
        ><AlertTriangle
          :size="34"
          class="text-primary"
          aria-hidden="true"
        /></template
      ><template #heading
        ><h1 class="text-2xl font-black">
          Impossible de charger cette ville
        </h1></template
      >
      <p>{{ upstreamError }}</p>
      <template #actions
        ><button
          type="button"
          class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase"
          @click="refresh()"
        >
          <RefreshCw :size="17" aria-hidden="true" />
          Réessayer
        </button></template
      ></EditorialStatePanel
    >

    <template v-else-if="success">
      <Breadcrumbs
        :items="[
          { label: 'Accueil', to: '/' },
          { label: success.detail.city.name, to: `${cityPath}/cinemas` },
          { label: 'Séances' }
        ]"
      />
      <header class="border-2 border-ink bg-surface shadow-[8px_8px_0_#27272a]">
        <div class="p-5 sm:p-8 lg:p-10">
          <p
            class="font-mono text-[0.68rem] font-black uppercase tracking-[0.1em]"
          >
            Séances · ville
          </p>
          <h1
            class="mt-4 break-words text-[clamp(2.25rem,5vw,4.5rem)] font-black uppercase leading-[0.9] tracking-[-0.065em]"
          >
            Séances à {{ success.detail.city.name
            }}<span class="text-primary">.</span>
          </h1>
          <p
            class="mt-5 font-mono text-xs font-bold uppercase tracking-[0.08em] text-muted"
          >
            <time :datetime="success.date" class="text-ink">{{
              formatLongDateWithYear(success.date)
            }}</time><template
              v-if="formatParisUpdate(success.detail.generated_at)"
            >
              · mis à jour le
              {{ formatParisUpdate(success.detail.generated_at) }}</template
            >
          </p>
          <p
            class="mt-4 max-w-4xl text-sm font-semibold leading-6 sm:text-base"
          >
            <template v-if="success.pageCount > 1"
              >{{ success.detail.theaters.length }}
              cinémas, les plus programmés d’abord. Page {{ success.page }} sur
              {{ success.pageCount }}
              : {{ showtimeCount }} séance{{
                showtimeCount === 1 ? '' : 's'
              }}.</template
            ><template v-else
              >{{ showtimeCount }}
              séance{{ showtimeCount === 1 ? '' : 's' }}
              dans {{ success.detail.theaters.length }} cinéma{{
                success.detail.theaters.length === 1 ? '' : 's'
              }}.</template
            >{{ ' '
            }}<NuxtLink
              :to="`${cityPath}/cinemas`"
              class="underline decoration-2 underline-offset-4 hover:text-primary"
              >Annuaire des cinémas de {{ success.detail.city.name }}</NuxtLink
            >
          </p>
        </div>
        <div
          v-if="success.dates.length"
          class="border-t-2 border-ink bg-[#f1efe8] px-5 py-4 sm:px-8 lg:px-10"
        >
          <ShowtimeDateBar
            :selected-date="success.date"
            :available-dates="success.dates"
            :today="today"
            :date-href="cityDateHref"
            mobile-tabs="all"
            @select="selectDate"
          />
        </div>
      </header>

      <EditorialStatePanel
        v-if="success.errorMessage"
        semantic="alert"
        size="standard"
        shadow="large"
        class="mx-auto mt-10 max-w-3xl font-bold"
        ><template #icon
          ><AlertTriangle
            :size="34"
            class="text-primary"
            aria-hidden="true"
          /></template
        >
        <p>{{ success.errorMessage }}</p>
        <template #actions
          ><button
            type="button"
            class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-ink px-[0.9rem] py-[0.65rem] font-mono text-[0.7rem] font-black text-surface uppercase"
            @click="refresh()"
          >
            <RefreshCw :size="17" aria-hidden="true" />
            Réessayer
          </button></template
        ></EditorialStatePanel
      >
      <EditorialStatePanel
        v-else-if="success.detail.theaters.length === 0"
        size="standard"
        shadow="large"
        class="mx-auto mt-10 max-w-3xl font-bold"
        ><template #icon><Building2 :size="36" aria-hidden="true" /></template
        ><template #heading
          ><h2 class="text-2xl font-black">
            Aucun cinéma disponible
          </h2></template
        >
        <p>
          La programmation actuelle ne contient aucun cinéma dans cette ville.
        </p></EditorialStatePanel
      >

      <div v-else class="mt-10 grid gap-8">
        <section
          v-for="section in theaterSections"
          :key="section.theater.id"
          class="border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]"
          :aria-labelledby="`cinema-${section.theater.id}`"
        >
          <div class="border-b-2 border-ink p-5 sm:p-6">
            <h2
              :id="`cinema-${section.theater.id}`"
              class="text-2xl font-black tracking-[-0.04em] sm:text-3xl"
            >
              <NuxtLink
                :to="cinemaHref(section.theater.slug)"
                class="underline decoration-2 underline-offset-4 hover:text-primary"
                ><TheaterName
                  :name="section.theater.name"
                  :provider="section.theater.provider"
                /></NuxtLink
              >
            </h2>
            <p
              v-if="section.theater.address || section.theater.postal_code"
              class="mt-2 text-sm font-semibold"
            >
              {{ section.theater.address
              }}<template
                v-if="section.theater.postal_code && !section.theater.address.includes(section.theater.postal_code)"
                >, {{ section.theater.postal_code }}
                {{ section.theater.city }}</template
              >
            </p>
          </div>
          <p
            v-if="section.movies.length === 0"
            class="p-5 text-sm font-bold text-muted sm:p-6"
          >
            Aucune séance ce jour.
          </p>
          <ul v-else class="divide-y-2 divide-ink">
            <li
              v-for="group in section.movies"
              :key="group.movie.slug"
              class="grid gap-3 p-5 sm:grid-cols-[minmax(12rem,18rem)_minmax(0,1fr)] sm:gap-6 sm:p-6"
            >
              <h3 class="min-w-0 text-lg font-black leading-tight">
                <NuxtLink
                  :to="filmHref(group.movie.slug)"
                  class="underline decoration-2 underline-offset-4 hover:text-primary"
                  >{{
                    group.movie.title
                  }}</NuxtLink
                >
                <span
                  v-if="group.movie.runtime_minutes > 0"
                  class="mt-1 block font-mono text-[11px] font-bold uppercase tracking-[0.08em] text-muted"
                  >{{
                    formatRuntime(group.movie.runtime_minutes)
                  }}</span
                >
              </h3>
              <ul
                class="flex flex-wrap gap-2"
                :aria-label="`Séances de ${group.movie.title}`"
              >
                <li
                  v-for="(showtime, index) in group.showtimes"
                  :key="showtime.id"
                >
                  <BookingLink
                    :url="showtime.booking_url"
                    :provider="showtime.provider"
                    :showtime-id="showtime.id"
                    :theater-id="section.theater.id"
                    :aria-label="`Réserver ${group.movie.title} à ${showtimeLabel(group, index).start}`"
                    unstyled
                    class="showtime-chip"
                    available-class="showtime-chip-available"
                  >
                    <b
                      >{{ showtimeLabel(group, index).start
                      }}<small v-if="showtimeLabel(group, index).end">
                        → {{ showtimeLabel(group, index).end }}</small
                      ></b
                    ><span v-if="showtimeLabel(group, index).details">{{
                      showtimeLabel(group, index).details
                    }}</span>
                  </BookingLink>
                </li>
              </ul>
            </li>
          </ul>
        </section>

        <nav
          v-if="success.pageCount > 1"
          class="flex flex-wrap items-center justify-between gap-3 border-2 border-ink bg-surface p-3 shadow-[6px_6px_0_#27272a]"
          aria-label="Pagination des cinémas"
        >
          <NuxtLink
            v-if="success.page > 1"
            :to="pageHref(success.page - 1)"
            class="inline-flex min-h-11 items-center border-2 border-ink bg-[#ffcf3f] px-4 font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] hover:bg-ink hover:text-white"
            >← Cinémas précédents</NuxtLink
          >
          <span v-else />
          <span
            class="font-mono text-[11px] font-bold uppercase tracking-[0.14em]"
            >Page {{ success.page }} / {{ success.pageCount }}</span
          >
          <NuxtLink
            v-if="success.page < success.pageCount"
            :to="pageHref(success.page + 1)"
            class="inline-flex min-h-11 items-center border-2 border-ink bg-[#ffcf3f] px-4 font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] hover:bg-ink hover:text-white"
            >Cinémas suivants →</NuxtLink
          >
          <span v-else />
        </nav>
      </div>

      <aside
        class="mt-10 border-2 border-ink bg-[#f1efe8] p-5 text-sm font-semibold leading-6 sm:p-6"
        aria-labelledby="city-day-legend"
      >
        <h2
          id="city-day-legend"
          class="font-mono text-[0.68rem] font-black uppercase tracking-[0.1em]"
        >
          Légende
        </h2>
        <dl class="mt-3 grid gap-x-6 gap-y-1 sm:grid-cols-2">
          <div>
            <dt class="inline font-black">VF</dt>
            :
            <dd class="inline">version française (doublée)</dd>
          </div>
          <div>
            <dt class="inline font-black">VOF</dt>
            :
            <dd class="inline">version originale française</dd>
          </div>
          <div>
            <dt class="inline font-black">VOSTFR</dt>
            :
            <dd class="inline">version originale sous-titrée en français</dd>
          </div>
          <div>
            <dt class="inline font-black">VO</dt>
            :
            <dd class="inline">version originale</dd>
          </div>
          <div>
            <dt class="inline font-black">VF SME</dt>
            :
            <dd class="inline">sous-titres pour sourds et malentendants</dd>
          </div>
          <div>
            <dt class="inline font-black">VFSTF</dt>
            :
            <dd class="inline">version française sous-titrée en français</dd>
          </div>
          <div>
            <dt class="inline font-black">~</dt>
            :
            <dd class="inline">fin estimée, publicité comprise</dd>
          </div>
          <div>
            <dt class="inline font-black">Heures</dt>
            :
            <dd class="inline">heure de Paris</dd>
          </div>
        </dl>
        <p class="mt-3">
          Agents IA :
          <a
            :href="`${cityPath}.md`"
            class="underline decoration-2 underline-offset-4"
            >version Markdown de cette page</a
          >
          ·
          <NuxtLink
            to="/agents"
            class="underline decoration-2 underline-offset-4"
            >guide pour agents et API</NuxtLink
          >
        </p>
      </aside>
    </template>
  </main>
</template>

<style scoped>
@reference "../../../assets/css/main.css";

/* One class per chip: big cities render hundreds of sessions. */
.showtime-chip {
  @apply inline-flex min-h-11 flex-col justify-center border-2 border-ink bg-[#f1efe8] px-3 py-1 text-left;
}

.showtime-chip-available {
  @apply bg-surface shadow-[3px_3px_0_#27272a] hover:bg-highlight;
}

.showtime-chip b {
  @apply font-black;
}

.showtime-chip small {
  @apply text-xs font-bold text-muted;
}

.showtime-chip span {
  @apply font-mono text-[10px] font-bold uppercase tracking-[0.06em];
}
</style>
