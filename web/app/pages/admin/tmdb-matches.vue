<script setup lang="ts">
import { AlertTriangle, ArrowLeft, Check, ExternalLink, Film, LoaderCircle, LogOut, RefreshCw, Trash2 } from '@lucide/vue'
import type { AdminPendingMatch, AdminPendingMatchesResponse, AdminTMDBCandidate, Provider } from '~/types/api'

definePageMeta({ middleware: 'admin-auth' })

const PAGE_SIZE = 20
const api = useMovieFlowApi()
const result = ref<AdminPendingMatchesResponse | null>(null)
const offset = ref(0)
const pending = ref(true)
const errorMessage = ref('')
const selectedCandidates = ref<Record<string, number>>({})
const manualTmdbIds = ref<Record<string, string | number>>({})
const activeMutation = ref('')
const activeMutationKind = ref<'candidate' | 'manual' | 'reject' | ''>('')
const rejectConfirmation = ref('')
const loggingOut = ref(false)
const failedPosters = ref<string[]>([])
let requestId = 0

const page = computed(() => Math.floor(offset.value / PAGE_SIZE) + 1)
const canGoNext = computed(() => (result.value?.items.length ?? 0) === PAGE_SIZE)

async function loadMatches() {
  const currentRequest = ++requestId
  pending.value = true
  errorMessage.value = ''
  rejectConfirmation.value = ''
  try {
    const response = await api.adminPendingMatches(PAGE_SIZE, offset.value)
    if (currentRequest === requestId) {
      result.value = response
      failedPosters.value = []
      selectedCandidates.value = Object.fromEntries(
        response.items.filter((match) => match.candidates.length === 1).map((match) => [match.source_movie_id, match.candidates[0]!.id])
      )
    }
  } catch (error) {
    if (currentRequest === requestId) {
      result.value = null
      errorMessage.value = getFrenchAdminApiError(error)
    }
  } finally {
    if (currentRequest === requestId) pending.value = false
  }
}

async function refreshAfterDecision() {
  await loadMatches()
  if (!errorMessage.value && offset.value > 0 && result.value?.items.length === 0) {
    offset.value = Math.max(0, offset.value - PAGE_SIZE)
  }
}

async function approveWithTmdbId(match: AdminPendingMatch, tmdbId: number, kind: 'candidate' | 'manual') {
  if (activeMutation.value) return
  activeMutation.value = match.source_movie_id
  activeMutationKind.value = kind
  errorMessage.value = ''
  try {
    await api.adminApproveMatch(match.source_provider, match.source_movie_id, tmdbId)
    if (kind === 'manual') delete manualTmdbIds.value[match.source_movie_id]
    await refreshAfterDecision()
  } catch (error) {
    errorMessage.value = getFrenchAdminApiError(error)
  } finally {
    activeMutation.value = ''
    activeMutationKind.value = ''
  }
}

async function approve(match: AdminPendingMatch) {
  const tmdbId = selectedCandidates.value[match.source_movie_id]
  if (!tmdbId) return
  await approveWithTmdbId(match, tmdbId, 'candidate')
}

function manualTmdbId(match: AdminPendingMatch): number | null {
  const value = String(manualTmdbIds.value[match.source_movie_id] ?? '').trim()
  if (!/^\d+$/.test(value)) return null
  const tmdbId = Number(value)
  return Number.isSafeInteger(tmdbId) && tmdbId > 0 ? tmdbId : null
}

async function assignManual(match: AdminPendingMatch) {
  const tmdbId = manualTmdbId(match)
  if (tmdbId === null) return
  await approveWithTmdbId(match, tmdbId, 'manual')
}

async function reject(match: AdminPendingMatch) {
  if (activeMutation.value) return
  activeMutation.value = match.source_movie_id
  activeMutationKind.value = 'reject'
  errorMessage.value = ''
  try {
    await api.adminRejectMatch(match.source_provider, match.source_movie_id)
    await refreshAfterDecision()
  } catch (error) {
    errorMessage.value = getFrenchAdminApiError(error)
  } finally {
    activeMutation.value = ''
    activeMutationKind.value = ''
  }
}

function changePage(nextOffset: number) {
  if (pending.value || activeMutation.value || nextOffset < 0 || nextOffset === offset.value) return
  offset.value = nextOffset
  window.scrollTo({ top: 0, behavior: 'smooth' })
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

function sourcePosterKey(match: AdminPendingMatch): string {
  return `${match.source_provider}:${match.source_movie_id}`
}

function candidatePosterKey(match: AdminPendingMatch, candidate: AdminTMDBCandidate): string {
  return `tmdb:${match.source_movie_id}:${candidate.id}`
}

function posterAvailable(url: string | undefined, key: string): boolean {
  return Boolean(url?.trim()) && !failedPosters.value.includes(key)
}

function markPosterUnavailable(key: string) {
  if (!failedPosters.value.includes(key)) failedPosters.value = [...failedPosters.value, key]
}

watch(offset, loadMatches)
onMounted(loadMatches)
useHead({ title: 'Correspondances TMDB — MovieFlow' })
</script>

<template>
  <main class="mx-auto max-w-[1800px] px-4 py-5 sm:px-6 sm:py-7 lg:px-8 lg:py-8">
    <div class="flex flex-col gap-4 border-b border-line pb-5 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <NuxtLink to="/admin" class="mb-2 inline-flex items-center gap-1 text-sm font-semibold text-muted hover:text-accent">
          <ArrowLeft :size="16" aria-hidden="true" /> Administration
        </NuxtLink>
        <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Correspondances TMDB en attente</h1>
      </div>
      <button type="button" class="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink transition hover:border-stone-400 disabled:cursor-not-allowed disabled:opacity-50" :disabled="loggingOut" @click="logout">
        <LoaderCircle v-if="loggingOut" :size="17" class="animate-spin" aria-hidden="true" />
        <LogOut v-else :size="17" aria-hidden="true" />
        {{ loggingOut ? 'Déconnexion…' : 'Se déconnecter' }}
      </button>
    </div>

    <div v-if="errorMessage && !pending" class="mt-6 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <div class="flex-1">
        <p>{{ errorMessage }}</p>
        <button type="button" class="mt-3 inline-flex items-center gap-2 font-semibold underline underline-offset-2" @click="loadMatches">
          <RefreshCw :size="16" aria-hidden="true" /> Réessayer
        </button>
      </div>
    </div>

    <div v-if="pending" class="state-panel mt-6" role="status" aria-live="polite">
      <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
      <p>Chargement des correspondances…</p>
    </div>

    <div v-else-if="!errorMessage && !result?.items.length" class="state-panel mt-6">
      <Check :size="30" class="text-accent" aria-hidden="true" />
      <p>Aucune correspondance à valider.</p>
    </div>

    <ul v-else-if="result?.items.length" class="mt-5 space-y-4" aria-label="Correspondances à valider">
      <li v-for="match in result.items" :key="match.source_movie_id" class="min-w-0 rounded-lg border border-line bg-surface p-4 shadow-sm sm:p-5">
        <div class="grid min-w-0 gap-5 lg:grid-cols-[14rem_minmax(0,1fr)] lg:gap-6">
          <section class="min-w-0" :aria-labelledby="`source-title-${match.source_movie_id}`">
            <div class="mb-2 flex flex-wrap items-center gap-2">
              <p class="text-xs font-semibold uppercase tracking-wide text-muted"><BrandedText :text="`Source ${providerLabel(match.source_provider)}`" /></p>
              <span
                class="rounded-full px-2 py-0.5 text-xs font-semibold"
                :class="match.status === 'review_required' ? 'bg-orange-100 text-orange-800' : 'bg-stone-200 text-stone-700'"
              >
                {{ match.status === 'review_required' ? 'À vérifier' : 'Sans correspondance' }}
              </span>
            </div>
            <div class="flex min-w-0 gap-3">
              <div class="aspect-[2/3] w-20 shrink-0 overflow-hidden rounded-md border border-line bg-subtle sm:w-24 lg:w-20">
                <img
                  v-if="posterAvailable(match.source_poster_url, sourcePosterKey(match))"
                  :src="match.source_poster_url"
                  :alt="`Affiche ${providerLabel(match.source_provider)} de ${match.source_title}`"
                  class="h-full w-full object-cover"
                  loading="lazy"
                  decoding="async"
                  @error="markPosterUnavailable(sourcePosterKey(match))"
                />
                <div v-else class="flex h-full flex-col items-center justify-center gap-1 px-2 text-center text-muted">
                  <Film :size="24" aria-hidden="true" />
                  <span class="text-[11px] font-medium leading-tight">Affiche indisponible</span>
                </div>
              </div>
              <div class="min-w-0 flex-1">
                <h2 :id="`source-title-${match.source_movie_id}`" class="line-clamp-3 text-sm font-semibold leading-snug text-ink">{{ match.source_title }}</h2>
                <dl class="mt-2 space-y-1 text-xs text-muted">
                  <div><dt class="sr-only">Durée source</dt><dd>{{ match.source_runtime_minutes }} min</dd></div>
                  <div class="break-all"><dt class="inline"><BrandedText :text="`ID ${providerLabel(match.source_provider)} :`" /></dt> <dd class="inline">{{ match.source_movie_id }}</dd></div>
                </dl>
                <a v-if="match.source_detail_url" :href="match.source_detail_url" target="_blank" rel="noopener noreferrer" class="mt-3 inline-flex items-center gap-1 text-xs font-semibold text-accent underline-offset-2 hover:underline focus-visible:rounded-sm" :aria-label="`Voir sur ${providerLabel(match.source_provider)}, ouverture dans un nouvel onglet`">
                  <BrandedText :text="`Voir sur ${providerLabel(match.source_provider)}`" decorative /> <ExternalLink :size="13" aria-hidden="true" />
                </a>
              </div>
            </div>
          </section>

          <fieldset class="min-w-0" :disabled="Boolean(activeMutation)">
            <legend class="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">Candidats TMDB</legend>
            <div v-if="match.candidates.length" class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-5">
              <div
                v-for="candidate in match.candidates"
                :key="candidate.id"
                class="min-w-0 rounded-md border p-3 transition focus-within:ring-2 focus-within:ring-accent focus-within:ring-offset-2"
                :class="selectedCandidates[match.source_movie_id] === candidate.id ? 'border-accent bg-orange-50' : 'border-line bg-surface hover:border-stone-400'"
              >
                <label :for="`candidate-${match.source_movie_id}-${candidate.id}`" class="flex min-w-0 cursor-pointer items-start gap-2.5">
                  <input
                    :id="`candidate-${match.source_movie_id}-${candidate.id}`"
                    v-model="selectedCandidates[match.source_movie_id]"
                    type="radio"
                    :name="`candidate-${match.source_movie_id}`"
                    :value="candidate.id"
                    class="mt-1 shrink-0 accent-orange-700"
                  />
                  <span class="aspect-[2/3] w-20 shrink-0 overflow-hidden rounded border border-line bg-subtle sm:w-24 lg:w-20">
                    <img
                      v-if="posterAvailable(candidate.poster_url, candidatePosterKey(match, candidate))"
                      :src="candidate.poster_url"
                      :alt="`Affiche TMDB de ${candidate.title}`"
                      class="h-full w-full object-cover"
                      loading="lazy"
                      decoding="async"
                      @error="markPosterUnavailable(candidatePosterKey(match, candidate))"
                    />
                    <span v-else class="flex h-full flex-col items-center justify-center gap-1 px-2 text-center text-muted">
                      <Film :size="24" aria-hidden="true" />
                      <span class="text-[11px] font-medium leading-tight">Affiche indisponible</span>
                    </span>
                  </span>
                  <span class="min-w-0 flex-1">
                    <span class="line-clamp-2 text-sm font-semibold leading-snug text-ink">{{ candidate.title }}</span>
                    <span class="mt-1 line-clamp-2 text-xs leading-snug text-muted">Titre original : {{ candidate.original_title || 'non renseigné' }}</span>
                    <span class="mt-2 block space-y-1 text-xs text-muted">
                      <span class="block">ID TMDB : {{ candidate.id }}</span>
                      <span class="block">{{ candidateRuntime(candidate) }}</span>
                      <span class="block">Score : {{ candidateScore(candidate) }}</span>
                    </span>
                  </span>
                </label>
                <a v-if="candidate.detail_url" :href="candidate.detail_url" target="_blank" rel="noopener noreferrer" class="mt-2 inline-flex items-center gap-1 text-xs font-semibold text-accent underline-offset-2 hover:underline focus-visible:rounded-sm">
                  Voir sur TMDB <ExternalLink :size="13" aria-hidden="true" />
                </a>
              </div>
            </div>
            <p v-else class="rounded-md border border-dashed border-line p-4 text-sm text-muted">Aucun candidat enregistré.</p>
          </fieldset>
        </div>

        <div class="mt-4 grid gap-4 border-t border-line pt-4 lg:grid-cols-[auto_minmax(18rem,1fr)_auto] lg:items-end">
          <button type="button" class="button-primary" :disabled="!selectedCandidates[match.source_movie_id] || Boolean(activeMutation)" @click="approve(match)">
            <LoaderCircle v-if="activeMutation === match.source_movie_id && activeMutationKind === 'candidate'" :size="17" class="animate-spin" aria-hidden="true" />
            <Check v-else :size="17" aria-hidden="true" />
            Valider le candidat
          </button>

          <form class="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-end" @submit.prevent="assignManual(match)">
            <div class="min-w-0 flex-1">
              <label :for="`manual-tmdb-${match.source_movie_id}`" class="mb-1.5 block text-sm font-semibold text-ink">Identifiant TMDB</label>
              <input
                :id="`manual-tmdb-${match.source_movie_id}`"
                v-model="manualTmdbIds[match.source_movie_id]"
                class="field"
                type="number"
                min="1"
                max="9007199254740991"
                step="1"
                inputmode="numeric"
                :disabled="Boolean(activeMutation)"
              />
            </div>
            <button type="submit" class="button-primary shrink-0" :disabled="manualTmdbId(match) === null || Boolean(activeMutation)">
              <LoaderCircle v-if="activeMutation === match.source_movie_id && activeMutationKind === 'manual'" :size="17" class="animate-spin" aria-hidden="true" />
              <Check v-else :size="17" aria-hidden="true" />
              Associer cet identifiant TMDB
            </button>
          </form>

          <div v-if="match.status === 'review_required' && rejectConfirmation === match.source_movie_id" class="flex flex-col gap-2 sm:flex-row sm:items-center lg:justify-end">
            <span class="text-sm font-semibold text-red-800">Rejet définitif ?</span>
            <button type="button" class="h-9 rounded-md bg-red-700 px-3 text-sm font-semibold text-white hover:bg-red-800 disabled:cursor-not-allowed disabled:opacity-50" :disabled="Boolean(activeMutation)" @click="reject(match)">Confirmer le rejet</button>
            <button type="button" class="h-9 rounded-md border border-line px-3 text-sm font-semibold text-ink hover:border-stone-400" :disabled="Boolean(activeMutation)" @click="rejectConfirmation = ''">Annuler</button>
          </div>
          <button v-else-if="match.status === 'review_required'" type="button" class="inline-flex h-9 items-center justify-center gap-2 rounded-md px-3 text-sm font-semibold text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50 lg:justify-self-end" :disabled="Boolean(activeMutation)" @click="rejectConfirmation = match.source_movie_id">
            <Trash2 :size="16" aria-hidden="true" /> Rejeter définitivement
          </button>
        </div>
      </li>
    </ul>

    <nav v-if="!pending && !errorMessage && (offset > 0 || canGoNext)" class="mt-8 flex items-center justify-center gap-4 border-t border-line pt-6" aria-label="Pagination des correspondances">
      <button type="button" class="h-10 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink hover:border-stone-400 disabled:cursor-not-allowed disabled:opacity-50" :disabled="offset === 0 || Boolean(activeMutation)" @click="changePage(offset - PAGE_SIZE)">Précédent</button>
      <span class="text-sm text-muted" aria-live="polite">Page {{ page }}</span>
      <button type="button" class="h-10 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink hover:border-stone-400 disabled:cursor-not-allowed disabled:opacity-50" :disabled="!canGoNext || Boolean(activeMutation)" @click="changePage(offset + PAGE_SIZE)">Suivant</button>
    </nav>
  </main>
</template>
