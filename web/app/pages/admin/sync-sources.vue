<script setup lang="ts">
import {
  AlertTriangle,
  ArrowLeft,
  ExternalLink,
  LoaderCircle,
  Power,
  PowerOff,
  RefreshCw,
  Search,
} from '@lucide/vue'
import type { AdminSyncSource, Provider } from '~/types/api'
import {
  groupAdminSyncSources,
  type AdminSyncSourceStatusFilter,
} from '~/utils/adminSyncSources'

definePageMeta({ middleware: 'admin-auth' })

const api = useMesSeancesApi()
const numberFormatter = new Intl.NumberFormat('fr-FR')
const sources = ref<AdminSyncSource[]>([])
const loading = ref(true)
const loadError = ref('')
const actionError = ref('')
const successMessage = ref('')
const search = ref('')
const status = ref<AdminSyncSourceStatusFilter>('all')
// Keys of rows being saved, and the scope of a running bulk action.
const pendingRows = ref<Record<string, boolean>>({})
const bulkPending = ref<string | null>(null)

const groups = computed(() =>
  groupAdminSyncSources(sources.value, search.value, status.value),
)
const totalEnabled = computed(
  () => sources.value.filter((source) => source.enabled).length,
)
const statusOptions = [
  { value: 'all', label: 'Tous' },
  { value: 'enabled', label: 'Activés' },
  { value: 'disabled', label: 'Désactivés' },
] as const satisfies { value: AdminSyncSourceStatusFilter; label: string }[]

function rowKey(source: AdminSyncSource): string {
  return `${source.provider}:${source.key}`
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    sources.value = (await api.adminSyncSources()).sources
  } catch (error) {
    loadError.value = getFrenchAdminApiError(error)
  } finally {
    loading.value = false
  }
}

async function toggle(source: AdminSyncSource) {
  const key = rowKey(source)
  if (pendingRows.value[key] || bulkPending.value) return
  pendingRows.value = { ...pendingRows.value, [key]: true }
  actionError.value = ''
  successMessage.value = ''
  try {
    const saved = await api.adminSetSyncSource(
      source.provider,
      source.key,
      !source.enabled,
    )
    sources.value = sources.value.map((item) =>
      rowKey(item) === key ? saved : item,
    )
  } catch (error) {
    actionError.value = getFrenchAdminApiError(error)
  } finally {
    const { [key]: _done, ...rest } = pendingRows.value
    pendingRows.value = rest
  }
}

async function setAll(provider: Provider | null, enabled: boolean) {
  if (bulkPending.value) return
  const scope = provider
    ? `les cinémas ${groups.value.find((g) => g.provider === provider)?.label ?? provider}`
    : 'tous les cinémas sources'
  if (
    !window.confirm(
      `${enabled ? 'Activer' : 'Désactiver'} ${scope} ? Le changement s’applique à la prochaine synchronisation.`,
    )
  )
    return
  bulkPending.value = `${provider ?? 'all'}:${enabled}`
  actionError.value = ''
  successMessage.value = ''
  try {
    const { updated } = await api.adminSetSyncSources(provider, enabled)
    sources.value = sources.value.map((source) =>
      !provider || source.provider === provider
        ? { ...source, enabled }
        : source,
    )
    successMessage.value = `${numberFormatter.format(updated)} cinéma${updated > 1 ? 's' : ''} ${enabled ? 'activé' : 'désactivé'}${updated > 1 ? 's' : ''}.`
  } catch (error) {
    actionError.value = getFrenchAdminApiError(error)
    await load()
  } finally {
    bulkPending.value = null
  }
}

onMounted(load)

useHead({
  title: 'Cinémas sources - Administration - MesSeances',
  meta: [
    { name: 'robots', content: 'noindex, nofollow' },
    { name: 'referrer', content: 'no-referrer' },
  ],
})
</script>

<template>
  <main class="mx-auto max-w-5xl px-4 py-8 sm:px-6 sm:py-10 lg:px-8">
    <header class="border-b-2 border-ink pb-6">
      <NuxtLink
        to="/admin"
        class="mb-4 inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-ink hover:text-accent"
      >
        <ArrowLeft :size="17" aria-hidden="true" />
        Administration
      </NuxtLink>
      <h1 class="editorial-title">Cinémas sources</h1>
      <p class="mt-3 max-w-3xl text-sm text-muted">
        Cinémas indépendants servis par les connecteurs TicketingCiné, SCARE et
        Webedia. Seuls les cinémas activés sont synchronisés ; un changement
        s’applique à la prochaine
        <NuxtLink
          to="/admin/sync"
          class="font-semibold text-ink underline underline-offset-2"
          >synchronisation</NuxtLink
        >.
      </p>
    </header>

    <EditorialStatePanel
      v-if="loading"
      class="mt-6"
      semantic="status"
      live="polite"
      size="compact"
      shadow="small"
    >
      <p class="font-semibold text-ink">Chargement des cinémas sources…</p>
    </EditorialStatePanel>

    <div
      v-else-if="loadError"
      class="editorial-alert mt-6 flex flex-wrap items-start gap-3 p-4"
      role="alert"
    >
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <p class="min-w-0 flex-1">{{ loadError }}</p>
      <button type="button" class="editorial-button-outline" @click="load">
        <RefreshCw :size="16" aria-hidden="true" />
        Réessayer
      </button>
    </div>

    <template v-else>
      <section
        class="mt-6 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a]"
        aria-labelledby="sync-sources-summary"
      >
        <div class="flex flex-wrap items-center justify-between gap-4">
          <h2 id="sync-sources-summary" class="text-lg font-black text-ink">
            {{ numberFormatter.format(totalEnabled) }} /
            {{ numberFormatter.format(sources.length) }} cinémas activés
          </h2>
          <div class="flex flex-wrap gap-3">
            <button
              type="button"
              class="editorial-button"
              :disabled="!!bulkPending || totalEnabled === sources.length"
              @click="setAll(null, true)"
            >
              <LoaderCircle
                v-if="bulkPending === 'all:true'"
                :size="16"
                class="animate-spin"
                aria-hidden="true"
              />
              <Power v-else :size="16" aria-hidden="true" />
              Tout activer
            </button>
            <button
              type="button"
              class="editorial-button-danger"
              :disabled="!!bulkPending || totalEnabled === 0"
              @click="setAll(null, false)"
            >
              <LoaderCircle
                v-if="bulkPending === 'all:false'"
                :size="16"
                class="animate-spin"
                aria-hidden="true"
              />
              <PowerOff v-else :size="16" aria-hidden="true" />
              Tout désactiver
            </button>
          </div>
        </div>
        <div class="mt-5 flex flex-wrap items-end gap-4">
          <label
            class="grid min-w-0 flex-1 gap-1 text-sm font-semibold text-ink"
          >
            Rechercher
            <span class="relative">
              <Search
                :size="16"
                class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted"
                aria-hidden="true"
              />
              <input
                v-model="search"
                type="search"
                class="editorial-field w-full pl-9"
                placeholder="Nom, identifiant ou n° CNC"
              >
            </span>
          </label>
          <fieldset class="flex flex-wrap gap-2">
            <legend class="sr-only">Statut</legend>
            <label
              v-for="option in statusOptions"
              :key="option.value"
              class="inline-flex min-h-11 cursor-pointer items-center border-2 border-ink px-3 text-sm font-bold has-focus-visible:outline-3 has-focus-visible:outline-offset-1 has-focus-visible:outline-ink"
              :class="status === option.value ? 'bg-ink text-white' : 'bg-surface text-ink'"
            >
              <input
                v-model="status"
                type="radio"
                name="sync-source-status"
                :value="option.value"
                class="sr-only"
              >
              {{ option.label }}
            </label>
          </fieldset>
        </div>
      </section>

      <p
        v-if="actionError"
        class="editorial-alert mt-4 flex items-start gap-3 p-4"
        role="alert"
      >
        <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
        {{ actionError }}
      </p>
      <p
        v-else-if="successMessage"
        class="mt-4 border-l-4 border-ink bg-highlight/30 p-4 text-sm text-ink"
        role="status"
      >
        {{ successMessage }}
      </p>

      <section
        v-for="group in groups"
        :key="group.provider"
        class="mt-8"
        :aria-labelledby="`sync-sources-${group.provider}`"
      >
        <div
          class="flex flex-wrap items-center justify-between gap-3 border-b-2 border-ink pb-3"
        >
          <h2
            :id="`sync-sources-${group.provider}`"
            class="text-xl font-black text-ink"
          >
            {{ group.label }}
            <span class="ml-2 text-sm font-semibold text-muted"
              >{{ numberFormatter.format(group.enabled) }}
              /
              {{ numberFormatter.format(group.total) }}
              activés</span
            >
          </h2>
          <div class="flex flex-wrap gap-2">
            <button
              type="button"
              class="editorial-button-outline"
              :disabled="!!bulkPending || group.enabled === group.total"
              @click="setAll(group.provider, true)"
            >
              <LoaderCircle
                v-if="bulkPending === `${group.provider}:true`"
                :size="16"
                class="animate-spin"
                aria-hidden="true"
              />
              Tout activer
            </button>
            <button
              type="button"
              class="editorial-button-outline"
              :disabled="!!bulkPending || group.enabled === 0"
              @click="setAll(group.provider, false)"
            >
              <LoaderCircle
                v-if="bulkPending === `${group.provider}:false`"
                :size="16"
                class="animate-spin"
                aria-hidden="true"
              />
              Tout désactiver
            </button>
          </div>
        </div>
        <p v-if="group.sources.length === 0" class="mt-3 text-sm text-muted">
          Aucun cinéma ne correspond aux filtres.
        </p>
        <ul v-else class="divide-y divide-ink/20">
          <li
            v-for="source in group.sources"
            :key="source.key"
            class="flex flex-wrap items-center gap-x-4 gap-y-2 py-3"
          >
            <button
              type="button"
              role="switch"
              :aria-checked="source.enabled"
              :aria-label="`${source.enabled ? 'Désactiver' : 'Activer'} ${source.name}`"
              :disabled="!!pendingRows[`${source.provider}:${source.key}`] || !!bulkPending"
              class="relative inline-flex h-7 w-12 shrink-0 items-center border-2 border-ink transition-colors focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ink disabled:cursor-wait disabled:opacity-60"
              :class="source.enabled ? 'bg-ink' : 'bg-surface'"
              @click="toggle(source)"
            >
              <span
                class="absolute top-0.5 size-5 border-2 border-ink transition-[left]"
                :class="source.enabled ? 'left-[1.45rem] bg-white' : 'left-0.5 bg-subtle'"
                aria-hidden="true"
              />
            </button>
            <div class="min-w-0 flex-1">
              <p class="font-bold text-ink [overflow-wrap:anywhere]">
                {{ source.name || source.key }}
              </p>
              <p class="text-xs text-muted">
                <span class="font-mono">{{ source.key }}</span>
                <template v-if="source.cnc_id">
                  · CNC {{ source.cnc_id }}</template
                >
              </p>
            </div>
            <a
              v-if="source.website"
              :href="source.website"
              target="_blank"
              rel="noopener noreferrer"
              class="inline-flex min-h-11 items-center gap-1 text-sm font-semibold text-ink underline underline-offset-2 hover:text-accent"
            >
              Site
              <ExternalLink :size="14" aria-hidden="true" />
            </a>
          </li>
        </ul>
      </section>
    </template>
  </main>
</template>
