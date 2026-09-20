<script setup lang="ts">
import { AlertTriangle, ArrowLeft, LoaderCircle, RefreshCw } from '@lucide/vue'
import type { UpcomingReviewFilter } from '~/types/api'
import {
  frenchReleaseTypeLabels,
  formatUpcomingAssessmentTime,
  normalizeUpcomingReviewSearch,
  parseUpcomingReviewRoute,
  upcomingReviewDecisionLabels,
  upcomingReviewFilters,
  upcomingReviewReasonLabels,
  upcomingReviewRouteQuery,
  upcomingReviewVisibility,
  UPCOMING_REVIEW_PAGE_SIZE,
  UPCOMING_REVIEW_SEARCH_DELAY,
} from '~/utils/adminUpcomingMovies'
import { formatFrenchReleaseDate } from '~/utils/upcomingMovies'

definePageMeta({ middleware: 'admin-auth' })
useHead({ title: 'Revue des sorties à venir - MesSeances' })

const route = useRoute()
const router = useRouter()
const api = useMesSeancesApi()
const {
  pending: upcomingPending,
  running: upcomingRunning,
  canStart: canStartUpcoming,
  canCheck: canCheckUpcoming,
  needsCheck: upcomingNeedsCheck,
  error: upcomingError,
  message: upcomingMessage,
  checkStatus: checkUpcomingStatus,
  start: startUpcomingSync,
  dispose: disposeUpcomingSync,
} = useAdminUpcomingSync(api)

onMounted(() => {
  void checkUpcomingStatus()
})
onBeforeUnmount(disposeUpcomingSync)

const filters = computed(() => parseUpcomingReviewRoute(route.query))
const search = ref(filters.value.q)
const {
  items,
  total,
  loading,
  loaded,
  error,
  message,
  mutationID,
  canMutate,
  load,
  invalidate,
  decide,
  dispose,
} = useAdminUpcomingMovies(api, async (page) => {
  await router.replace({
    query: upcomingReviewRouteQuery({ ...filters.value, page }, route.query),
  })
})
const pageCount = computed(() =>
  Math.max(1, Math.ceil(total.value / UPCOMING_REVIEW_PAGE_SIZE)),
)
const secondaryButtonClass =
  'inline-flex min-h-11 items-center justify-center rounded-md border border-line bg-surface px-3 py-2 text-sm font-semibold text-ink enabled:hover:border-line-hover enabled:hover:bg-subtle disabled:cursor-not-allowed disabled:opacity-45 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent'
let searchTimer: ReturnType<typeof setTimeout> | undefined
let mounted = false

function cancelSearch() {
  if (searchTimer !== undefined) clearTimeout(searchTimer)
  searchTimer = undefined
}

async function syncRoute() {
  cancelSearch()
  search.value = filters.value.q
  const query = upcomingReviewRouteQuery(filters.value, route.query)
  if (JSON.stringify(query) !== JSON.stringify(route.query)) {
    await router.replace({ query })
    return
  }
  await load(filters.value)
}

function changeSearch() {
  cancelSearch()
  invalidate()
  searchTimer = setTimeout(() => {
    searchTimer = undefined
    const q = normalizeUpcomingReviewSearch(search.value)
    search.value = q
    if (q === filters.value.q && filters.value.page === 1)
      void load(filters.value)
    else
      void router.replace({
        query: upcomingReviewRouteQuery(
          { ...filters.value, q, page: 1 },
          route.query,
        ),
      })
  }, UPCOMING_REVIEW_SEARCH_DELAY)
}

async function changeFilter(event: Event) {
  if (!(event.target instanceof HTMLSelectElement)) return
  const value = event.target.value
  const filter: UpcomingReviewFilter =
    upcomingReviewFilters.find((option) => option.value === value)?.value ??
    'needs_review'
  await selectFilter(filter)
}

async function selectFilter(filter: UpcomingReviewFilter) {
  cancelSearch()
  await router.replace({
    query: upcomingReviewRouteQuery(
      { filter, q: normalizeUpcomingReviewSearch(search.value), page: 1 },
      route.query,
    ),
  })
}

async function changePage(page: number) {
  cancelSearch()
  await router.push({
    query: upcomingReviewRouteQuery({ ...filters.value, page }, route.query),
  })
}

watch(
  () => route.query,
  () => {
    if (mounted) void syncRoute()
  },
)
onMounted(() => {
  mounted = true
  void syncRoute()
})
onBeforeUnmount(() => {
  mounted = false
  cancelSearch()
  dispose()
})
</script>

<template>
  <main class="mx-auto max-w-5xl px-4 py-6 sm:px-6 sm:py-8 lg:px-8">
    <header class="border-b border-line pb-5">
      <NuxtLink
        to="/admin"
        class="mb-1 inline-flex min-h-11 items-center gap-1 text-sm font-semibold text-muted hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        <ArrowLeft :size="16" aria-hidden="true" />
        Administration
      </NuxtLink>
      <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">
        Revue des sorties à venir
      </h1>
      <p class="mt-3 text-sm text-muted">
        Les signalements restent visibles jusqu’à leur exclusion.
      </p>
    </header>

    <div class="mt-5">
      <button
        type="button"
        class="inline-flex min-h-11 w-full items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-semibold text-white enabled:hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent sm:w-auto"
        :disabled="!canStartUpcoming"
        aria-describedby="upcoming-sync-status"
        @click="startUpcomingSync"
      >
        <LoaderCircle
          v-if="upcomingPending || upcomingRunning"
          :size="17"
          class="shrink-0 animate-spin"
          aria-hidden="true"
        />
        <RefreshCw v-else :size="17" class="shrink-0" aria-hidden="true" />
        Synchroniser TMDB - Prochainement
      </button>
      <p
        id="upcoming-sync-status"
        class="text-sm text-muted"
        :class="{ 'mt-3': upcomingMessage }"
        role="status"
        aria-live="polite"
      >
        {{ upcomingMessage }}
      </p>
      <div
        v-if="upcomingError"
        class="mt-3 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800"
        role="alert"
      >
        <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
        <p>{{ upcomingError }}</p>
      </div>
      <button
        v-if="upcomingNeedsCheck && !upcomingPending"
        type="button"
        class="mt-3"
        :class="secondaryButtonClass"
        :disabled="!canCheckUpcoming"
        @click="checkUpcomingStatus"
      >
        Vérifier le statut
      </button>
    </div>

    <div class="mt-5 grid grid-cols-1 gap-4 sm:grid-cols-3">
      <div>
        <label
          for="review-filter"
          class="mb-2 block text-sm font-semibold text-ink"
          >État de revue</label
        >
        <select
          id="review-filter"
          :value="filters.filter"
          class="min-h-11 w-full rounded-md border border-line bg-surface px-3 text-sm text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          @change="changeFilter"
        >
          <option
            v-for="option in upcomingReviewFilters"
            :key="option.value"
            :value="option.value"
          >
            {{ option.label }}
          </option>
        </select>
      </div>
      <div class="sm:col-span-2">
        <label
          for="review-search"
          class="mb-2 block text-sm font-semibold text-ink"
          >Titre ou ID TMDB</label
        >
        <input
          id="review-search"
          v-model="search"
          type="search"
          autocomplete="off"
          class="min-h-11 w-full rounded-md border border-line bg-surface px-3 text-sm text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          @input="changeSearch"
        >
      </div>
    </div>

    <p
      class="text-sm font-semibold text-ink empty:hidden"
      :class="{ 'mt-4': message }"
      role="status"
      aria-live="polite"
    >
      {{ message }}
    </p>
    <div
      v-if="error"
      class="mt-4 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800"
      role="alert"
    >
      <p>{{ error }}</p>
      <button
        type="button"
        class="mt-2 min-h-11 font-semibold underline underline-offset-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        :disabled="loading"
        @click="load(filters)"
      >
        Actualiser la liste
      </button>
    </div>

    <section class="mt-4" aria-label="Sorties à examiner" :aria-busy="loading">
      <div
        v-if="loading"
        class="flex min-h-16 items-center gap-3 text-sm text-muted"
        role="status"
      >
        <LoaderCircle :size="22" class="animate-spin" aria-hidden="true" />
        Chargement des sorties…
      </div>
      <div v-if="!loaded && loading" class="space-y-4" aria-hidden="true">
        <div
          v-for="index in 3"
          :key="index"
          class="h-32 animate-pulse rounded-md bg-subtle"
        />
      </div>
      <template v-if="loaded">
        <p v-if="items.length" class="mb-3 text-sm text-muted">
          {{ total }} {{ total > 1 ? 'sorties' : 'sortie' }}
        </p>
        <ul class="divide-y divide-line">
          <li
            v-for="movie in items"
            :key="movie.tmdb_id"
            class="py-5 first:pt-2"
          >
            <article
              :aria-labelledby="`review-title-${movie.tmdb_id}`"
              class="grid min-w-0 grid-cols-[4rem_minmax(0,1fr)] items-start gap-x-4 gap-y-3 sm:grid-cols-[6rem_minmax(0,1fr)]"
            >
              <PosterImage
                :src="movie.poster_url"
                alt=""
                sizes="(min-width: 640px) 96px, 64px"
                class="aspect-2/3 w-16 overflow-hidden rounded-md bg-subtle sm:row-span-2 sm:w-24"
                image-class="size-full object-cover"
                fallback-class="gap-2 px-1 text-center text-[10px] leading-tight text-muted"
                fallback-variant="compact"
                :fallback-icon-size="22"
                fallback-marker="upcoming-review"
              />
              <div
                class="flex min-w-0 flex-col gap-2 lg:flex-row lg:items-start lg:justify-between lg:gap-4"
              >
                <div class="min-w-0">
                  <h2
                    :id="`review-title-${movie.tmdb_id}`"
                    class="text-base leading-snug font-semibold wrap-anywhere text-ink sm:text-lg"
                  >
                    <NuxtLink
                      :to="`/film/${movie.slug}`"
                      class="hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                      >{{
                        movie.title
                      }}</NuxtLink
                    >
                  </h2>
                  <p class="mt-1 text-sm text-ink">
                    {{
                      movie.french_release_date ? `Sortie française le ${formatFrenchReleaseDate(movie.french_release_date)}` : 'Aucune date de sortie cinéma française'
                    }}
                  </p>
                  <div
                    class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted"
                  >
                    <a
                      :href="`https://www.themoviedb.org/movie/${movie.tmdb_id}`"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="inline-flex min-h-6 items-center font-semibold text-accent underline underline-offset-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                      >TMDB {{ movie.tmdb_id
                      }}<span class="sr-only"> (nouvel onglet)</span></a
                    >
                    <p v-if="movie.assessment_status === 'assessed'">
                      Évalué<span v-if="movie.assessed_at">
                        le
                        <time :datetime="movie.assessed_at">{{
                          formatUpcomingAssessmentTime(movie.assessed_at)
                        }}</time></span
                      >
                    </p>
                  </div>
                </div>
                <div
                  class="flex shrink-0 flex-wrap items-start gap-2 self-start text-xs leading-4"
                >
                  <span
                    class="inline-flex h-6 items-center whitespace-nowrap rounded bg-subtle px-2 font-semibold text-ink"
                    >{{
                      upcomingReviewDecisionLabels[movie.decision]
                    }}</span
                  >
                  <span
                    class="inline-flex h-6 items-center whitespace-nowrap rounded border border-line px-2 text-ink"
                    >{{
                      upcomingReviewVisibility(movie)
                    }}</span
                  >
                </div>
              </div>

              <div class="col-span-2 min-w-0 sm:col-span-1 sm:col-start-2">
                <div class="text-sm">
                  <template v-if="movie.assessment_status === 'pending'">
                    <p class="font-semibold text-ink">
                      En attente d’évaluation
                    </p>
                    <p class="mt-1 text-muted">
                      Évaluation après la prochaine synchronisation réussie.
                    </p>
                  </template>
                  <template v-else>
                    <ul
                      v-if="movie.reason_codes.length"
                      class="list-disc space-y-1 pl-4 text-ink"
                    >
                      <li v-for="reason in movie.reason_codes" :key="reason">
                        {{ upcomingReviewReasonLabels[reason] }}
                      </li>
                    </ul>
                    <p v-else class="text-muted">Aucun signal</p>
                  </template>
                </div>

                <details
                  v-if="movie.assessment_status === 'assessed'"
                  class="mt-1 text-sm"
                >
                  <summary
                    class="min-h-11 cursor-pointer content-center font-semibold text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                  >
                    Dates et notes françaises ({{
                      movie.french_releases.length
                    }})
                  </summary>
                  <ul
                    v-if="movie.french_releases.length"
                    class="space-y-3 border-l-2 border-line pl-4"
                  >
                    <li
                      v-for="(release, index) in movie.french_releases"
                      :key="index"
                      class="min-w-0"
                    >
                      <p class="font-semibold text-ink">
                        <time :datetime="release.date">{{
                          formatFrenchReleaseDate(release.date)
                        }}</time>
                        · {{ frenchReleaseTypeLabels[release.type] }}
                      </p>
                      <p
                        v-if="release.note"
                        class="mt-1 whitespace-pre-wrap wrap-anywhere text-muted"
                      >
                        {{ release.note }}
                      </p>
                    </li>
                  </ul>
                  <p v-else class="text-muted">
                    Aucune date française conservée.
                  </p>
                </details>

                <div
                  class="mt-2 flex flex-wrap gap-2"
                  role="group"
                  :aria-label="`Décision pour ${movie.title}`"
                >
                  <button
                    type="button"
                    :class="secondaryButtonClass"
                    :disabled="!canMutate || movie.decision === 'approved'"
                    @click="decide(movie, 'approved')"
                  >
                    Approuver
                  </button>
                  <button
                    type="button"
                    :class="secondaryButtonClass"
                    :disabled="!canMutate || movie.decision === 'excluded'"
                    @click="decide(movie, 'excluded')"
                  >
                    Exclure
                  </button>
                  <button
                    type="button"
                    :class="secondaryButtonClass"
                    :disabled="!canMutate || movie.decision === 'unreviewed'"
                    @click="decide(movie, 'unreviewed')"
                  >
                    Réinitialiser
                  </button>
                  <span
                    v-if="mutationID === movie.tmdb_id"
                    class="inline-flex min-h-11 items-center gap-2 text-sm text-muted"
                    role="status"
                    ><LoaderCircle
                      :size="16"
                      class="animate-spin"
                      aria-hidden="true"
                    />
                    Enregistrement…</span
                  >
                </div>
              </div>
            </article>
          </li>
        </ul>

        <div
          v-if="!items.length && !loading && !error"
          class="py-8"
          role="status"
        >
          <p class="font-semibold text-ink">
            {{
              filters.q ? 'Aucune sortie ne correspond à la recherche.' : filters.filter === 'needs_review' ? 'Aucune sortie à examiner.' : filters.filter === 'pending_assessment' ? 'Aucune sortie en attente d’évaluation.' : filters.filter === 'all' ? 'Aucune sortie conservée.' : 'Aucune sortie dans cette liste.'
            }}
          </p>
          <div
            v-if="filters.filter === 'needs_review'"
            class="mt-3 flex flex-wrap gap-2"
          >
            <button
              type="button"
              :class="secondaryButtonClass"
              @click="selectFilter('all')"
            >
              Tous
            </button>
            <button
              type="button"
              :class="secondaryButtonClass"
              @click="selectFilter('pending_assessment')"
            >
              En attente d’évaluation
            </button>
          </div>
        </div>

        <nav
          v-if="total > UPCOMING_REVIEW_PAGE_SIZE || filters.page > 1"
          aria-label="Pagination des sorties"
          class="mt-5 grid grid-cols-2 items-center justify-between gap-3 border-t border-line pt-5 sm:flex sm:flex-wrap"
        >
          <button
            type="button"
            :class="secondaryButtonClass"
            :disabled="loading || filters.page <= 1"
            @click="changePage(filters.page - 1)"
          >
            Précédente
          </button>
          <span
            class="order-first col-span-2 min-w-0 text-center text-sm wrap-anywhere text-muted sm:order-none"
            >Page {{ filters.page }} sur {{ pageCount }}</span
          >
          <button
            type="button"
            :class="secondaryButtonClass"
            :disabled="loading || filters.page >= pageCount"
            @click="changePage(filters.page + 1)"
          >
            Suivante
          </button>
        </nav>
      </template>
    </section>
  </main>
</template>
