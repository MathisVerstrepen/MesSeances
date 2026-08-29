<script setup lang="ts">
import { AlertTriangle, ArrowLeft, CalendarClock, CheckCircle2, Clock3, LoaderCircle, LogOut, Plus, RefreshCw, Save, Trash2 } from '@lucide/vue'
import type {
  AdminSyncJob,
  AdminSyncProviderState,
  AdminSyncScheduleItem,
  AdminSyncScheduleKind,
  AdminSyncScheduleTarget,
  AdminSyncSchedulesResponse,
  AdminSyncTrigger,
  AdminSyncWeekday,
  AdminSyncResponse,
  Provider
} from '~/types/api'
import {
  adminSyncRunDurationMilliseconds,
  adminSyncScheduleDraftFingerprint,
  adminSyncScheduleDraftFromItem,
  blankAdminSyncScheduleDraft,
  buildAdminSyncScheduleRequest,
  isAdminSyncScheduleTargetAvailable,
  selectLatestProviderRun,
  validateAdminSyncScheduleDraft,
  type AdminSyncScheduleDraft
} from '~/utils/adminSyncSchedules'

definePageMeta({ middleware: 'admin-auth' })

type EntryOperation = 'save' | 'delete' | null

interface ScheduleEntryState {
  clientKey: string
  target: AdminSyncScheduleTarget
  persisted: AdminSyncScheduleItem | null
  draft: AdminSyncScheduleDraft
  baseline: string
  dirty: boolean
  pending: EntryOperation
  showValidation: boolean
  error: string
  success: string
}

interface TargetSectionState {
  target: AdminSyncScheduleTarget
  entries: ScheduleEntryState[]
}

const providers = ['ugc', 'kinepolis', 'pathe', 'cgr'] as const
const targets = [...providers, 'tmdb_metadata_refresh'] as const
const api = useMesSeancesApi()
const schedulesPending = ref(true)
const schedulesLoaded = ref(false)
const schedulesError = ref('')
const availableTargets = ref<AdminSyncScheduleTarget[]>([])
const syncStatus = ref<AdminSyncResponse | null>(null)
const syncStatusPending = ref(true)
const syncStatusLoaded = ref(false)
const syncStatusError = ref('')
const loggingOut = ref(false)
const logoutError = ref('')
const sections = reactive<TargetSectionState[]>(targets.map(target => ({ target, entries: [] })))
let active = false
let nextClientKey = 0

const targetLabels = {
  ugc: 'UGC',
  kinepolis: 'Kinepolis',
  pathe: 'Pathé',
  cgr: 'CGR',
  tmdb_metadata_refresh: 'Actualiser toutes les métadonnées TMDB'
} satisfies Record<AdminSyncScheduleTarget, string>

const modeLabels = {
  daily: 'Quotidien',
  weekly: 'Hebdomadaire',
  cron: 'Cron avancé'
} satisfies Record<AdminSyncScheduleKind, string>

const weekdayOptions: ReadonlyArray<{ value: AdminSyncWeekday, short: string, label: string }> = [
  { value: 'mon', short: 'Lun', label: 'Lundi' },
  { value: 'tue', short: 'Mar', label: 'Mardi' },
  { value: 'wed', short: 'Mer', label: 'Mercredi' },
  { value: 'thu', short: 'Jeu', label: 'Jeudi' },
  { value: 'fri', short: 'Ven', label: 'Vendredi' },
  { value: 'sat', short: 'Sam', label: 'Samedi' },
  { value: 'sun', short: 'Dim', label: 'Dimanche' }
]

const providerStateLabels = {
  not_requested: 'Non demandée',
  pending: 'En attente',
  running: 'En cours',
  succeeded: 'Terminée avec succès',
  failed: 'Échec',
  skipped: 'Ignorée après un échec'
} satisfies Record<AdminSyncProviderState, string>

const triggerLabels = {
  manual: 'Manuelle',
  scheduled: 'Planifiée'
} satisfies Record<AdminSyncTrigger, string>

const dateTimeFormatter = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'full',
  timeStyle: 'short',
  timeZone: 'Europe/Paris'
})

const latestRuns = computed<Record<Provider, AdminSyncJob | null>>(() => ({
  ugc: selectLatestProviderRun('ugc', syncStatus.value?.job ?? null, syncStatus.value?.runs ?? []),
  kinepolis: selectLatestProviderRun('kinepolis', syncStatus.value?.job ?? null, syncStatus.value?.runs ?? []),
  pathe: selectLatestProviderRun('pathe', syncStatus.value?.job ?? null, syncStatus.value?.runs ?? []),
  cgr: selectLatestProviderRun('cgr', syncStatus.value?.job ?? null, syncStatus.value?.runs ?? [])
}))

function newClientKey(): string {
  nextClientKey += 1
  return `sync-schedule-${nextClientKey}`
}

function createEntry(target: AdminSyncScheduleTarget, item: AdminSyncScheduleItem | null = null): ScheduleEntryState {
  const draft = item ? adminSyncScheduleDraftFromItem(item) : blankAdminSyncScheduleDraft()
  return {
    clientKey: newClientKey(),
    target,
    persisted: item,
    draft,
    baseline: item ? adminSyncScheduleDraftFingerprint(draft) : '',
    dirty: item === null,
    pending: null,
    showValidation: false,
    error: '',
    success: ''
  }
}

function applyPersistedItem(entry: ScheduleEntryState, item: AdminSyncScheduleItem) {
  const draft = adminSyncScheduleDraftFromItem(item)
  entry.persisted = item
  entry.draft = draft
  entry.baseline = adminSyncScheduleDraftFingerprint(draft)
  entry.dirty = false
  entry.showValidation = false
  entry.error = ''
}

function targetAvailable(target: AdminSyncScheduleTarget): boolean {
  return isAdminSyncScheduleTargetAvailable(target, availableTargets.value)
}

function updateDirty(entry: ScheduleEntryState) {
  entry.dirty = entry.persisted === null || adminSyncScheduleDraftFingerprint(entry.draft) !== entry.baseline
  entry.success = ''
  entry.error = ''
}

function setMode(entry: ScheduleEntryState, kind: AdminSyncScheduleKind) {
  entry.draft.kind = kind
  entry.showValidation = false
  updateDirty(entry)
}

function revealValidation(entry: ScheduleEntryState) {
  entry.showValidation = true
}

function validation(entry: ScheduleEntryState) {
  return validateAdminSyncScheduleDraft(entry.draft)
}

function addSchedule(section: TargetSectionState) {
  const entry = createEntry(section.target)
  section.entries.push(entry)
  void nextTick(() => document.getElementById(`${entry.clientKey}-title`)?.focus())
}

function removeEntry(section: TargetSectionState, entry: ScheduleEntryState) {
  const index = section.entries.indexOf(entry)
  if (index !== -1) section.entries.splice(index, 1)
}

async function loadSchedules() {
  if (schedulesPending.value && schedulesLoaded.value) return
  schedulesPending.value = true
  schedulesError.value = ''
  try {
    const response: AdminSyncSchedulesResponse = await api.adminSyncSchedules()
    if (!active) return
    availableTargets.value = [...response.available_targets]
    for (const section of sections) {
      section.entries = response.schedules
        .filter(item => item.target === section.target)
        .map(item => createEntry(section.target, item))
    }
    schedulesLoaded.value = true
  } catch {
    if (!active) return
    schedulesError.value = 'Les planifications n’ont pas pu être chargées. Le service est temporairement indisponible. Réessayez.'
  } finally {
    if (active) schedulesPending.value = false
  }
}

async function loadSyncStatus() {
  if (syncStatusPending.value && syncStatusLoaded.value) return
  syncStatusPending.value = true
  syncStatusError.value = ''
  try {
    const response = await api.adminSyncStatus()
    if (!active) return
    syncStatus.value = response
    syncStatusLoaded.value = true
  } catch (error) {
    if (!active) return
    syncStatusError.value = getFrenchAdminApiError(error)
  } finally {
    if (active) syncStatusPending.value = false
  }
}

async function saveSchedule(entry: ScheduleEntryState) {
  if (entry.pending !== null || !entry.dirty) return
  entry.showValidation = true
  entry.error = ''
  entry.success = ''
  if (!validation(entry).valid) return
  if (entry.draft.enabled && !targetAvailable(entry.target)) {
    entry.error = 'Cette synchronisation est temporairement indisponible. Désactivez la planification pour l’enregistrer.'
    return
  }

  entry.pending = 'save'
  try {
    const request = buildAdminSyncScheduleRequest(entry.draft)
    const saved = entry.persisted === null
      ? await api.adminCreateSyncSchedule(entry.target, request)
      : await api.adminUpdateSyncSchedule(entry.target, entry.persisted.id, request)
    if (!active) return
    applyPersistedItem(entry, saved)
    entry.success = `Planification ${saved.enabled ? 'activée' : 'enregistrée et désactivée'}.`
  } catch (error) {
    if (!active) return
    entry.error = getFrenchAdminApiError(error)
  } finally {
    if (active) entry.pending = null
  }
}

async function deleteSchedule(section: TargetSectionState, entry: ScheduleEntryState) {
  if (entry.pending !== null) return
  entry.error = ''
  entry.success = ''
  if (entry.persisted === null) {
    removeEntry(section, entry)
    await nextTick()
    document.getElementById(`${section.target}-add`)?.focus()
    return
  }

  entry.pending = 'delete'
  try {
    await api.adminDeleteSyncSchedule(entry.target, entry.persisted.id)
    if (!active) return
    removeEntry(section, entry)
    await nextTick()
    document.getElementById(`${section.target}-add`)?.focus()
  } catch (error) {
    if (!active) return
    entry.error = getFrenchAdminApiError(error)
  } finally {
    if (active) entry.pending = null
  }
}

async function logout() {
  if (loggingOut.value) return
  loggingOut.value = true
  logoutError.value = ''
  try {
    await api.adminLogout()
    await navigateTo('/admin/login')
  } catch (error) {
    logoutError.value = getFrenchAdminApiError(error)
  } finally {
    loggingOut.value = false
  }
}

function entryLabel(entry: ScheduleEntryState): string {
  if (!entry.persisted) return 'Nouvelle'
  return entry.persisted.enabled ? 'Activée' : 'Désactivée'
}

function entryLabelClass(entry: ScheduleEntryState): string {
  if (!entry.persisted) return 'bg-accent-soft text-accent'
  return entry.persisted.enabled ? 'bg-green-100 text-green-800' : 'bg-amber-100 text-amber-800'
}

function formatDateTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : dateTimeFormatter.format(date)
}

function formatDuration(milliseconds: number): string {
  const totalSeconds = Math.floor(milliseconds / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) return `${hours} h ${minutes} min ${seconds} s`
  if (minutes > 0) return `${minutes} min ${seconds} s`
  return `${seconds} s`
}

function latestRun(provider: Provider): AdminSyncJob | null {
  return latestRuns.value[provider]
}

function latestProviderState(provider: Provider): AdminSyncProviderState | null {
  return latestRun(provider)?.providers[provider].state ?? null
}

function latestDuration(provider: Provider): string {
  const run = latestRun(provider)
  return run ? formatDuration(adminSyncRunDurationMilliseconds(run, Date.now())) : ''
}

function latestTrigger(provider: Provider): string {
  const trigger = latestRun(provider)?.trigger
  return trigger ? triggerLabels[trigger] : ''
}

function outcomeClass(state: AdminSyncProviderState | null): string {
  if (state === 'succeeded') return 'text-green-700'
  if (state === 'failed') return 'text-red-700'
  return 'text-muted'
}

function isProvider(target: AdminSyncScheduleTarget): target is Provider {
  return target !== 'tmdb_metadata_refresh'
}

onMounted(() => {
  active = true
  void loadSchedules()
  void loadSyncStatus()
})

onBeforeUnmount(() => {
  active = false
})

useHead({ title: 'Planification des synchronisations - MesSeances' })
</script>

<template>
  <main class="mx-auto max-w-5xl px-4 py-6 sm:px-6 sm:py-8 lg:px-8">
    <div class="flex flex-col gap-4 border-b border-line pb-5 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <NuxtLink to="/admin" class="mb-2 inline-flex min-h-11 items-center gap-1 text-sm font-semibold text-muted hover:text-accent">
          <ArrowLeft :size="16" aria-hidden="true" /> Administration
        </NuxtLink>
        <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Planification des synchronisations</h1>
      </div>
      <button type="button" class="inline-flex h-11 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink transition hover:border-line-hover disabled:cursor-not-allowed disabled:opacity-50" :disabled="loggingOut" @click="logout">
        <LoaderCircle v-if="loggingOut" :size="17" class="animate-spin" aria-hidden="true" />
        <LogOut v-else :size="17" aria-hidden="true" />
        {{ loggingOut ? 'Déconnexion…' : 'Se déconnecter' }}
      </button>
    </div>

    <div v-if="logoutError" class="mt-6 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <p>{{ logoutError }}</p>
    </div>

    <div class="mt-6 flex items-center gap-3 rounded-lg border border-accent-line bg-accent-soft p-4 text-sm text-ink">
      <CalendarClock :size="20" class="shrink-0 text-accent" aria-hidden="true" />
      <p><span class="font-semibold">Fuseau horaire :</span> Europe/Paris</p>
    </div>

    <div v-if="schedulesPending && !schedulesLoaded" class="mt-6 flex min-h-48 items-center justify-center gap-3 rounded-lg border border-dashed border-line bg-canvas p-6 text-sm text-muted" role="status" aria-live="polite">
      <LoaderCircle :size="22" class="animate-spin text-accent" aria-hidden="true" />
      Chargement des planifications…
    </div>

    <div v-else-if="schedulesError && !schedulesLoaded" class="mt-6 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
      <div class="flex items-start gap-3">
        <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
        <div>
          <p>{{ schedulesError }}</p>
          <button type="button" class="mt-3 inline-flex min-h-11 items-center gap-2 font-semibold underline underline-offset-2 disabled:cursor-not-allowed disabled:opacity-50" :disabled="schedulesPending" @click="loadSchedules">
            <RefreshCw :size="16" aria-hidden="true" /> Réessayer
          </button>
        </div>
      </div>
    </div>

    <div v-else-if="schedulesLoaded" class="mt-6 grid gap-6">
      <section v-for="section in sections" :key="section.target" class="rounded-lg border border-line bg-surface p-5 shadow-sm sm:p-6" :aria-labelledby="`${section.target}-title`">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line pb-4">
          <h2 :id="`${section.target}-title`" class="text-xl font-semibold text-ink">{{ targetLabels[section.target] }}</h2>
          <button :id="`${section.target}-add`" type="button" class="inline-flex min-h-11 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink transition hover:border-line-hover" @click="addSchedule(section)">
            <Plus :size="17" aria-hidden="true" /> Ajouter une planification
          </button>
        </div>

        <div v-if="!targetAvailable(section.target)" class="mt-4 flex items-start gap-3 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900" role="status">
          <AlertTriangle :size="19" class="shrink-0" aria-hidden="true" />
          <p>Cette synchronisation est temporairement indisponible. Les planifications désactivées restent modifiables.</p>
        </div>

        <div v-if="section.entries.length === 0" class="mt-5 flex min-h-24 items-center justify-center rounded-md border border-dashed border-line bg-canvas p-4 text-center text-sm text-muted">
          Aucune planification.
        </div>

        <div v-else>
          <article v-for="(entry, index) in section.entries" :key="entry.clientKey" class="border-b border-line py-6 last:border-b-0" :aria-labelledby="`${entry.clientKey}-title`">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <h3 :id="`${entry.clientKey}-title`" tabindex="-1" class="font-semibold text-ink focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2">
                Planification {{ index + 1 }}
              </h3>
              <span class="rounded-full px-3 py-1 text-sm font-semibold" :class="entryLabelClass(entry)">{{ entryLabel(entry) }}</span>
            </div>

            <div class="mt-5 grid gap-6 lg:grid-cols-[minmax(0,1.45fr)_minmax(16rem,0.8fr)]">
              <form :aria-label="`Planification ${index + 1} - ${targetLabels[section.target]}`" @submit.prevent="saveSchedule(entry)">
                <fieldset :disabled="entry.pending !== null">
                  <label class="flex min-h-11 cursor-pointer items-center gap-3 rounded-md border border-line px-3 py-2 text-sm font-semibold text-ink hover:border-line-hover has-disabled:cursor-not-allowed has-disabled:opacity-60">
                    <input v-model="entry.draft.enabled" type="checkbox" class="size-5 shrink-0 accent-accent" :disabled="!targetAvailable(entry.target) && !entry.draft.enabled" @change="updateDirty(entry)">
                    Activer la planification
                  </label>

                  <fieldset class="mt-5">
                    <legend class="text-sm font-semibold text-ink">Fréquence</legend>
                    <div class="mt-2 grid gap-2 sm:grid-cols-3">
                      <label v-for="(label, kind) in modeLabels" :key="kind" class="cursor-pointer">
                        <input class="peer sr-only" type="radio" :name="`${entry.clientKey}-mode`" :value="kind" :checked="entry.draft.kind === kind" @change="setMode(entry, kind)">
                        <span class="flex min-h-11 items-center justify-center rounded-md border border-line px-3 text-center text-sm font-semibold text-muted transition hover:border-line-hover peer-checked:border-accent peer-checked:bg-accent-soft peer-checked:text-accent peer-focus-visible:ring-2 peer-focus-visible:ring-accent peer-focus-visible:ring-offset-2">
                          {{ label }}
                        </span>
                      </label>
                    </div>
                  </fieldset>

                  <div v-if="entry.draft.kind === 'daily' || entry.draft.kind === 'weekly'" class="mt-5">
                    <label :for="`${entry.clientKey}-time`" class="text-sm font-semibold text-ink">Heure</label>
                    <input :id="`${entry.clientKey}-time`" v-model="entry.draft.time" type="time" class="field mt-2 min-h-11" :aria-invalid="entry.showValidation && Boolean(validation(entry).errors.time)" :aria-describedby="entry.showValidation && validation(entry).errors.time ? `${entry.clientKey}-time-error` : undefined" @input="updateDirty(entry)" @blur="revealValidation(entry)">
                    <p v-if="entry.showValidation && validation(entry).errors.time" :id="`${entry.clientKey}-time-error`" class="mt-2 text-sm font-medium text-red-700">
                      {{ validation(entry).errors.time }}
                    </p>
                  </div>

                  <fieldset v-if="entry.draft.kind === 'weekly'" class="mt-5" :aria-describedby="entry.showValidation && validation(entry).errors.weekdays ? `${entry.clientKey}-weekdays-error` : undefined">
                    <legend class="text-sm font-semibold text-ink">Jours</legend>
                    <div class="mt-2 grid grid-cols-4 gap-2 sm:grid-cols-7">
                      <label v-for="weekday in weekdayOptions" :key="weekday.value" class="cursor-pointer">
                        <input v-model="entry.draft.weekdays" class="peer sr-only" type="checkbox" :value="weekday.value" :aria-label="weekday.label" @change="updateDirty(entry)" @blur="revealValidation(entry)">
                        <span class="flex min-h-11 items-center justify-center rounded-md border border-line px-2 text-sm font-semibold text-muted transition hover:border-line-hover peer-checked:border-accent peer-checked:bg-accent-soft peer-checked:text-accent peer-focus-visible:ring-2 peer-focus-visible:ring-accent peer-focus-visible:ring-offset-2">
                          {{ weekday.short }}
                        </span>
                      </label>
                    </div>
                    <p v-if="entry.showValidation && validation(entry).errors.weekdays" :id="`${entry.clientKey}-weekdays-error`" class="mt-2 text-sm font-medium text-red-700">
                      {{ validation(entry).errors.weekdays }}
                    </p>
                  </fieldset>

                  <div v-if="entry.draft.kind === 'cron'" class="mt-5">
                    <label :for="`${entry.clientKey}-cron`" class="text-sm font-semibold text-ink">Expression cron</label>
                    <input :id="`${entry.clientKey}-cron`" v-model="entry.draft.expression" type="text" class="field mt-2 min-h-11 font-mono" autocomplete="off" spellcheck="false" :aria-invalid="entry.showValidation && Boolean(validation(entry).errors.expression)" :aria-describedby="`${entry.clientKey}-cron-help${entry.showValidation && validation(entry).errors.expression ? ` ${entry.clientKey}-cron-error` : ''}`" @input="updateDirty(entry)" @blur="revealValidation(entry)">
                    <p :id="`${entry.clientKey}-cron-help`" class="mt-2 text-sm text-muted">Cinq champs : minute, heure, jour du mois, mois, jour de la semaine.</p>
                    <p v-if="entry.showValidation && validation(entry).errors.expression" :id="`${entry.clientKey}-cron-error`" class="mt-2 text-sm font-medium text-red-700">
                      {{ validation(entry).errors.expression }}
                    </p>
                  </div>
                </fieldset>

                <div class="mt-6 flex flex-wrap items-center gap-3 border-t border-line pt-5">
                  <button type="submit" class="button-primary min-h-11" :disabled="entry.pending !== null || !entry.dirty">
                    <LoaderCircle v-if="entry.pending === 'save'" :size="17" class="animate-spin" aria-hidden="true" />
                    <Save v-else :size="17" aria-hidden="true" />
                    {{ entry.pending === 'save' ? 'Enregistrement…' : 'Enregistrer' }}
                  </button>
                  <button type="button" class="inline-flex min-h-11 items-center justify-center gap-2 rounded-md border border-red-200 px-4 text-sm font-semibold text-red-700 transition hover:border-red-300 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50" :disabled="entry.pending !== null" :aria-label="`Supprimer la planification ${index + 1} - ${targetLabels[section.target]}`" @click="deleteSchedule(section, entry)">
                    <LoaderCircle v-if="entry.pending === 'delete'" :size="17" class="animate-spin" aria-hidden="true" />
                    <Trash2 v-else :size="17" aria-hidden="true" />
                    {{ entry.pending === 'delete' ? 'Suppression…' : 'Supprimer' }}
                  </button>
                  <span v-if="!entry.dirty && entry.pending === null" class="text-sm text-muted">Aucune modification</span>
                </div>

                <div v-if="entry.error" class="mt-4 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
                  <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
                  <p>{{ entry.error }}</p>
                </div>
                <div v-if="entry.success" class="mt-4 flex items-start gap-3 rounded-md border border-green-200 bg-green-50 p-4 text-sm text-green-800" role="status" aria-live="polite">
                  <CheckCircle2 :size="20" class="shrink-0" aria-hidden="true" />
                  <p>{{ entry.success }}</p>
                </div>
              </form>

              <section :aria-labelledby="`${entry.clientKey}-preview-title`">
                <h4 :id="`${entry.clientKey}-preview-title`" class="font-semibold text-ink">
                  {{ entry.persisted?.enabled ? 'Prochaines exécutions' : 'Prévisualisation' }}
                </h4>
                <p v-if="entry.dirty" class="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
                  Enregistrez les modifications pour recalculer les horaires.
                </p>
                <ol v-else-if="entry.persisted && entry.persisted.next_runs.length" class="mt-3 space-y-2 text-sm text-ink">
                  <li v-for="(nextRun, runIndex) in entry.persisted.next_runs" :key="nextRun" class="flex gap-3">
                    <span class="font-semibold tabular-nums text-muted">{{ runIndex + 1 }}.</span>
                    <time :datetime="nextRun">{{ formatDateTime(nextRun) }}</time>
                  </li>
                </ol>
                <p v-else class="mt-3 text-sm text-muted">
                  {{ entry.persisted ? 'Aucun horaire disponible.' : 'Enregistrez cette planification pour afficher les cinq prochaines occurrences.' }}
                </p>
              </section>
            </div>
          </article>
        </div>

        <section v-if="isProvider(section.target)" class="mt-1 border-t border-line pt-5" :aria-labelledby="`${section.target}-latest-title`">
          <div class="flex items-center justify-between gap-3">
            <h3 :id="`${section.target}-latest-title`" class="font-semibold text-ink">Dernière exécution</h3>
            <button v-if="syncStatusError" type="button" class="inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-accent hover:text-accent-hover disabled:cursor-not-allowed disabled:opacity-50" :disabled="syncStatusPending" @click="loadSyncStatus">
              <RefreshCw :size="16" aria-hidden="true" /> Réessayer
            </button>
          </div>

          <div v-if="syncStatusPending && !syncStatusLoaded" class="mt-3 flex min-h-24 items-center justify-center gap-3 rounded-md bg-canvas p-4 text-sm text-muted" role="status" aria-live="polite">
            <LoaderCircle :size="20" class="animate-spin text-accent" aria-hidden="true" /> Chargement…
          </div>
          <div v-else-if="syncStatusError && !syncStatusLoaded" class="mt-3 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800" role="alert">
            <AlertTriangle :size="18" class="shrink-0" aria-hidden="true" />
            <p>{{ syncStatusError }}</p>
          </div>
          <dl v-else-if="latestRun(section.target)" class="mt-3 grid gap-3 text-sm sm:grid-cols-4">
            <div>
              <dt class="font-semibold text-muted">Résultat</dt>
              <dd class="mt-1 font-semibold" :class="outcomeClass(latestProviderState(section.target))">
                {{ providerStateLabels[latestProviderState(section.target) ?? 'not_requested'] }}
              </dd>
            </div>
            <div>
              <dt class="font-semibold text-muted">Démarrée</dt>
              <dd class="mt-1 text-ink">{{ formatDateTime(latestRun(section.target)?.started_at ?? '') }}</dd>
            </div>
            <div>
              <dt class="font-semibold text-muted">Durée</dt>
              <dd class="mt-1 tabular-nums text-ink">{{ latestDuration(section.target) }}</dd>
            </div>
            <div>
              <dt class="font-semibold text-muted">Déclenchement</dt>
              <dd class="mt-1 text-ink">{{ latestTrigger(section.target) }}</dd>
            </div>
          </dl>
          <div v-else-if="syncStatusLoaded" class="mt-3 flex min-h-24 flex-col items-center justify-center gap-2 rounded-md border border-dashed border-line bg-canvas p-4 text-center text-sm text-muted">
            <Clock3 :size="20" aria-hidden="true" />
            Aucune exécution enregistrée.
          </div>
        </section>
      </section>
    </div>
  </main>
</template>
