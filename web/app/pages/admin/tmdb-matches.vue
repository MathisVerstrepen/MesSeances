<script setup lang="ts">
import { AlertTriangle, ArrowLeft, Check, ExternalLink, Film, Layers3, LoaderCircle, LogOut, RefreshCw, Trash2, X } from '@lucide/vue'
import type {
  AdminLocalMovieGroup,
  AdminLocalMovieGroupsResponse,
  AdminLocalMovieMember,
  AdminLocalMovieSource,
  AdminPendingMatch,
  AdminPendingMatchesResponse,
  AdminTMDBCandidate,
  Provider
} from '~/types/api'
import { mergeOwnedQuery, positiveSafeInteger, queriesEqual, singularQueryValue } from '~/utils/routeQuery'

definePageMeta({ middleware: 'admin-auth' })

const PAGE_SIZE = 20
const OWNED_QUERY_KEYS = ['page', 'groups_page'] as const
const api = useMesSeancesApi()
const route = useRoute()
const router = useRouter()

const result = ref<AdminPendingMatchesResponse | null>(null)
const groupsResult = ref<AdminLocalMovieGroupsResponse | null>(null)
const offset = ref(0)
const groupsOffset = ref(0)
const pending = ref(true)
const groupsPending = ref(true)
const matchesError = ref('')
const groupsError = ref('')
const errorMessage = ref('')
const selectedCandidates = ref<Record<string, number>>({})
const manualTmdbIds = ref<Record<string, string | number>>({})
const selectedSources = ref<Record<string, AdminPendingMatch>>({})
const primarySourceKey = ref('')
const activeMutation = ref('')
const activeMutationKind = ref<'candidate' | 'manual' | 'reject' | ''>('')
const mergePending = ref(false)
const unmergePending = ref('')
const rejectConfirmation = ref('')
const unmergeConfirmation = ref('')
const loggingOut = ref(false)
const failedPosters = ref<string[]>([])
const matchesPosterVersion = ref(0)
const groupsPosterVersion = ref(0)
let matchesRequestId = 0
let groupsRequestId = 0
let isMounted = false
let scrollAfterLoad = false
let lastLoadPage = 0
let lastLoadGroupsPage = 0

const page = computed(() => Math.floor(offset.value / PAGE_SIZE) + 1)
const groupsPage = computed(() => Math.floor(groupsOffset.value / PAGE_SIZE) + 1)
const canGoNext = computed(() => (result.value?.items.length ?? 0) === PAGE_SIZE)
const canGroupsGoNext = computed(() => (groupsResult.value?.items.length ?? 0) === PAGE_SIZE)
const selectedSourceList = computed(() => Object.values(selectedSources.value))
const canMerge = computed(() => selectedSourceList.value.length >= 2 && Boolean(primarySourceKey.value && selectedSources.value[primarySourceKey.value]))
const anyMutation = computed(() => Boolean(activeMutation.value || mergePending.value || unmergePending.value))

function sourceKey(source: AdminLocalMovieSource): string {
  return `${source.source_provider}:${source.source_movie_id}`
}

function domKey(source: AdminLocalMovieSource): string {
  return sourceKey(source).replace(/[^a-zA-Z0-9_-]/g, '-')
}

function sameSource(left: AdminLocalMovieSource | null, right: AdminLocalMovieSource): boolean {
  return left?.source_provider === right.source_provider && left.source_movie_id === right.source_movie_id
}

async function loadMatches(background = false) {
  const currentRequest = ++matchesRequestId
  if (!background) pending.value = true
  matchesError.value = ''
  rejectConfirmation.value = ''
  try {
    const response = await api.adminPendingMatches(PAGE_SIZE, offset.value)
    if (currentRequest !== matchesRequestId) return
    matchesPosterVersion.value += 1
    failedPosters.value = failedPosters.value.filter(key => !key.startsWith('matches:'))
    result.value = response
    const nextSelectedCandidates: Record<string, number> = {}
    for (const match of response.items) {
      const key = sourceKey(match)
      const selectedCandidate = selectedCandidates.value[key]
      if (match.status === 'rejected') continue
      if (selectedCandidate !== undefined && match.candidates.some(candidate => candidate.id === selectedCandidate)) {
        nextSelectedCandidates[key] = selectedCandidate
      } else if (match.candidates.length === 1) {
        nextSelectedCandidates[key] = match.candidates[0]!.id
      }
    }
    selectedCandidates.value = nextSelectedCandidates
    if (scrollAfterLoad) {
      scrollAfterLoad = false
      window.scrollTo({ top: 0, behavior: 'smooth' })
    }
  } catch (error) {
    if (currentRequest === matchesRequestId) {
      if (!background) result.value = null
      matchesError.value = getFrenchAdminApiError(error)
    }
  } finally {
    if (!background && currentRequest === matchesRequestId) pending.value = false
  }
}

async function loadGroups() {
  const currentRequest = ++groupsRequestId
  groupsPending.value = true
  groupsError.value = ''
  unmergeConfirmation.value = ''
  try {
    const response = await api.adminLocalMovieGroups(PAGE_SIZE, groupsOffset.value)
    if (currentRequest !== groupsRequestId) return
    groupsPosterVersion.value += 1
    failedPosters.value = failedPosters.value.filter(key => !key.startsWith('groups:'))
    groupsResult.value = response
  } catch (error) {
    if (currentRequest === groupsRequestId) {
      groupsResult.value = null
      groupsError.value = getFrenchAdminApiError(error)
    }
  } finally {
    if (currentRequest === groupsRequestId) groupsPending.value = false
  }
}

function adminQuery(nextPage = page.value, nextGroupsPage = groupsPage.value) {
  return mergeOwnedQuery(route.query, OWNED_QUERY_KEYS, {
    page: nextPage === 1 ? undefined : String(nextPage),
    groups_page: nextGroupsPage === 1 ? undefined : String(nextGroupsPage)
  })
}

function hydrateRoute() {
  const requestedPage = positiveSafeInteger(singularQueryValue(route.query.page)) ?? 1
  const requestedGroupsPage = positiveSafeInteger(singularQueryValue(route.query.groups_page)) ?? 1
  const nextOffset = (requestedPage - 1) * PAGE_SIZE
  const nextGroupsOffset = (requestedGroupsPage - 1) * PAGE_SIZE
  const safePage = Number.isSafeInteger(nextOffset) ? requestedPage : 1
  const safeGroupsPage = Number.isSafeInteger(nextGroupsOffset) ? requestedGroupsPage : 1
  offset.value = (safePage - 1) * PAGE_SIZE
  groupsOffset.value = (safeGroupsPage - 1) * PAGE_SIZE
  return adminQuery(safePage, safeGroupsPage)
}

async function applyRoute() {
  const canonicalQuery = hydrateRoute()
  if (!queriesEqual(route.query, canonicalQuery)) {
    await router.replace({ query: canonicalQuery })
    return
  }
  const loads: Promise<void>[] = []
  if (page.value !== lastLoadPage) {
    lastLoadPage = page.value
    loads.push(loadMatches())
  }
  if (groupsPage.value !== lastLoadGroupsPage) {
    lastLoadGroupsPage = groupsPage.value
    loads.push(loadGroups())
  }
  await Promise.all(loads)
}

async function refreshResources() {
  await Promise.all([loadMatches(), loadGroups()])
  const nextPage = !matchesError.value && offset.value > 0 && result.value?.items.length === 0 ? page.value - 1 : page.value
  const nextGroupsPage = !groupsError.value && groupsOffset.value > 0 && groupsResult.value?.items.length === 0 ? groupsPage.value - 1 : groupsPage.value
  if (nextPage !== page.value || nextGroupsPage !== groupsPage.value) {
    await router.replace({ query: adminQuery(nextPage, nextGroupsPage) })
  }
}

async function refreshAfterDecision() {
  await loadMatches(true)
  if (!matchesError.value && offset.value > 0 && result.value?.items.length === 0) {
    await router.replace({ query: adminQuery(page.value - 1) })
  }
}

function clearMergeSelection() {
  selectedSources.value = {}
  primarySourceKey.value = ''
}

function toggleMergeSelection(match: AdminPendingMatch) {
  const key = sourceKey(match)
  if (selectedSources.value[key]) {
    const next = { ...selectedSources.value }
    delete next[key]
    selectedSources.value = next
    if (primarySourceKey.value === key) primarySourceKey.value = ''
    return
  }
  selectedSources.value = { ...selectedSources.value, [key]: match }
}

function removeMergeSelection(match: AdminPendingMatch) {
  if (selectedSources.value[sourceKey(match)]) toggleMergeSelection(match)
}

async function handleMutationError(cause: unknown) {
  errorMessage.value = getFrenchAdminApiError(cause)
  if (getApiErrorStatus(cause) === 409) {
    clearMergeSelection()
    await refreshResources()
  }
}

async function approveWithTmdbId(match: AdminPendingMatch, tmdbId: number, kind: 'candidate' | 'manual') {
  if (anyMutation.value || match.status === 'rejected') return
  const key = sourceKey(match)
  activeMutation.value = key
  activeMutationKind.value = kind
  errorMessage.value = ''
  try {
    await api.adminApproveMatch(match.source_provider, match.source_movie_id, tmdbId)
    if (kind === 'manual') delete manualTmdbIds.value[key]
    removeMergeSelection(match)
    await refreshAfterDecision()
  } catch (error) {
    await handleMutationError(error)
  } finally {
    activeMutation.value = ''
    activeMutationKind.value = ''
  }
}

async function approve(match: AdminPendingMatch) {
  const tmdbId = selectedCandidates.value[sourceKey(match)]
  if (tmdbId) await approveWithTmdbId(match, tmdbId, 'candidate')
}

function manualTmdbId(match: AdminPendingMatch): number | null {
  const value = String(manualTmdbIds.value[sourceKey(match)] ?? '').trim()
  if (!/^\d+$/.test(value)) return null
  const tmdbId = Number(value)
  return Number.isSafeInteger(tmdbId) && tmdbId > 0 ? tmdbId : null
}

async function assignManual(match: AdminPendingMatch) {
  const tmdbId = manualTmdbId(match)
  if (tmdbId !== null) await approveWithTmdbId(match, tmdbId, 'manual')
}

async function reject(match: AdminPendingMatch) {
  if (anyMutation.value || match.status === 'rejected') return
  const key = sourceKey(match)
  activeMutation.value = key
  activeMutationKind.value = 'reject'
  errorMessage.value = ''
  try {
    await api.adminRejectMatch(match.source_provider, match.source_movie_id)
    await refreshAfterDecision()
  } catch (error) {
    await handleMutationError(error)
  } finally {
    activeMutation.value = ''
    activeMutationKind.value = ''
  }
}

async function mergeSelectedSources() {
  if (!canMerge.value || anyMutation.value) return
  const primary = selectedSources.value[primarySourceKey.value]
  if (!primary) return
  mergePending.value = true
  errorMessage.value = ''
  try {
    await api.adminCreateLocalMovieGroup({
      members: selectedSourceList.value.map(({ source_provider, source_movie_id }) => ({ source_provider, source_movie_id })),
      primary: { source_provider: primary.source_provider, source_movie_id: primary.source_movie_id }
    })
    clearMergeSelection()
    await refreshResources()
  } catch (error) {
    await handleMutationError(error)
  } finally {
    mergePending.value = false
  }
}

async function unmerge(group: AdminLocalMovieGroup) {
  if (anyMutation.value) return
  unmergePending.value = group.local_movie_id
  errorMessage.value = ''
  try {
    await api.adminUnmergeLocalMovie(group.local_movie_id)
    await refreshResources()
  } catch (error) {
    if (getApiErrorStatus(error) === 404) {
      errorMessage.value = 'Ce regroupement avait déjà été supprimé. Les listes ont été actualisées.'
      await refreshResources()
    } else {
      await handleMutationError(error)
    }
  } finally {
    unmergePending.value = ''
    unmergeConfirmation.value = ''
  }
}

function changePage(nextOffset: number) {
  if (pending.value || anyMutation.value || nextOffset < 0 || nextOffset === offset.value) return
  scrollAfterLoad = true
  router.push({ query: adminQuery(Math.floor(nextOffset / PAGE_SIZE) + 1) })
}

function changeGroupsPage(nextOffset: number) {
  if (groupsPending.value || anyMutation.value || nextOffset < 0 || nextOffset === groupsOffset.value) return
  router.push({ query: adminQuery(page.value, Math.floor(nextOffset / PAGE_SIZE) + 1) })
}

async function logout() {
  if (loggingOut.value) return
  loggingOut.value = true
  errorMessage.value = ''
  try {
    await api.adminLogout()
    await navigateTo('/admin/login')
  } catch (error) {
    errorMessage.value = getFrenchAdminApiError(error)
  } finally {
    loggingOut.value = false
  }
}

function candidateRuntime(candidate: AdminTMDBCandidate): string {
  return candidate.runtime_minutes ? `${candidate.runtime_minutes} min` : 'Durée non renseignée'
}

function candidateScore(candidate: AdminTMDBCandidate): string {
  return candidate.score !== undefined ? `${Math.round(candidate.score * 100)} %` : 'Non renseigné'
}

function providerLabel(provider: Provider): string {
  return provider === 'ugc' ? 'UGC' : 'Kinepolis'
}

function statusLabel(match: AdminPendingMatch): string {
  if (match.status === 'review_required') return 'À vérifier'
  if (match.status === 'rejected') return 'Non-TMDB'
  return 'Sans correspondance'
}

function sourcePosterKey(source: AdminPendingMatch): string {
  return `matches:${matchesPosterVersion.value}:source:${sourceKey(source)}:${source.source_poster_url ?? ''}`
}

function memberPosterKey(group: AdminLocalMovieGroup, member: AdminLocalMovieMember): string {
  return `groups:${groupsPosterVersion.value}:${group.local_movie_id}:${sourceKey(member)}:${member.source_poster_url ?? ''}`
}

function candidatePosterKey(match: AdminPendingMatch, candidate: AdminTMDBCandidate): string {
  return `matches:${matchesPosterVersion.value}:tmdb:${sourceKey(match)}:${candidate.id}:${candidate.poster_url ?? ''}`
}

function posterAvailable(url: string | null | undefined, key: string): boolean {
  return Boolean(url?.trim()) && !failedPosters.value.includes(key)
}

function markPosterUnavailable(key: string) {
  if (!failedPosters.value.includes(key)) failedPosters.value = [...failedPosters.value, key]
}

watch(() => route.query, () => {
  if (isMounted) applyRoute()
})
onMounted(() => {
  isMounted = true
  applyRoute()
})
useHead({ title: 'Identités des films - MesSeances' })
</script>

<template>
  <main class="mx-auto max-w-[1800px] px-4 py-5 sm:px-6 sm:py-7 lg:px-8 lg:py-8">
    <div class="flex flex-col gap-4 border-b border-line pb-5 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <NuxtLink to="/admin" class="mb-2 inline-flex items-center gap-1 text-sm font-semibold text-muted hover:text-accent">
          <ArrowLeft :size="16" aria-hidden="true" /> Administration
        </NuxtLink>
        <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Identités des films</h1>
      </div>
      <button type="button" class="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink transition hover:border-line-hover disabled:cursor-not-allowed disabled:opacity-50" :disabled="loggingOut" @click="logout">
        <LoaderCircle v-if="loggingOut" :size="17" class="animate-spin" aria-hidden="true" />
        <LogOut v-else :size="17" aria-hidden="true" />
        {{ loggingOut ? 'Déconnexion…' : 'Se déconnecter' }}
      </button>
    </div>

    <div v-if="errorMessage" class="mt-6 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <p class="flex-1">{{ errorMessage }}</p>
      <button type="button" class="font-semibold underline underline-offset-2" :disabled="anyMutation" @click="errorMessage = ''">Fermer</button>
    </div>

    <section class="mt-7" aria-labelledby="pending-matches-title">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 id="pending-matches-title" class="text-xl font-semibold text-ink">Films sans identité TMDB résolue</h2>
        <button type="button" class="inline-flex items-center gap-2 text-sm font-semibold text-accent disabled:opacity-50" :disabled="pending || anyMutation" @click="loadMatches()">
          <RefreshCw :size="16" :class="pending ? 'animate-spin' : ''" aria-hidden="true" /> Actualiser
        </button>
      </div>

      <div v-if="selectedSourceList.length" class="mt-4 rounded-lg border border-accent-line bg-accent-soft p-4" aria-labelledby="merge-selection-title">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h3 id="merge-selection-title" class="font-semibold text-ink">Sélection pour regroupement ({{ selectedSourceList.length }})</h3>
          <button type="button" class="text-sm font-semibold text-muted underline" :disabled="mergePending" @click="clearMergeSelection">Effacer</button>
        </div>
        <fieldset class="mt-3" :disabled="mergePending">
          <legend class="sr-only">Choisir la source principale</legend>
          <ul class="flex flex-wrap gap-2">
            <li v-for="match in selectedSourceList" :key="sourceKey(match)" class="flex items-center gap-2 rounded-md border border-accent-line bg-surface px-3 py-2 text-sm">
              <label class="flex cursor-pointer items-center gap-2">
                <input v-model="primarySourceKey" type="radio" name="local-primary" :value="sourceKey(match)" class="accent-accent" />
                <span><span class="font-semibold">{{ match.source_title }}</span> · {{ providerLabel(match.source_provider) }}</span>
              </label>
              <button type="button" class="text-muted hover:text-red-700" :aria-label="`Retirer ${match.source_title}`" @click="removeMergeSelection(match)"><X :size="16" aria-hidden="true" /></button>
            </li>
          </ul>
        </fieldset>
        <div class="mt-4 flex flex-wrap items-center gap-3">
          <button type="button" class="button-primary" :disabled="!canMerge || anyMutation" @click="mergeSelectedSources">
            <LoaderCircle v-if="mergePending" :size="17" class="animate-spin" aria-hidden="true" /><Layers3 v-else :size="17" aria-hidden="true" /> Regrouper les films
          </button>
          <p v-if="selectedSourceList.length < 2" class="text-sm text-muted">Sélectionnez au moins deux films.</p>
          <p v-else-if="!primarySourceKey" class="text-sm text-muted">Choisissez la source principale.</p>
        </div>
      </div>

      <div v-if="matchesError" class="mt-4 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
        <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
        <div><p>{{ matchesError }}</p><button type="button" class="mt-2 font-semibold underline" @click="loadMatches()">Réessayer</button></div>
      </div>
      <div v-if="pending" class="state-panel mt-4" role="status" aria-live="polite">
        <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" /><p>Chargement des films…</p>
      </div>
      <div v-else-if="result && !result.items.length" class="state-panel mt-4">
        <Check :size="30" class="text-accent" aria-hidden="true" /><p>Aucun film à traiter.</p>
      </div>
      <ul v-else-if="result?.items.length" class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2" aria-label="Films sans identité TMDB résolue">
        <li v-for="match in result.items" :key="sourceKey(match)" class="min-w-0 rounded-lg border border-line bg-surface p-4 shadow-sm sm:p-5" :class="match.status === 'rejected' ? '' : 'lg:col-span-2'">
          <div class="mb-4 flex flex-wrap items-center justify-between gap-3 border-b border-line pb-3">
            <label class="flex cursor-pointer items-center gap-2 text-sm font-semibold text-ink" :for="`merge-${domKey(match)}`">
              <input :id="`merge-${domKey(match)}`" type="checkbox" class="size-4 accent-accent" :checked="Boolean(selectedSources[sourceKey(match)])" :disabled="mergePending" @change="toggleMergeSelection(match)" /> Sélectionner pour un regroupement local
            </label>
            <span class="rounded-full px-2 py-0.5 text-xs font-semibold" :class="match.status === 'review_required' ? 'bg-amber-100 text-amber-800' : match.status === 'rejected' ? 'bg-violet-100 text-violet-800' : 'bg-subtle text-muted'">{{ statusLabel(match) }}</span>
          </div>

          <div class="grid min-w-0 gap-5" :class="match.status === 'rejected' ? '' : 'lg:grid-cols-[14rem_minmax(0,1fr)] lg:gap-6'">
            <section class="min-w-0" :aria-labelledby="`source-title-${domKey(match)}`">
              <p class="mb-2 text-xs font-semibold uppercase tracking-wide text-muted"><BrandedText :text="`Source ${providerLabel(match.source_provider)}`" /></p>
              <div class="flex min-w-0 gap-3">
                <div class="aspect-[2/3] w-20 shrink-0 overflow-hidden rounded-md border border-line bg-subtle sm:w-24 lg:w-20">
                  <img v-if="posterAvailable(match.source_poster_url, sourcePosterKey(match))" :src="match.source_poster_url" :alt="`Affiche ${providerLabel(match.source_provider)} de ${match.source_title}`" class="h-full w-full object-cover" loading="lazy" decoding="async" @error="markPosterUnavailable(sourcePosterKey(match))" />
                  <div v-else class="flex h-full flex-col items-center justify-center gap-1 px-2 text-center text-muted"><Film :size="24" aria-hidden="true" /><span class="text-[11px] font-medium leading-tight">Affiche indisponible</span></div>
                </div>
                <div class="min-w-0 flex-1">
                  <h3 :id="`source-title-${domKey(match)}`" class="line-clamp-3 text-sm font-semibold leading-snug text-ink">{{ match.source_title }}</h3>
                  <dl class="mt-2 space-y-1 text-xs text-muted"><div><dt class="sr-only">Durée source</dt><dd>{{ match.source_runtime_minutes }} min</dd></div><div class="break-all"><dt class="inline"><BrandedText :text="`ID ${providerLabel(match.source_provider)} :`" /></dt> <dd class="inline">{{ match.source_movie_id }}</dd></div></dl>
                  <a v-if="match.source_detail_url" :href="match.source_detail_url" target="_blank" rel="noopener noreferrer" class="mt-3 inline-flex items-center gap-1 text-xs font-semibold text-accent hover:underline" :aria-label="`Voir sur ${providerLabel(match.source_provider)}, ouverture dans un nouvel onglet`"><BrandedText :text="`Voir sur ${providerLabel(match.source_provider)}`" decorative /><ExternalLink :size="13" aria-hidden="true" /></a>
                </div>
              </div>
            </section>

            <fieldset v-if="match.status !== 'rejected'" class="min-w-0" :disabled="anyMutation">
              <legend class="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">Candidats TMDB</legend>
              <div v-if="match.candidates.length" class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-5">
                <div v-for="candidate in match.candidates" :key="candidate.id" class="min-w-0 rounded-md border p-3 transition focus-within:ring-2 focus-within:ring-accent focus-within:ring-offset-2" :class="selectedCandidates[sourceKey(match)] === candidate.id ? 'border-accent bg-accent-soft' : 'border-line bg-surface hover:border-line-hover'">
                  <label :for="`candidate-${domKey(match)}-${candidate.id}`" class="flex min-w-0 cursor-pointer items-start gap-2.5">
                    <input :id="`candidate-${domKey(match)}-${candidate.id}`" v-model="selectedCandidates[sourceKey(match)]" type="radio" :name="`candidate-${domKey(match)}`" :value="candidate.id" class="mt-1 shrink-0 accent-accent" />
                    <span class="aspect-[2/3] w-20 shrink-0 overflow-hidden rounded border border-line bg-subtle sm:w-24 lg:w-20">
                      <img v-if="posterAvailable(candidate.poster_url, candidatePosterKey(match, candidate))" :src="candidate.poster_url" :alt="`Affiche TMDB de ${candidate.title}`" class="h-full w-full object-cover" loading="lazy" decoding="async" @error="markPosterUnavailable(candidatePosterKey(match, candidate))" />
                      <span v-else class="flex h-full flex-col items-center justify-center gap-1 px-2 text-center text-muted"><Film :size="24" aria-hidden="true" /><span class="text-[11px] font-medium leading-tight">Affiche indisponible</span></span>
                    </span>
                    <span class="min-w-0 flex-1"><span class="line-clamp-2 text-sm font-semibold leading-snug text-ink">{{ candidate.title }}</span><span class="mt-1 line-clamp-2 text-xs leading-snug text-muted">Titre original : {{ candidate.original_title || 'non renseigné' }}</span><span class="mt-2 block space-y-1 text-xs text-muted"><span class="block">ID TMDB : {{ candidate.id }}</span><span class="block">{{ candidateRuntime(candidate) }}</span><span class="block">Score : {{ candidateScore(candidate) }}</span></span></span>
                  </label>
                  <a v-if="candidate.detail_url" :href="candidate.detail_url" target="_blank" rel="noopener noreferrer" class="mt-2 inline-flex items-center gap-1 text-xs font-semibold text-accent hover:underline">Voir sur TMDB <ExternalLink :size="13" aria-hidden="true" /></a>
                </div>
              </div>
              <p v-else class="rounded-md border border-dashed border-line p-4 text-sm text-muted">Aucun candidat enregistré.</p>
            </fieldset>
          </div>

          <div v-if="match.status !== 'rejected'" class="mt-4 grid gap-4 border-t border-line pt-4 lg:grid-cols-[auto_minmax(18rem,1fr)_auto] lg:items-end">
            <button type="button" class="button-primary" :disabled="!selectedCandidates[sourceKey(match)] || anyMutation" @click="approve(match)"><LoaderCircle v-if="activeMutation === sourceKey(match) && activeMutationKind === 'candidate'" :size="17" class="animate-spin" aria-hidden="true" /><Check v-else :size="17" aria-hidden="true" />Valider le candidat</button>
            <form class="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-end" @submit.prevent="assignManual(match)">
              <div class="min-w-0 flex-1"><label :for="`manual-tmdb-${domKey(match)}`" class="mb-1.5 block text-sm font-semibold text-ink">Identifiant TMDB</label><input :id="`manual-tmdb-${domKey(match)}`" v-model="manualTmdbIds[sourceKey(match)]" class="field" type="number" min="1" max="9007199254740991" step="1" inputmode="numeric" :disabled="anyMutation" /></div>
              <button type="submit" class="button-primary shrink-0" :disabled="manualTmdbId(match) === null || anyMutation"><LoaderCircle v-if="activeMutation === sourceKey(match) && activeMutationKind === 'manual'" :size="17" class="animate-spin" aria-hidden="true" /><Check v-else :size="17" aria-hidden="true" />Associer cet identifiant TMDB</button>
            </form>
            <div v-if="rejectConfirmation === sourceKey(match)" class="flex flex-col gap-2 sm:flex-row sm:items-center lg:justify-end"><span class="text-sm font-semibold text-red-800">Marquer ce film comme Non-TMDB ?</span><button type="button" class="h-9 rounded-md bg-red-700 px-3 text-sm font-semibold text-white disabled:opacity-50" :disabled="anyMutation" @click="reject(match)">Confirmer</button><button type="button" class="h-9 rounded-md border border-line px-3 text-sm font-semibold text-ink" :disabled="anyMutation" @click="rejectConfirmation = ''">Annuler</button></div>
            <button v-else type="button" class="inline-flex h-9 items-center justify-center gap-2 rounded-md px-3 text-sm font-semibold text-red-700 hover:bg-red-50 disabled:opacity-50 lg:justify-self-end" :disabled="anyMutation" @click="rejectConfirmation = sourceKey(match)"><X :size="16" aria-hidden="true" /> Aucun résultat TMDB</button>
          </div>
        </li>
      </ul>

      <nav v-if="!pending && !matchesError && (offset > 0 || canGoNext)" class="mt-8 flex items-center justify-center gap-4 border-t border-line pt-6" aria-label="Pagination des films sans identité TMDB résolue">
        <button type="button" class="h-10 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink disabled:opacity-50" :disabled="offset === 0 || anyMutation" @click="changePage(offset - PAGE_SIZE)">Précédent</button>
        <span class="text-sm text-muted" aria-live="polite">Page {{ page }}</span>
        <button type="button" class="h-10 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink disabled:opacity-50" :disabled="!canGoNext || anyMutation" @click="changePage(offset + PAGE_SIZE)">Suivant</button>
      </nav>
    </section>

    <section class="mt-8 border-t border-line pt-7" aria-labelledby="local-groups-title">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 id="local-groups-title" class="text-xl font-semibold text-ink">Regroupements locaux</h2>
        <button type="button" class="inline-flex items-center gap-2 text-sm font-semibold text-accent disabled:opacity-50" :disabled="groupsPending || anyMutation" @click="loadGroups">
          <RefreshCw :size="16" :class="groupsPending ? 'animate-spin' : ''" aria-hidden="true" /> Actualiser
        </button>
      </div>

      <div v-if="groupsError" class="mt-4 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
        <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
        <div><p>{{ groupsError }}</p><button type="button" class="mt-2 font-semibold underline" @click="loadGroups">Réessayer</button></div>
      </div>
      <div v-else-if="groupsPending" class="state-panel mt-4" role="status" aria-live="polite">
        <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" /><p>Chargement des regroupements…</p>
      </div>
      <div v-else-if="!groupsResult?.items.length" class="state-panel mt-4">
        <Layers3 :size="30" class="text-muted" aria-hidden="true" /><p>Aucun regroupement local.</p>
      </div>
      <ul v-else class="mt-3 grid gap-3 xl:grid-cols-2 2xl:grid-cols-3" aria-label="Regroupements locaux actifs">
        <li v-for="group in groupsResult.items" :key="group.local_movie_id" class="overflow-hidden rounded-md border border-line bg-surface">
          <div class="flex flex-wrap items-start justify-between gap-2 px-3 py-2.5">
            <div class="min-w-0 text-xs">
              <h3 class="break-all text-sm font-semibold text-ink">{{ group.local_movie_id }}</h3>
              <p class="mt-0.5 text-muted">Primaire : {{ providerLabel(group.primary.source_provider) }} · {{ group.primary.source_movie_id }}</p>
              <p v-if="group.metadata_source" class="mt-0.5 text-muted">
                Métadonnées : {{ providerLabel(group.metadata_source.source_provider) }} · {{ group.metadata_source.source_movie_id }}
                <span v-if="!sameSource(group.metadata_source, group.primary)" class="font-semibold text-amber-700">(repli)</span>
              </p>
              <p v-else class="mt-0.5 font-semibold text-red-700">Aucune source disponible</p>
            </div>
            <div v-if="unmergeConfirmation === group.local_movie_id" class="flex flex-wrap items-center justify-end gap-1.5">
              <span class="text-xs font-semibold text-red-800">Dissocier ?</span>
              <button type="button" class="h-8 rounded-md bg-red-700 px-2.5 text-xs font-semibold text-white hover:bg-red-800 disabled:opacity-50" :disabled="anyMutation" @click="unmerge(group)">
                <LoaderCircle v-if="unmergePending === group.local_movie_id" :size="14" class="inline animate-spin" aria-hidden="true" /> Confirmer
              </button>
              <button type="button" class="h-8 rounded-md border border-line px-2.5 text-xs font-semibold text-ink" :disabled="anyMutation" @click="unmergeConfirmation = ''">Annuler</button>
            </div>
            <button v-else type="button" class="inline-flex h-8 items-center gap-1.5 rounded-md px-2 text-xs font-semibold text-red-700 hover:bg-red-50 disabled:opacity-50" :disabled="anyMutation" @click="unmergeConfirmation = group.local_movie_id">
              <Trash2 :size="14" aria-hidden="true" /> Dissocier
            </button>
          </div>
          <ul class="divide-y divide-line border-t border-line" :aria-label="`Membres de ${group.local_movie_id}`">
            <li v-for="member in group.members" :key="sourceKey(member)" class="flex min-w-0 items-center gap-2 px-3 py-2" :class="member.available ? '' : 'bg-subtle text-muted'">
              <div class="aspect-[2/3] w-8 shrink-0 overflow-hidden rounded-sm bg-subtle">
                <img v-if="posterAvailable(member.source_poster_url, memberPosterKey(group, member))" :src="member.source_poster_url!" :alt="`Affiche de ${member.source_title ?? member.source_movie_id}`" class="h-full w-full object-cover" loading="lazy" decoding="async" @error="markPosterUnavailable(memberPosterKey(group, member))" />
                <div v-else class="flex h-full items-center justify-center text-muted"><Film :size="15" aria-hidden="true" /></div>
              </div>
              <div class="min-w-0 flex-1 text-xs leading-tight">
                <p class="truncate font-semibold text-ink">{{ member.source_title ?? 'Source indisponible' }}</p>
                <p class="mt-0.5 truncate text-muted">{{ providerLabel(member.source_provider) }} · {{ member.source_movie_id }}<span v-if="member.source_runtime_minutes"> · {{ member.source_runtime_minutes }} min</span></p>
              </div>
              <div class="shrink-0 text-right text-[11px] font-semibold leading-tight">
                <p v-if="sameSource(member, group.primary)" class="text-accent">Primaire</p>
                <p :class="member.available ? 'text-emerald-700' : 'text-red-700'">{{ member.available ? 'Disponible' : 'Indisponible' }}</p>
              </div>
            </li>
          </ul>
        </li>
      </ul>

      <nav v-if="!groupsPending && !groupsError && (groupsOffset > 0 || canGroupsGoNext)" class="mt-5 flex items-center justify-center gap-4" aria-label="Pagination des regroupements locaux">
        <button type="button" class="h-10 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink disabled:opacity-50" :disabled="groupsOffset === 0 || anyMutation" @click="changeGroupsPage(groupsOffset - PAGE_SIZE)">Précédent</button>
        <span class="text-sm text-muted" aria-live="polite">Page {{ groupsPage }}</span>
        <button type="button" class="h-10 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink disabled:opacity-50" :disabled="!canGroupsGoNext || anyMutation" @click="changeGroupsPage(groupsOffset + PAGE_SIZE)">Suivant</button>
      </nav>
    </section>
  </main>
</template>
