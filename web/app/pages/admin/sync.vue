<script setup lang="ts">
import { AlertTriangle, ArrowLeft, Check, Clock3, LoaderCircle, LogOut, RefreshCw, X } from '@lucide/vue'
import type { AdminSyncProviderState, AdminSyncResponse, AdminSyncState, AdminSyncTarget, Provider } from '~/types/api'

definePageMeta({ middleware: 'admin-auth' })

const POLL_DELAY = 2000
const api = useMovieFlowApi()
const status = ref<AdminSyncResponse | null>(null)
const initialPending = ref(true)
const statusRequestPending = ref(false)
const startingTarget = ref<AdminSyncTarget | null>(null)
const loggingOut = ref(false)
const errorMessage = ref('')
let pollTimer: ReturnType<typeof setTimeout> | undefined
let active = false

const job = computed(() => status.value?.job ?? null)
const controlsDisabled = computed(() => initialPending.value || startingTarget.value !== null || status.value === null || job.value?.state === 'running')

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

function providerState(provider: Provider): AdminSyncProviderState {
  return job.value?.providers[provider].state ?? 'not_requested'
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
  void loadStatus()
})

onBeforeUnmount(() => {
  active = false
  clearPolling()
})

useHead({ title: 'Synchronisation — MovieFlow' })
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
      <button type="button" class="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink transition hover:border-stone-400 disabled:cursor-not-allowed disabled:opacity-50" :disabled="loggingOut" @click="logout">
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
        <h2 id="launch-title" class="text-lg font-semibold text-ink">Lancer une synchronisation</h2>
        <div class="mt-4 grid gap-3 sm:grid-cols-3">
          <button v-for="target in (['all', 'ugc', 'kinepolis'] as const)" :key="target" type="button" class="button-primary" :disabled="controlsDisabled" @click="startSync(target)">
            <LoaderCircle v-if="startingTarget === target" :size="17" class="animate-spin" aria-hidden="true" />
            <RefreshCw v-else :size="17" aria-hidden="true" />
            {{ targetLabels[target] }}
          </button>
        </div>
      </section>

      <section class="mt-6 rounded-lg border border-line bg-surface p-5 shadow-sm sm:p-6" aria-labelledby="status-title" role="status" aria-live="polite">
        <div v-if="job" class="space-y-5">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 id="status-title" class="text-lg font-semibold text-ink">{{ job.state === 'running' ? 'Synchronisation en cours' : 'Dernière synchronisation' }}</h2>
            <span class="rounded-full px-3 py-1 text-sm font-semibold" :class="job.state === 'succeeded' ? 'bg-green-100 text-green-800' : job.state === 'failed' ? 'bg-red-100 text-red-800' : 'bg-orange-100 text-orange-800'">
              {{ stateLabels[job.state] }}
            </span>
          </div>

          <dl class="grid gap-3 text-sm sm:grid-cols-2">
            <div><dt class="font-semibold text-muted">Cible</dt><dd class="mt-1 text-ink">{{ targetLabels[job.target] }}</dd></div>
            <div><dt class="font-semibold text-muted">Période</dt><dd class="mt-1 text-ink">Du {{ job.from }} au {{ job.through }}</dd></div>
          </dl>

          <ul class="divide-y divide-line rounded-md border border-line" aria-label="État par cinéma">
            <li v-for="provider in (['ugc', 'kinepolis'] as const)" :key="provider" class="flex items-center justify-between gap-4 p-4">
              <span class="font-semibold text-ink">{{ provider === 'ugc' ? 'UGC' : 'Kinepolis' }}</span>
              <span class="flex items-center gap-2 text-sm font-medium text-muted">
                <component :is="providerIcon(providerState(provider))" :size="18" :class="providerIconClass(providerState(provider))" aria-hidden="true" />
                {{ providerStateLabels[providerState(provider)] }}
              </span>
            </li>
          </ul>
        </div>

        <div v-else>
          <h2 id="status-title" class="text-lg font-semibold text-ink">État de synchronisation</h2>
          <p class="mt-4 text-sm text-muted">Aucune synchronisation lancée depuis le démarrage du service.</p>
        </div>
      </section>
    </template>
  </main>
</template>
