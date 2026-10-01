<script setup lang="ts">
import {
  AlertTriangle,
  ArrowLeft,
  CalendarClock,
  Check,
  ChevronDown,
  Clock3,
  LoaderCircle,
  RefreshCw,
  X,
} from '@lucide/vue'
import type {
  AdminSyncEnrichmentState,
  AdminSyncFailureCode,
  AdminSyncJob,
  AdminSyncProviderState,
  AdminSyncResponse,
  AdminSyncState,
  AdminSyncTarget,
  AdminSyncTrigger,
  Provider,
} from '~/types/api'
import { hasAdminSyncLog, joinAdminSyncLog } from '~/utils/adminSyncLog'

definePageMeta({ middleware: 'admin-auth' })

const POLL_DELAY = 2000
const api = useMesSeancesApi()
const status = ref<AdminSyncResponse | null>(null)
const initialPending = ref(true)
const statusRequestPending = ref(false)
const startingTarget = ref<AdminSyncTarget | null>(null)
const errorMessage = ref('')
let pollTimer: ReturnType<typeof setTimeout> | undefined
let clockTimer: ReturnType<typeof setInterval> | undefined
let active = false

const job = computed(() => status.value?.job ?? null)
const now = ref(Date.now())
const controlsDisabled = computed(
  () =>
    initialPending.value ||
    startingTarget.value !== null ||
    status.value === null ||
    job.value?.state === 'running',
)
const providers = [
  'ugc',
  'kinepolis',
  'pathe',
  'cgr',
  'megarama',
  'cineville',
  'mk2',
  'cinewest',
  'grandecran',
  'noecinemas',
] as const
const targets = ['all', ...providers] as const
const activeJob = computed(() =>
  job.value?.state === 'running' ? job.value : null,
)
const history = computed(() => {
  const seen = new Set<string>()
  const entries =
    job.value && job.value.state !== 'running'
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
  failed: 'Échec',
} satisfies Record<AdminSyncState, string>

const providerStateLabels = {
  not_requested: 'Non demandée',
  pending: 'En attente',
  running: 'En cours',
  succeeded: 'Terminée avec succès',
  failed: 'Échec',
  skipped: 'Ignorée après un échec',
} satisfies Record<AdminSyncProviderState, string>

const targetLabels = {
  all: 'Tous',
  ugc: 'UGC',
  kinepolis: 'Kinepolis',
  pathe: 'Pathé',
  cgr: 'CGR',
  megarama: 'Megarama',
  cineville: 'Cinéville',
  mk2: 'MK2',
  cinewest: 'Cinewest',
  grandecran: 'Grand Ecran',
  noecinemas: 'Noé Cinémas',
} satisfies Record<AdminSyncTarget, string>

const providerBrands = {
  ugc: 'UGC',
  kinepolis: 'KINEPOLIS',
  pathe: 'PATHE',
  cgr: 'CGR',
  megarama: 'MEGARAMA',
  cineville: 'CINEVILLE',
  mk2: 'MK2',
  cinewest: 'CINEWEST',
  grandecran: 'Grand Ecran',
  noecinemas: 'Noé Cinémas',
} as const satisfies Record<Provider, string>

const providerLabels = {
  ugc: 'UGC',
  kinepolis: 'Kinepolis',
  pathe: 'Pathé',
  cgr: 'CGR',
  megarama: 'Megarama',
  cineville: 'Cinéville',
  mk2: 'MK2',
  cinewest: 'Cinewest',
  grandecran: 'Grand Ecran',
  noecinemas: 'Noé Cinémas',
} satisfies Record<Provider, string>

const triggerLabels = {
  manual: 'Manuelle',
  scheduled: 'Planifiée',
} satisfies Record<AdminSyncTrigger, string>

const failureLabels = {
  none: 'Échec de synchronisation',
  client_creation_failed: 'Connexion au fournisseur impossible',
  provider_sync_failed: 'Récupération des données impossible',
  dataset_rejected: 'Données fournisseur invalides',
  replacement_failed: 'Publication des données impossible',
  canceled: 'Synchronisation interrompue',
  internal_failure: 'Erreur interne',
} satisfies Record<AdminSyncFailureCode, string>

const enrichmentLabels = {
  skipped: 'Non exécuté',
  complete: 'Terminé',
  degraded: 'Partiellement terminé',
} satisfies Record<AdminSyncEnrichmentState, string>

const dateTimeFormatter = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'medium',
  timeStyle: 'short',
  timeZone: 'Europe/Paris',
})

function formatDateTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : dateTimeFormatter.format(date)
}

function durationMilliseconds(run: AdminSyncJob): number {
  const startedAt = new Date(run.started_at).getTime()
  const finishedAt = run.finished_at
    ? new Date(run.finished_at).getTime()
    : now.value
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
  return providers.filter(
    (provider) => run.providers[provider].state !== 'not_requested',
  )
}

function formatNewShowtimes(run: AdminSyncJob): string {
  const count = requestedProviders(run).reduce(
    (total, provider) =>
      total + (run.providers[provider].outcome?.sync.new_showtimes ?? 0),
    0,
  )
  return `+ ${count} séance${count === 1 ? '' : 's'}`
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

function providerIcon(state: AdminSyncProviderState) {
  if (state === 'succeeded') return Check
  if (state === 'failed') return X
  if (state === 'running') return LoaderCircle
  return Clock3
}

function providerIconClass(state: AdminSyncProviderState): string {
  if (state === 'succeeded') return 'text-accent'
  if (state === 'failed') return 'text-primary'
  if (state === 'running') return 'animate-spin text-ink'
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
  <main class="mx-auto max-w-5xl px-4 py-8 sm:px-6 sm:py-10 lg:px-8">
    <div
      class="flex flex-col gap-4 border-b-2 border-ink pb-6 sm:flex-row sm:items-center sm:justify-between"
    >
      <div>
        <NuxtLink
          to="/admin"
          class="mb-2 inline-flex min-h-11 items-center gap-1 font-mono text-xs font-bold text-ink underline underline-offset-4 hover:text-primary"
        >
          <ArrowLeft :size="16" aria-hidden="true" />
          Administration
        </NuxtLink>
        <h1 class="editorial-title">Synchronisation des séances</h1>
      </div>
    </div>

    <div
      v-if="errorMessage"
      class="editorial-alert mt-6 flex items-start gap-3 p-4"
      role="alert"
    >
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <div class="flex-1">
        <p>{{ errorMessage }}</p>
        <button
          type="button"
          class="mt-3 inline-flex items-center gap-2 font-semibold underline underline-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="initialPending || statusRequestPending || startingTarget !== null"
          @click="loadStatus()"
        >
          <RefreshCw :size="16" aria-hidden="true" />
          Réessayer
        </button>
      </div>
    </div>

    <EditorialStatePanel
      v-if="initialPending"
      class="mt-6"
      semantic="status"
      live="polite"
      size="compact"
      shadow="small"
    >
      <LoaderCircle
        :size="28"
        class="animate-spin text-accent"
        aria-hidden="true"
      />
      <p>Chargement de l’état de synchronisation…</p>
    </EditorialStatePanel>

    <template v-else>
      <section
        class="mt-6 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] sm:p-6"
        aria-labelledby="launch-title"
      >
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 id="launch-title" class="editorial-heading">
            Lancer une synchronisation
          </h2>
          <NuxtLink to="/admin/sync-schedules" class="editorial-button-outline">
            <CalendarClock :size="17" aria-hidden="true" />
            Planifier
          </NuxtLink>
        </div>
        <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <button
            v-for="target in targets"
            :key="target"
            type="button"
            class="editorial-button"
            :disabled="controlsDisabled"
            @click="startSync(target)"
          >
            <LoaderCircle
              v-if="startingTarget === target"
              :size="17"
              class="animate-spin"
              aria-hidden="true"
            />
            <span
              v-else-if="target !== 'all'"
              class="flex h-8 w-9 shrink-0 items-center justify-center bg-surface p-1"
              aria-hidden="true"
            >
              <BrandLogo
                :brand="providerBrands[target]"
                decorative
                class="sync-launch-logo h-full! w-full!"
              />
            </span>
            <RefreshCw v-else :size="17" aria-hidden="true" />
            {{ targetLabels[target] }}
          </button>
        </div>
      </section>

      <section
        v-if="activeJob"
        class="mt-6 border-2 border-ink bg-canvas p-5 shadow-[5px_5px_0_#27272a] sm:p-6"
        aria-labelledby="active-title"
      >
        <div class="space-y-5">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 id="active-title" class="editorial-heading">
              Synchronisation en cours
            </h2>
            <span
              class="border-2 border-ink bg-highlight px-3 py-1 font-mono text-xs font-bold text-ink"
              aria-live="polite"
            >
              {{ stateLabels[activeJob.state] }}
            </span>
          </div>

          <dl class="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-5">
            <div>
              <dt class="font-semibold text-muted">Cible</dt>
              <dd class="mt-1 text-ink">
                {{ targetLabels[activeJob.target] }}
              </dd>
            </div>
            <div>
              <dt class="font-semibold text-muted">Déclenchement</dt>
              <dd class="mt-1 text-ink">
                {{ triggerLabels[activeJob.trigger] }}
              </dd>
            </div>
            <div>
              <dt class="font-semibold text-muted">Démarrée</dt>
              <dd class="mt-1 text-ink">
                {{ formatDateTime(activeJob.started_at) }}
              </dd>
            </div>
            <div>
              <dt class="font-semibold text-muted">Durée</dt>
              <dd class="mt-1 tabular-nums text-ink">
                {{ formatDuration(activeJob) }}
              </dd>
            </div>
            <div>
              <dt class="font-semibold text-muted">Période</dt>
              <dd class="mt-1 text-ink">
                Du {{ activeJob.from }} au {{ activeJob.through }}
              </dd>
            </div>
          </dl>

          <ul
            class="divide-y divide-ink/30 border-y-2 border-ink"
            aria-label="État par fournisseur"
          >
            <li
              v-for="provider in requestedProviders(activeJob)"
              :key="provider"
              class="flex items-center justify-between gap-4 py-3"
            >
              <span class="font-semibold text-ink">{{
                providerLabels[provider]
              }}</span>
              <span
                class="flex items-center gap-2 text-sm font-medium text-muted"
              >
                <component
                  :is="providerIcon(activeJob.providers[provider].state)"
                  :size="18"
                  :class="providerIconClass(activeJob.providers[provider].state)"
                  aria-hidden="true"
                />
                {{ providerStateLabels[activeJob.providers[provider].state] }}
              </span>
            </li>
          </ul>
        </div>
      </section>

      <section class="mt-6" aria-labelledby="history-title">
        <div
          class="flex flex-wrap items-center justify-between gap-3 border-b-2 border-ink pb-3"
        >
          <h2 id="history-title" class="editorial-heading">
            Historique des synchronisations
          </h2>
          <span v-if="history.length" class="text-sm text-muted"
            >{{ history.length }}
            exécution{{ history.length > 1 ? 's' : '' }}</span
          >
        </div>

        <div v-if="history.length" class="divide-y divide-ink/30">
          <details v-for="(run, index) in history" :key="run.id" class="group">
            <summary
              class="flex cursor-pointer list-none items-center gap-3 py-4 marker:content-none"
            >
              <component
                :is="run.state === 'succeeded' ? Check : X"
                :size="19"
                class="shrink-0"
                :class="run.state === 'succeeded' ? 'text-accent' : 'text-primary'"
                aria-hidden="true"
              />
              <BrandLogo
                v-if="run.target !== 'all'"
                :brand="providerBrands[run.target]"
                variant="display"
                decorative
                class="sync-history-logo h-12! w-12!"
              />
              <span class="min-w-0 flex-1">
                <span class="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <span class="font-semibold text-ink">{{
                    targetLabels[run.target]
                  }}</span>
                  <span
                    class="text-sm font-medium"
                    :class="run.state === 'succeeded' ? 'text-accent' : 'text-primary'"
                    >{{
                      stateLabels[run.state]
                    }}</span
                  >
                </span>
                <span
                  class="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted"
                >
                  <span>{{ formatDateTime(run.started_at) }}</span>
                  <span class="tabular-nums">{{ formatDuration(run) }}</span>
                  <span>{{ triggerLabels[run.trigger] }}</span>
                  <span>{{ formatNewShowtimes(run) }}</span>
                </span>
              </span>
              <span
                v-if="index === 0"
                class="hidden border-2 border-ink bg-canvas px-2.5 py-1 font-mono text-xs font-bold text-ink sm:inline"
                >Dernière</span
              >
              <ChevronDown
                :size="18"
                class="shrink-0 text-muted transition-transform group-open:rotate-180"
                aria-hidden="true"
              />
            </summary>

            <div class="pb-5 pl-8">
              <div
                v-for="provider in requestedProviders(run)"
                :key="provider"
                class="border-t border-ink/30 py-4 first:border-t-0 first:pt-0"
              >
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <h3 class="font-semibold text-ink">
                    {{ providerLabels[provider] }}
                  </h3>
                  <span
                    class="flex items-center gap-2 text-sm font-medium text-muted"
                  >
                    <component
                      :is="providerIcon(run.providers[provider].state)"
                      :size="17"
                      :class="providerIconClass(run.providers[provider].state)"
                      aria-hidden="true"
                    />
                    {{ providerStateLabels[run.providers[provider].state] }}
                  </span>
                </div>

                <p
                  v-if="run.providers[provider].error_code"
                  class="mt-2 text-sm font-medium text-primary"
                >
                  {{ failureLabels[run.providers[provider].error_code] }}
                </p>

                <div
                  v-if="run.providers[provider].state === 'failed'"
                  class="mt-3 min-w-0 max-w-full"
                >
                  <template v-if="hasAdminSyncLog(run.providers[provider].log)">
                    <h4 class="text-sm font-semibold text-ink">
                      Journal opérationnel
                    </h4>
                    <pre
                      class="mt-2 max-w-full whitespace-pre-wrap break-words font-mono text-xs leading-5 text-ink [overflow-wrap:anywhere]"
                    ><code>{{ joinAdminSyncLog(run.providers[provider].log) }}</code></pre>
                  </template>
                  <p v-else class="text-sm text-muted">
                    Aucun journal détaillé enregistré pour cette exécution.
                  </p>
                </div>

                <template v-if="run.providers[provider].outcome">
                  <dl
                    class="mt-3 grid grid-cols-2 gap-x-5 gap-y-3 text-sm sm:grid-cols-4"
                  >
                    <div>
                      <dt class="text-muted">Cinémas</dt>
                      <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                        {{ run.providers[provider].outcome.sync.cinemas }}
                      </dd>
                    </div>
                    <div>
                      <dt class="text-muted">Films</dt>
                      <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                        {{ run.providers[provider].outcome.sync.movies }}
                      </dd>
                    </div>
                    <div>
                      <dt class="text-muted">Nouveaux films</dt>
                      <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                        {{ run.providers[provider].outcome.sync.new_movies }}
                      </dd>
                    </div>
                    <div>
                      <dt class="text-muted">Séances</dt>
                      <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                        {{ run.providers[provider].outcome.sync.showtimes }}
                      </dd>
                    </div>
                    <div>
                      <dt class="text-muted">Nouvelles séances</dt>
                      <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                        {{ run.providers[provider].outcome.sync.new_showtimes }}
                      </dd>
                    </div>
                    <div
                      v-if="run.providers[provider].outcome.sync.requests !== undefined"
                    >
                      <dt class="text-muted">Requêtes</dt>
                      <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                        {{ run.providers[provider].outcome.sync.requests }}
                      </dd>
                    </div>
                    <div
                      v-if="run.providers[provider].outcome.sync.dates !== undefined"
                    >
                      <dt class="text-muted">Dates</dt>
                      <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                        {{ run.providers[provider].outcome.sync.dates }}
                      </dd>
                    </div>
                    <div
                      v-if="run.providers[provider].outcome.sync.skipped !== undefined"
                    >
                      <dt class="text-muted">Éléments ignorés</dt>
                      <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                        {{ run.providers[provider].outcome.sync.skipped }}
                      </dd>
                    </div>
                    <div>
                      <dt class="text-muted">Version</dt>
                      <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                        {{ run.providers[provider].outcome.sync.version }}
                      </dd>
                    </div>
                  </dl>

                  <div class="mt-4 border-t border-ink/30 pt-3 text-sm">
                    <p>
                      <span class="text-muted">Enrichissement TMDB</span>
                      <span class="ml-1 font-semibold text-ink">{{
                        enrichmentLabels[run.providers[provider].outcome.enrichment.status]
                      }}</span>
                    </p>
                    <dl
                      v-if="run.providers[provider].outcome.enrichment.counts"
                      class="mt-3 grid grid-cols-2 gap-x-5 gap-y-3 sm:grid-cols-5"
                    >
                      <div>
                        <dt class="text-muted">Réutilisés</dt>
                        <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                          {{
                            run.providers[provider].outcome.enrichment.counts.reused
                          }}
                        </dd>
                      </div>
                      <div>
                        <dt class="text-muted">Associés</dt>
                        <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                          {{
                            run.providers[provider].outcome.enrichment.counts.matched
                          }}
                        </dd>
                      </div>
                      <div>
                        <dt class="text-muted">À valider</dt>
                        <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                          {{
                            run.providers[provider].outcome.enrichment.counts.review_required
                          }}
                        </dd>
                      </div>
                      <div>
                        <dt class="text-muted">Sans résultat</dt>
                        <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                          {{
                            run.providers[provider].outcome.enrichment.counts.unmatched
                          }}
                        </dd>
                      </div>
                      <div>
                        <dt class="text-muted">Échecs</dt>
                        <dd class="mt-0.5 font-semibold tabular-nums text-ink">
                          {{
                            run.providers[provider].outcome.enrichment.counts.failed
                          }}
                        </dd>
                      </div>
                    </dl>
                  </div>
                </template>
              </div>
            </div>
          </details>
        </div>

        <EditorialStatePanel v-else class="mt-4" size="compact" shadow="small">
          Aucune synchronisation enregistrée.
        </EditorialStatePanel>
      </section>
    </template>
  </main>
</template>
