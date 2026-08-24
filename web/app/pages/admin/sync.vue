<script setup lang="ts">
import { AlertTriangle, ArrowLeft, CalendarClock, Check, ChevronDown, Clock3, LoaderCircle, LogOut, RefreshCw, X } from '@lucide/vue'
import type { AdminSyncEnrichmentState, AdminSyncFailureCode, AdminSyncJob, AdminSyncProviderState, AdminSyncResponse, AdminSyncState, AdminSyncTarget, AdminSyncTrigger, Provider } from '~/types/api'

definePageMeta({ middleware: 'admin-auth' })

const POLL_DELAY = 2000
const api = useMesSeancesApi()
const status = ref<AdminSyncResponse | null>(null)
const initialPending = ref(true)
const statusRequestPending = ref(false)
const startingTarget = ref<AdminSyncTarget | null>(null)
const loggingOut = ref(false)
const errorMessage = ref('')
let pollTimer: ReturnType<typeof setTimeout> | undefined
let clockTimer: ReturnType<typeof setInterval> | undefined
let active = false

const job = computed(() => status.value?.job ?? null)
const now = ref(Date.now())
const controlsDisabled = computed(() => initialPending.value || startingTarget.value !== null || status.value === null || job.value?.state === 'running')
const providers = ['ugc', 'kinepolis'] as const
const targets = ['all', 'ugc', 'kinepolis'] as const
const activeJob = computed(() => job.value?.state === 'running' ? job.value : null)
const history = computed(() => {
  const seen = new Set<string>()
  const entries = job.value && job.value.state !== 'running'
    ? [job.value, ...(status.value?.runs ?? [])]
    : (status.value?.runs ?? [])
  return entries.filter((entry) => {
    if (seen.has(entry.id)) return false
    seen.add(entry.id)
    return true
  })
})

const stateLabels = {
  running: 'En cours',
  succeeded: 'Terminée avec succès',
  failed: 'Échec'
} satisfies Record<AdminSyncState, string>

const providerStateLabels = {
  not_requested: 'Non demandée',
  pending: 'En attente',
  running: 'En cours',
  succeeded: 'Terminée avec succès',
  failed: 'Échec',
  skipped: 'Ignorée après un échec'
} satisfies Record<AdminSyncProviderState, string>

const targetLabels = {
  all: 'Tous les cinémas',
  ugc: 'UGC',
  kinepolis: 'Kinepolis'
} satisfies Record<AdminSyncTarget, string>

const providerLabels = {
  ugc: 'UGC',
  kinepolis: 'Kinepolis'
} satisfies Record<Provider, string>

const triggerLabels = {
  manual: 'Manuelle',
  scheduled: 'Planifiée'
} satisfies Record<AdminSyncTrigger, string>

const failureLabels = {
  none: 'Échec de synchronisation',
  client_creation_failed: 'Connexion au fournisseur impossible',
  provider_sync_failed: 'Récupération des données impossible',
  dataset_rejected: 'Données fournisseur invalides',
  replacement_failed: 'Publication des données impossible',
  canceled: 'Synchronisation interrompue',
  internal_failure: 'Erreur interne'
} satisfies Record<AdminSyncFailureCode, string>

const enrichmentLabels = {
  skipped: 'Non exécuté',
  complete: 'Terminé',
  degraded: 'Partiellement terminé'
} satisfies Record<AdminSyncEnrichmentState, string>

const dateTimeFormatter = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'medium',
  timeStyle: 'short',
  timeZone: 'Europe/Paris'
})

function formatDateTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : dateTimeFormatter.format(date)
}

function durationMilliseconds(run: AdminSyncJob): number {
  const startedAt = new Date(run.started_at).getTime()
  const finishedAt = run.finished_at ? new Date(run.finished_at).getTime() : now.value
  if (!Number.isFinite(startedAt) || !Number.isFinite(finishedAt)) return 0
  return Math.max(0, finishedAt - startedAt)
}

function formatDuration(run: AdminSyncJob): string {
  const totalSeconds = Math.floor(durationMilliseconds(run) / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) return `${hours} h ${minutes} min ${seconds} s`
  if (minutes > 0) return `${minutes} min ${seconds} s`
  return `${seconds} s`
}

function requestedProviders(run: AdminSyncJob): Provider[] {
  return providers.filter((provider) => run.providers[provider].state !== 'not_requested')
}

function clearPolling() {
  if (pollTimer !== undefined) {
    clearTimeout(pollTimer)
    pollTimer = undefined
  }
}

function schedulePolling() {
  clearPolling()
  if (!active || job.value?.state !== 'running') return
  pollTimer = setTimeout(() => {
    pollTimer = undefined
    void loadStatus(true)
  }, POLL_DELAY)
}

async function loadStatus(fromPolling = false) {
  if (statusRequestPending.value) return
  if (!fromPolling) clearPolling()
  if (!fromPolling && status.value === null) initialPending.value = true
  if (!fromPolling) errorMessage.value = ''
  statusRequestPending.value = true
  try {
    const response = await api.adminSyncStatus()
    if (!active) return
    status.value = response
    errorMessage.value = ''
  } catch (error) {
    if (!active) return
    errorMessage.value = getFrenchAdminApiError(error)
  } finally {
    if (active) {
      statusRequestPending.value = false
      initialPending.value = false
      schedulePolling()
    }
  }
}

async function startSync(target: AdminSyncTarget) {
  if (controlsDisabled.value) return
  clearPolling()
  startingTarget.value = target
  errorMessage.value = ''
  try {
    const response = await api.adminStartSync(target)
    if (!active) return
    status.value = response
  } catch (error) {
    if (!active) return
    if (getApiErrorStatus(error) === 409) {
      await loadStatus()
      return
    }
    errorMessage.value = getFrenchAdminApiError(error)
  } finally {
    if (active) {
      startingTarget.value = null
      schedulePolling()
    }
  }
}

async function logout() {
  if (loggingOut.value) return
  clearPolling()
  loggingOut.value = true
  errorMessage.value = ''
  try {
    await api.adminLogout()
    await navigateTo('/admin/login')
  } catch (error) {
    errorMessage.value = getFrenchAdminApiError(error)
    schedulePolling()
  } finally {
    loggingOut.value = false
  }
}

function providerIcon(state: AdminSyncProviderState) {
  if (state === 'succeeded') return Check
  if (state === 'failed') return X
  if (state === 'running') return LoaderCircle
  return Clock3
}

function providerIconClass(state: AdminSyncProviderState): string {
  if (state === 'succeeded') return 'text-green-700'
  if (state === 'failed') return 'text-red-700'
  if (state === 'running') return 'animate-spin text-accent'
  return 'text-muted'
}

onMounted(() => {
  active = true
  clockTimer = setInterval(() => {
    if (activeJob.value) now.value = Date.now()
  }, 1000)
  void loadStatus()
})

onBeforeUnmount(() => {
  active = false
  clearPolling()
  if (clockTimer !== undefined) clearInterval(clockTimer)
})

useHead({ title: 'Synchronisation - MesSeances' })
</script>

<template>
  <main class="mx-auto max-w-5xl px-4 py-6 sm:px-6 sm:py-8 lg:px-8">
    <div class="flex flex-col gap-4 border-b border-line pb-5 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <NuxtLink to="/admin" class="mb-2 inline-flex items-center gap-1 text-sm font-semibold text-muted hover:text-accent">
          <ArrowLeft :size="16" aria-hidden="true" /> Administration
        </NuxtLink>
        <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Synchronisation des séances</h1>
      </div>
      <button type="button" class="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink transition hover:border-line-hover disabled:cursor-not-allowed disabled:opacity-50" :disabled="loggingOut" @click="logout">
        <LoaderCircle v-if="loggingOut" :size="17" class="animate-spin" aria-hidden="true" />
        <LogOut v-else :size="17" aria-hidden="true" />
        {{ loggingOut ? 'Déconnexion…' : 'Se déconnecter' }}
      </button>
    </div>

    <div v-if="errorMessage" class="mt-6 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <div class="flex-1">
        <p>{{ errorMessage }}</p>
        <button type="button" class="mt-3 inline-flex items-center gap-2 font-semibold underline underline-offset-2 disabled:cursor-not-allowed disabled:opacity-50" :disabled="initialPending || statusRequestPending || startingTarget !== null" @click="loadStatus()">
          <RefreshCw :size="16" aria-hidden="true" /> Réessayer
        </button>
      </div>
    </div>

    <div v-if="initialPending" class="state-panel mt-6" role="status" aria-live="polite">
      <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
      <p>Chargement de l’état de synchronisation…</p>
    </div>

    <template v-else>
      <section class="mt-6 rounded-lg border border-line bg-surface p-5 shadow-sm sm:p-6" aria-labelledby="launch-title">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 id="launch-title" class="text-lg font-semibold text-ink">Lancer une synchronisation</h2>
          <NuxtLink to="/admin/sync-schedules" class="inline-flex min-h-11 items-center gap-2 rounded-md border border-line bg-surface px-3 text-sm font-semibold text-ink transition hover:border-line-hover hover:text-accent">
            <CalendarClock :size="17" aria-hidden="true" /> Planifier
          </NuxtLink>
        </div>
        <div class="mt-4 grid gap-3 sm:grid-cols-3">
          <button v-for="target in targets" :key="target" type="button" class="button-primary" :disabled="controlsDisabled" @click="startSync(target)">
            <LoaderCircle v-if="startingTarget === target" :size="17" class="animate-spin" aria-hidden="true" />
            <RefreshCw v-else :size="17" aria-hidden="true" />
            {{ targetLabels[target] }}
          </button>
        </div>
      </section>

      <section v-if="activeJob" class="mt-6 rounded-lg border border-amber-200 bg-amber-50 p-5 shadow-sm sm:p-6" aria-labelledby="active-title">
        <div class="space-y-5">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 id="active-title" class="text-lg font-semibold text-ink">Synchronisation en cours</h2>
            <span class="rounded-full bg-amber-100 px-3 py-1 text-sm font-semibold text-amber-800" aria-live="polite">
              {{ stateLabels[activeJob.state] }}
            </span>
          </div>

          <dl class="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-5">
            <div><dt class="font-semibold text-muted">Cible</dt><dd class="mt-1 text-ink">{{ targetLabels[activeJob.target] }}</dd></div>
            <div><dt class="font-semibold text-muted">Déclenchement</dt><dd class="mt-1 text-ink">{{ triggerLabels[activeJob.trigger] }}</dd></div>
            <div><dt class="font-semibold text-muted">Démarrée</dt><dd class="mt-1 text-ink">{{ formatDateTime(activeJob.started_at) }}</dd></div>
            <div><dt class="font-semibold text-muted">Durée</dt><dd class="mt-1 tabular-nums text-ink">{{ formatDuration(activeJob) }}</dd></div>
            <div><dt class="font-semibold text-muted">Période</dt><dd class="mt-1 text-ink">Du {{ activeJob.from }} au {{ activeJob.through }}</dd></div>
          </dl>

          <ul class="divide-y divide-amber-200 border-y border-amber-200" aria-label="État par fournisseur">
            <li v-for="provider in requestedProviders(activeJob)" :key="provider" class="flex items-center justify-between gap-4 py-3">
              <span class="font-semibold text-ink">{{ providerLabels[provider] }}</span>
              <span class="flex items-center gap-2 text-sm font-medium text-muted">
                <component :is="providerIcon(activeJob.providers[provider].state)" :size="18" :class="providerIconClass(activeJob.providers[provider].state)" aria-hidden="true" />
                {{ providerStateLabels[activeJob.providers[provider].state] }}
              </span>
            </li>
          </ul>
        </div>
      </section>

      <section class="mt-6" aria-labelledby="history-title">
        <div class="flex items-center justify-between gap-3 border-b border-line pb-3">
          <h2 id="history-title" class="text-lg font-semibold text-ink">Historique des synchronisations</h2>
          <span v-if="history.length" class="text-sm text-muted">{{ history.length }} exécution{{ history.length > 1 ? 's' : '' }}</span>
        </div>

        <div v-if="history.length" class="divide-y divide-line">
          <details v-for="(run, index) in history" :key="run.id" class="group">
            <summary class="flex cursor-pointer list-none items-center gap-3 py-4 marker:content-none">
              <component :is="run.state === 'succeeded' ? Check : X" :size="19" class="shrink-0" :class="run.state === 'succeeded' ? 'text-green-700' : 'text-red-700'" aria-hidden="true" />
              <span class="min-w-0 flex-1">
                <span class="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <span class="font-semibold text-ink">{{ targetLabels[run.target] }}</span>
                  <span class="text-sm font-medium" :class="run.state === 'succeeded' ? 'text-green-700' : 'text-red-700'">{{ stateLabels[run.state] }}</span>
                </span>
                <span class="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted">
                  <span>{{ formatDateTime(run.started_at) }}</span>
                  <span class="tabular-nums">{{ formatDuration(run) }}</span>
                  <span>{{ triggerLabels[run.trigger] }}</span>
                  <span>Du {{ run.from }} au {{ run.through }}</span>
                </span>
              </span>
              <span v-if="index === 0" class="hidden rounded-full bg-canvas px-2.5 py-1 text-xs font-semibold text-muted sm:inline">Dernière</span>
              <ChevronDown :size="18" class="shrink-0 text-muted transition-transform group-open:rotate-180" aria-hidden="true" />
            </summary>

            <div class="pb-5 pl-8">
              <div v-for="provider in requestedProviders(run)" :key="provider" class="border-t border-line py-4 first:border-t-0 first:pt-0">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <h3 class="font-semibold text-ink">{{ providerLabels[provider] }}</h3>
                  <span class="flex items-center gap-2 text-sm font-medium text-muted">
                    <component :is="providerIcon(run.providers[provider].state)" :size="17" :class="providerIconClass(run.providers[provider].state)" aria-hidden="true" />
                    {{ providerStateLabels[run.providers[provider].state] }}
                  </span>
                </div>

                <p v-if="run.providers[provider].error_code" class="mt-2 text-sm font-medium text-red-700">
                  {{ failureLabels[run.providers[provider].error_code] }}
                </p>

                <template v-if="run.providers[provider].outcome">
                  <dl class="mt-3 grid grid-cols-2 gap-x-5 gap-y-3 text-sm sm:grid-cols-4">
                    <div><dt class="text-muted">Cinémas</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.sync.cinemas }}</dd></div>
                    <div><dt class="text-muted">Films</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.sync.movies }}</dd></div>
                    <div><dt class="text-muted">Nouveaux films</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.sync.new_movies }}</dd></div>
                    <div><dt class="text-muted">Séances</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.sync.showtimes }}</dd></div>
                    <div><dt class="text-muted">Nouvelles séances</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.sync.new_showtimes }}</dd></div>
                    <div v-if="run.providers[provider].outcome.sync.requests !== undefined"><dt class="text-muted">Requêtes</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.sync.requests }}</dd></div>
                    <div v-if="run.providers[provider].outcome.sync.dates !== undefined"><dt class="text-muted">Dates</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.sync.dates }}</dd></div>
                    <div v-if="run.providers[provider].outcome.sync.skipped !== undefined"><dt class="text-muted">Éléments ignorés</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.sync.skipped }}</dd></div>
                    <div><dt class="text-muted">Version</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.sync.version }}</dd></div>
                  </dl>

                  <div class="mt-4 border-t border-line pt-3 text-sm">
                    <p><span class="text-muted">Enrichissement TMDB</span> <span class="ml-1 font-semibold text-ink">{{ enrichmentLabels[run.providers[provider].outcome.enrichment.status] }}</span></p>
                    <dl v-if="run.providers[provider].outcome.enrichment.counts" class="mt-3 grid grid-cols-2 gap-x-5 gap-y-3 sm:grid-cols-5">
                      <div><dt class="text-muted">Réutilisés</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.enrichment.counts.reused }}</dd></div>
                      <div><dt class="text-muted">Associés</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.enrichment.counts.matched }}</dd></div>
                      <div><dt class="text-muted">À valider</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.enrichment.counts.review_required }}</dd></div>
                      <div><dt class="text-muted">Sans résultat</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.enrichment.counts.unmatched }}</dd></div>
                      <div><dt class="text-muted">Échecs</dt><dd class="mt-0.5 font-semibold tabular-nums text-ink">{{ run.providers[provider].outcome.enrichment.counts.failed }}</dd></div>
                    </dl>
                  </div>
                </template>
              </div>
            </div>
          </details>
        </div>

        <p v-else class="py-6 text-sm text-muted">Aucune synchronisation enregistrée.</p>
      </section>
    </template>
  </main>
</template>
