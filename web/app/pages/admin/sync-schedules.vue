<script setup lang="ts">
import { AlertTriangle, ArrowLeft, CalendarClock, CheckCircle2, Clock3, LoaderCircle, LogOut, RefreshCw, Save } from '@lucide/vue'
import type {
  AdminSyncJob,
  AdminSyncProviderState,
  AdminSyncScheduleItem,
  AdminSyncScheduleKind,
  AdminSyncSchedulesResponse,
  AdminSyncTrigger,
  AdminSyncWeekday,
  AdminSyncResponse,
  Provider
} from '~/types/api'
import {
  adminSyncRunDurationMilliseconds,
  buildAdminSyncScheduleRequest,
  selectLatestProviderRun,
  validateAdminSyncScheduleDraft,
  type AdminSyncScheduleDraft
} from '~/utils/adminSyncSchedules'

definePageMeta({ middleware: 'admin-auth' })

interface ProviderFormState {
  provider: Provider
  persisted: AdminSyncScheduleItem | null
  draft: AdminSyncScheduleDraft
  baseline: string
  dirty: boolean
  pending: boolean
  showValidation: boolean
  error: string
  success: string
}

const providers = ['ugc', 'kinepolis', 'pathe'] as const
const api = useMesSeancesApi()
const schedulesPending = ref(true)
const schedulesLoaded = ref(false)
const schedulesError = ref('')
const syncStatus = ref<AdminSyncResponse | null>(null)
const syncStatusPending = ref(true)
const syncStatusLoaded = ref(false)
const syncStatusError = ref('')
const loggingOut = ref(false)
const logoutError = ref('')
let active = false

const providerLabels = {
  ugc: 'UGC',
  kinepolis: 'Kinepolis',
  pathe: 'Pathé'
} satisfies Record<Provider, string>

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

const forms = reactive<ProviderFormState[]>(providers.map((provider) => {
  const draft = blankDraft()
  return {
    provider,
    persisted: null,
    draft,
    baseline: draftFingerprint(draft),
    dirty: false,
    pending: false,
    showValidation: false,
    error: '',
    success: ''
  }
}))

const latestRuns = computed<Record<Provider, AdminSyncJob | null>>(() => ({
  ugc: selectLatestProviderRun('ugc', syncStatus.value?.job ?? null, syncStatus.value?.runs ?? []),
  kinepolis: selectLatestProviderRun('kinepolis', syncStatus.value?.job ?? null, syncStatus.value?.runs ?? []),
  pathe: selectLatestProviderRun('pathe', syncStatus.value?.job ?? null, syncStatus.value?.runs ?? [])
}))

function blankDraft(): AdminSyncScheduleDraft {
  return { enabled: false, kind: 'daily', time: '', weekdays: [], expression: '' }
}

function draftFromItem(item: AdminSyncScheduleItem): AdminSyncScheduleDraft {
  if (item.schedule.kind === 'daily') {
    return { enabled: item.enabled, kind: 'daily', time: item.schedule.time, weekdays: [], expression: '' }
  }
  if (item.schedule.kind === 'weekly') {
    return { enabled: item.enabled, kind: 'weekly', time: item.schedule.time, weekdays: [...item.schedule.weekdays], expression: '' }
  }
  return { enabled: item.enabled, kind: 'cron', time: '', weekdays: [], expression: item.schedule.expression }
}

function draftFingerprint(draft: AdminSyncScheduleDraft): string {
  return JSON.stringify(buildAdminSyncScheduleRequest(draft))
}

function applyPersistedItem(form: ProviderFormState, item: AdminSyncScheduleItem | null) {
  const draft = item ? draftFromItem(item) : blankDraft()
  form.persisted = item
  form.draft = draft
  form.baseline = draftFingerprint(draft)
  form.dirty = false
  form.showValidation = false
  form.error = ''
}

function updateDirty(form: ProviderFormState) {
  form.dirty = draftFingerprint(form.draft) !== form.baseline
  form.success = ''
  form.error = ''
}

function setMode(form: ProviderFormState, kind: AdminSyncScheduleKind) {
  form.draft.kind = kind
  form.showValidation = false
  updateDirty(form)
}

function revealValidation(form: ProviderFormState) {
  form.showValidation = true
}

function validation(form: ProviderFormState) {
  return validateAdminSyncScheduleDraft(form.draft)
}

async function loadSchedules() {
  if (schedulesPending.value && schedulesLoaded.value) return
  schedulesPending.value = true
  schedulesError.value = ''
  try {
    const response: AdminSyncSchedulesResponse = await api.adminSyncSchedules()
    if (!active) return
    for (const form of forms) {
      applyPersistedItem(form, response.schedules.find((item) => item.provider === form.provider) ?? null)
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

async function saveSchedule(form: ProviderFormState) {
  if (form.pending || !form.dirty) return
  form.showValidation = true
  form.error = ''
  form.success = ''
  if (!validation(form).valid) return

  form.pending = true
  try {
    const saved = await api.adminSaveSyncSchedule(form.provider, buildAdminSyncScheduleRequest(form.draft))
    if (!active) return
    applyPersistedItem(form, saved)
    form.success = `Planification ${saved.enabled ? 'activée' : 'enregistrée et désactivée'}.`
  } catch (error) {
    if (!active) return
    form.error = getFrenchAdminApiError(error)
  } finally {
    if (active) form.pending = false
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

function configurationLabel(form: ProviderFormState): string {
  if (!form.persisted) return 'Non configuré'
  return form.persisted.enabled ? 'Activée' : 'Désactivée'
}

function configurationLabelClass(form: ProviderFormState): string {
  if (!form.persisted) return 'bg-subtle text-muted'
  return form.persisted.enabled ? 'bg-green-100 text-green-800' : 'bg-amber-100 text-amber-800'
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

    <div class="mt-6 grid gap-6">
      <section v-for="form in forms" :key="form.provider" class="rounded-lg border border-line bg-surface p-5 shadow-sm sm:p-6" :aria-labelledby="`${form.provider}-title`">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line pb-4">
          <h2 :id="`${form.provider}-title`" class="text-xl font-semibold text-ink">{{ providerLabels[form.provider] }}</h2>
          <span v-if="schedulesLoaded" class="rounded-full px-3 py-1 text-sm font-semibold" :class="configurationLabelClass(form)">
            {{ configurationLabel(form) }}
          </span>
        </div>

        <div class="mt-5 grid gap-6 lg:grid-cols-[minmax(0,1.45fr)_minmax(16rem,0.8fr)]">
          <div>
            <div v-if="schedulesPending && !schedulesLoaded" class="flex min-h-48 items-center justify-center gap-3 rounded-lg border border-dashed border-line bg-canvas p-6 text-sm text-muted" role="status" aria-live="polite">
              <LoaderCircle :size="22" class="animate-spin text-accent" aria-hidden="true" />
              Chargement de la configuration…
            </div>

            <div v-else-if="schedulesError && !schedulesLoaded" class="rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
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

            <form v-else-if="schedulesLoaded" :aria-label="`Planification ${providerLabels[form.provider]}`" @submit.prevent="saveSchedule(form)">
              <label class="flex min-h-11 cursor-pointer items-center gap-3 rounded-md border border-line px-3 py-2 text-sm font-semibold text-ink hover:border-line-hover">
                <input v-model="form.draft.enabled" type="checkbox" class="size-5 shrink-0 accent-accent" @change="updateDirty(form)">
                Activer la planification
              </label>

              <fieldset class="mt-5">
                <legend class="text-sm font-semibold text-ink">Fréquence</legend>
                <div class="mt-2 grid gap-2 sm:grid-cols-3">
                  <label v-for="(label, kind) in modeLabels" :key="kind" class="cursor-pointer">
                    <input class="peer sr-only" type="radio" :name="`${form.provider}-mode`" :value="kind" :checked="form.draft.kind === kind" @change="setMode(form, kind)">
                    <span class="flex min-h-11 items-center justify-center rounded-md border border-line px-3 text-center text-sm font-semibold text-muted transition hover:border-line-hover peer-checked:border-accent peer-checked:bg-accent-soft peer-checked:text-accent peer-focus-visible:ring-2 peer-focus-visible:ring-accent peer-focus-visible:ring-offset-2">
                      {{ label }}
                    </span>
                  </label>
                </div>
              </fieldset>

              <div v-if="form.draft.kind === 'daily' || form.draft.kind === 'weekly'" class="mt-5">
                <label :for="`${form.provider}-time`" class="text-sm font-semibold text-ink">Heure</label>
                <input :id="`${form.provider}-time`" v-model="form.draft.time" type="time" class="field mt-2 min-h-11" :aria-invalid="form.showValidation && Boolean(validation(form).errors.time)" :aria-describedby="form.showValidation && validation(form).errors.time ? `${form.provider}-time-error` : undefined" @input="updateDirty(form)" @blur="revealValidation(form)">
                <p v-if="form.showValidation && validation(form).errors.time" :id="`${form.provider}-time-error`" class="mt-2 text-sm font-medium text-red-700">
                  {{ validation(form).errors.time }}
                </p>
              </div>

              <fieldset v-if="form.draft.kind === 'weekly'" class="mt-5">
                <legend class="text-sm font-semibold text-ink">Jours</legend>
                <div class="mt-2 grid grid-cols-4 gap-2 sm:grid-cols-7">
                  <label v-for="weekday in weekdayOptions" :key="weekday.value" class="cursor-pointer">
                    <input v-model="form.draft.weekdays" class="peer sr-only" type="checkbox" :value="weekday.value" :aria-label="weekday.label" @change="updateDirty(form)" @blur="revealValidation(form)">
                    <span class="flex min-h-11 items-center justify-center rounded-md border border-line px-2 text-sm font-semibold text-muted transition hover:border-line-hover peer-checked:border-accent peer-checked:bg-accent-soft peer-checked:text-accent peer-focus-visible:ring-2 peer-focus-visible:ring-accent peer-focus-visible:ring-offset-2">
                      {{ weekday.short }}
                    </span>
                  </label>
                </div>
                <p v-if="form.showValidation && validation(form).errors.weekdays" class="mt-2 text-sm font-medium text-red-700">
                  {{ validation(form).errors.weekdays }}
                </p>
              </fieldset>

              <div v-if="form.draft.kind === 'cron'" class="mt-5">
                <label :for="`${form.provider}-cron`" class="text-sm font-semibold text-ink">Expression cron</label>
                <input :id="`${form.provider}-cron`" v-model="form.draft.expression" type="text" class="field mt-2 min-h-11 font-mono" autocomplete="off" spellcheck="false" :aria-invalid="form.showValidation && Boolean(validation(form).errors.expression)" :aria-describedby="`${form.provider}-cron-help${form.showValidation && validation(form).errors.expression ? ` ${form.provider}-cron-error` : ''}`" @input="updateDirty(form)" @blur="revealValidation(form)">
                <p :id="`${form.provider}-cron-help`" class="mt-2 text-sm text-muted">Cinq champs : minute, heure, jour du mois, mois, jour de la semaine.</p>
                <p v-if="form.showValidation && validation(form).errors.expression" :id="`${form.provider}-cron-error`" class="mt-2 text-sm font-medium text-red-700">
                  {{ validation(form).errors.expression }}
                </p>
              </div>

              <div class="mt-6 flex flex-wrap items-center gap-3 border-t border-line pt-5">
                <button type="submit" class="button-primary min-h-11" :disabled="form.pending || !form.dirty">
                  <LoaderCircle v-if="form.pending" :size="17" class="animate-spin" aria-hidden="true" />
                  <Save v-else :size="17" aria-hidden="true" />
                  {{ form.pending ? 'Enregistrement…' : 'Enregistrer' }}
                </button>
                <span v-if="!form.dirty && !form.pending" class="text-sm text-muted">Aucune modification</span>
              </div>

              <div v-if="form.error" class="mt-4 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
                <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
                <p>{{ form.error }}</p>
              </div>
              <div v-if="form.success" class="mt-4 flex items-start gap-3 rounded-md border border-green-200 bg-green-50 p-4 text-sm text-green-800" role="status" aria-live="polite">
                <CheckCircle2 :size="20" class="shrink-0" aria-hidden="true" />
                <p>{{ form.success }}</p>
              </div>
            </form>
          </div>

          <div class="space-y-6">
            <section :aria-labelledby="`${form.provider}-preview-title`">
              <h3 :id="`${form.provider}-preview-title`" class="font-semibold text-ink">
                {{ form.persisted?.enabled ? 'Prochaines exécutions' : 'Prévisualisation' }}
              </h3>
              <p v-if="form.dirty" class="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
                Enregistrez les modifications pour recalculer les horaires.
              </p>
              <ol v-else-if="form.persisted && form.persisted.next_runs.length" class="mt-3 space-y-2 text-sm text-ink">
                <li v-for="(nextRun, index) in form.persisted.next_runs" :key="nextRun" class="flex gap-3">
                  <span class="font-semibold tabular-nums text-muted">{{ index + 1 }}.</span>
                  <time :datetime="nextRun">{{ formatDateTime(nextRun) }}</time>
                </li>
              </ol>
              <p v-else-if="schedulesLoaded" class="mt-3 text-sm text-muted">
                {{ form.persisted ? 'Aucun horaire disponible.' : 'Enregistrez une configuration pour afficher les cinq prochaines occurrences.' }}
              </p>
              <div v-else class="mt-3 h-20 animate-pulse rounded-md bg-subtle" aria-hidden="true" />
            </section>

            <section class="border-t border-line pt-5" :aria-labelledby="`${form.provider}-latest-title`">
              <div class="flex items-center justify-between gap-3">
                <h3 :id="`${form.provider}-latest-title`" class="font-semibold text-ink">Dernière exécution</h3>
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
              <dl v-else-if="latestRun(form.provider)" class="mt-3 grid gap-3 text-sm">
                <div>
                  <dt class="font-semibold text-muted">Résultat</dt>
                  <dd class="mt-1 font-semibold" :class="outcomeClass(latestProviderState(form.provider))">
                    {{ providerStateLabels[latestProviderState(form.provider) ?? 'not_requested'] }}
                  </dd>
                </div>
                <div>
                  <dt class="font-semibold text-muted">Démarrée</dt>
                  <dd class="mt-1 text-ink">{{ formatDateTime(latestRun(form.provider)?.started_at ?? '') }}</dd>
                </div>
                <div>
                  <dt class="font-semibold text-muted">Durée</dt>
                  <dd class="mt-1 tabular-nums text-ink">{{ latestDuration(form.provider) }}</dd>
                </div>
                <div>
                  <dt class="font-semibold text-muted">Déclenchement</dt>
                  <dd class="mt-1 text-ink">{{ latestTrigger(form.provider) }}</dd>
                </div>
              </dl>
              <div v-else-if="syncStatusLoaded" class="mt-3 flex min-h-24 flex-col items-center justify-center gap-2 rounded-md border border-dashed border-line bg-canvas p-4 text-center text-sm text-muted">
                <Clock3 :size="20" aria-hidden="true" />
                Aucune exécution enregistrée.
              </div>
            </section>
          </div>
        </div>
      </section>
    </div>
  </main>
</template>
