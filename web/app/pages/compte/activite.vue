<script setup lang="ts">
import { AlertTriangle, LoaderCircle, Settings, X } from '@lucide/vue'
import { latestActivityEventID } from '~/utils/activityVisit'

definePageMeta({ middleware: 'account-auth' })
useHead({ title: 'Activité - MesSeances' })
const account = useAccountSession()
const {
  response,
  loading,
  morePending,
  error,
  moreError,
  revalidationError,
  newActivityAvailable,
  departingHeight,
  groups,
  refresh,
  loadMore,
  readingPending,
  readingError,
  saveReadingMode,
  toggleConsulted,
  markAllConsulted,
} = useAccountActivity()
const visit = useActivityVisit(
  'followed',
  () => response.value?.items ?? [],
  () =>
    !!response.value &&
    !loading.value &&
    !error.value &&
    !response.value.reading.manual_reading,
)

const settingsOpen = ref(false)
const settingsDialog = useTemplateRef('settingsDialog')
const settingsTrigger = useTemplateRef('settingsTrigger')
const settingsClose = useTemplateRef('settingsClose')
const settingsReady = computed(
  () => !!response.value && !loading.value && !error.value,
)
let bodyOverflow: string | null = null
let active = true

function unlockScroll() {
  if (bodyOverflow === null) return
  document.body.style.overflow = bodyOverflow
  bodyOverflow = null
}
function closeSettings(restoreFocus = true) {
  if (!settingsOpen.value) return
  settingsOpen.value = false
  settingsDialog.value?.close()
  unlockScroll()
  if (restoreFocus && active && !settingsTrigger.value?.disabled)
    settingsTrigger.value?.focus({ preventScroll: true })
}
const settingsLifetime = useAccountLifetime(() => closeSettings(false))
async function openSettings() {
  if (!settingsReady.value || settingsOpen.value) return
  const current = settingsLifetime.capture()
  settingsOpen.value = true
  await nextTick()
  if (!active || !current() || !settingsOpen.value || !settingsDialog.value)
    return
  settingsDialog.value.showModal()
  bodyOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  settingsClose.value?.focus({ preventScroll: true })
}
function settingsBackdrop(event: MouseEvent) {
  const modal = settingsDialog.value
  if (!modal || event.target !== modal) return
  const rect = modal.getBoundingClientRect()
  if (
    event.clientX < rect.left ||
    event.clientX > rect.right ||
    event.clientY < rect.top ||
    event.clientY > rect.bottom
  )
    closeSettings()
}
function keepSettingsFocus(event: KeyboardEvent) {
  if (event.key !== 'Tab' || !settingsDialog.value) return
  const controls = [
    ...settingsDialog.value.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input:not(:disabled)',
    ),
  ]
  const first = controls[0]
  const last = controls.at(-1)
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last?.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first?.focus()
  }
}
async function markAllFromSettings() {
  const current = settingsLifetime.capture()
  const latest = latestActivityEventID(response.value?.items ?? [])
  if ((await markAllConsulted()) && current()) {
    visit.confirmConsulted(latest)
    closeSettings()
  }
}
watch(
  settingsReady,
  (ready) => {
    if (!ready) closeSettings(false)
  },
  { flush: 'sync' },
)
onBeforeUnmount(() => {
  active = false
  closeSettings(false)
  unlockScroll()
})
</script>

<template>
  <AccountShell
    title="Activité"
    account-area
    back-to-account
    hide-explore
    hide-logout
  >
    <template #title-actions>
      <button
        ref="settingsTrigger"
        type="button"
        class="flex size-11 shrink-0 items-center justify-center border-2 border-ink bg-surface hover:bg-highlight disabled:cursor-not-allowed disabled:opacity-50"
        aria-label="Paramètres de l’activité"
        aria-haspopup="dialog"
        aria-controls="activity-settings"
        :aria-expanded="settingsOpen"
        :disabled="!settingsReady"
        @click="openSettings"
      >
        <Settings :size="21" aria-hidden="true" />
      </button>
    </template>
    <dialog
      v-if="settingsOpen && response"
      id="activity-settings"
      ref="settingsDialog"
      class="fixed inset-0 m-auto flex max-h-[calc(100dvh-2rem)] w-[calc(100%-2rem)] max-w-lg flex-col overflow-hidden border-2 border-ink bg-surface p-0 text-ink shadow-[6px_6px_0_#27272a] backdrop:bg-black/60"
      aria-labelledby="activity-settings-title"
      @cancel.prevent="closeSettings()"
      @close="closeSettings()"
      @click="settingsBackdrop"
      @keydown="keepSettingsFocus"
    >
      <header
        class="flex shrink-0 items-center justify-between gap-3 border-b border-ink/20 p-4 sm:px-6"
      >
        <h2 id="activity-settings-title" class="account-heading">
          Paramètres de l’activité
        </h2>
        <button
          ref="settingsClose"
          type="button"
          class="flex size-11 shrink-0 items-center justify-center hover:bg-subtle"
          aria-label="Fermer les paramètres"
          @click="closeSettings()"
        >
          <X :size="20" aria-hidden="true" />
        </button>
      </header>
      <div
        class="min-h-0 space-y-5 overflow-y-auto overscroll-contain p-4 sm:p-6"
        :aria-busy="!!readingPending"
      >
        <label
          class="flex min-h-11 cursor-pointer items-start gap-3 font-semibold"
        >
          <input
            type="checkbox"
            class="mt-0.5 size-5 shrink-0 accent-ink"
            :checked="response.reading.manual_reading"
            :disabled="!!readingPending || morePending || account.writesBlocked.value"
            @change="saveReadingMode(($event.target as HTMLInputElement).checked)"
          >
          Marquer manuellement les nouveautés comme consultées
        </label>
        <div class="border-t border-ink/20 pt-5">
          <button
            type="button"
            class="account-secondary w-full"
            :disabled="!response.items.length || !!readingPending || morePending || account.writesBlocked.value"
            @click="markAllFromSettings"
          >
            {{
              readingPending === 'all' ? 'Enregistrement…' : 'Tout marquer comme consulté'
            }}
          </button>
          <p class="mt-2 text-xs text-muted">
            Y compris les activités plus anciennes, même non affichées.
          </p>
        </div>
        <p v-if="readingPending === 'mode'" role="status">
          Enregistrement du mode…
        </p>
        <p v-if="readingError" role="alert" class="account-alert">
          {{ readingError }}
        </p>
      </div>
    </dialog>
    <div
      id="account-activity"
      :aria-busy="loading || morePending"
      :style="departingHeight ? { minHeight: `${departingHeight}px` } : undefined"
    >
      <div v-if="loading">
        <p role="status" class="sr-only">Chargement de l’activité…</p>
        <ActivityTimelineSkeleton />
      </div>
      <div v-else-if="error" class="space-y-4">
        <p role="alert" class="account-alert">
          <AlertTriangle
            :size="20"
            class="mr-2 inline"
            aria-hidden="true"
          />Impossible de charger l’activité. {{ error }}
        </p>
        <button
          type="button"
          class="account-secondary"
          :disabled="account.writesBlocked.value"
          @click="refresh"
        >
          Réessayer
        </button>
      </div>
      <template v-else-if="response">
        <p
          v-if="readingError && !settingsOpen"
          role="alert"
          class="account-alert mb-6"
        >
          {{ readingError }}
        </p>
        <div
          v-if="newActivityAvailable"
          role="status"
          class="pointer-events-none fixed inset-x-4 bottom-24 z-40 flex justify-center"
        >
          <button
            type="button"
            class="account-primary pointer-events-auto shadow-lg"
            :disabled="account.writesBlocked.value"
            @click="refresh"
          >
            Nouvelle activité disponible
          </button>
        </div>
        <div v-if="response.followed_theater_count === 0" class="space-y-4">
          <h2 class="account-heading">Aucun cinéma suivi</h2>
          <NuxtLink to="/cinemas" class="account-primary"
            >Explorer les cinémas</NuxtLink
          >
        </div>
        <template v-else>
          <div
            v-if="response.coverage.initialized_theater_count === 0"
            class="space-y-3"
          >
            <h2 class="account-heading">
              Historique en cours d’initialisation
            </h2>
            <p class="max-w-prose text-sm">
              L’activité apparaîtra après les prochaines synchronisations des
              cinémas suivis.
            </p>
          </div>
          <h2 v-else-if="response.items.length === 0" class="account-heading">
            Aucune nouvelle programmation détectée.
          </h2>
          <ActivityTimeline
            v-else
            :groups="groups"
            :date-heading-level="2"
            :previous-last-event-id="visit.previousLastEventID.value"
            :manual-reading="response.reading.manual_reading"
            :reading-disabled="!!readingPending || morePending || account.writesBlocked.value"
            :reading-pending="readingPending"
            label="Activité des cinémas suivis"
            @toggle-consulted="toggleConsulted"
          />
          <ActivityVisitStatus
            v-if="response.items.length && !response.reading.manual_reading"
            :failed="visit.error.value"
            @retry="visit.retry"
          />
          <div v-if="revalidationError" class="mt-8 space-y-4">
            <p role="alert" class="account-alert">
              Impossible d’actualiser l’activité. {{ revalidationError }}
            </p>
            <button
              type="button"
              class="account-secondary"
              :disabled="account.writesBlocked.value"
              @click="account.revalidate()"
            >
              Réessayer l’actualisation
            </button>
          </div>
          <div v-if="response.next_cursor" class="mt-8 space-y-4">
            <p v-if="moreError" role="alert" class="account-alert">
              Impossible de charger la suite. {{ moreError }}
            </p>
            <button
              type="button"
              class="account-secondary"
              :disabled="morePending || !!readingPending || account.writesBlocked.value"
              @click="loadMore"
            >
              <LoaderCircle
                v-if="morePending"
                :size="17"
                class="motion-safe:animate-spin"
                aria-hidden="true"
              />
              {{
                morePending ? 'Chargement…' : moreError ? 'Réessayer' : 'Afficher plus'
              }}
            </button>
            <p role="status" aria-live="polite" class="sr-only">
              {{ morePending ? 'Chargement de la suite de l’activité…' : '' }}
            </p>
          </div>
        </template>
      </template>
    </div>
  </AccountShell>
</template>
