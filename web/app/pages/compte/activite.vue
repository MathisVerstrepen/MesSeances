<script setup lang="ts">
import { AlertTriangle, LoaderCircle } from '@lucide/vue'

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
</script>

<template>
  <AccountShell
    title="Activité"
    account-area
    back-to-account
    hide-explore
    hide-logout
  >
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
        <div class="mb-8 max-w-5xl border-b border-ink/20 pb-6">
          <label
            class="flex min-h-11 w-fit cursor-pointer items-center gap-3 font-bold"
          >
            <input
              type="checkbox"
              class="size-5 accent-ink"
              :checked="response.reading.manual_reading"
              :disabled="!!readingPending || morePending || account.writesBlocked.value"
              aria-describedby="activity-reading-help"
              @change="saveReadingMode(($event.target as HTMLInputElement).checked)"
            >
            Marquage manuel des nouveautés
          </label>
          <p
            id="activity-reading-help"
            class="mt-1 max-w-prose text-sm text-muted"
          >
            {{
              response.reading.manual_reading
              ? 'Les nouveautés restent marquées jusqu’à votre action. Cliquez sur « Nouveau » sur une affiche pour marquer l’activité comme consultée, puis sur « Consultée » pour annuler.'
              : 'En mode automatique, les activités sont marquées comme consultées à l’ouverture de cette page.'
            }}
          </p>
          <div
            v-if="response.reading.manual_reading && response.items.length"
            class="mt-3"
          >
            <button
              type="button"
              class="account-secondary"
              :disabled="!!readingPending || morePending || account.writesBlocked.value"
              @click="markAllConsulted"
            >
              {{
                readingPending === 'all' ? 'Enregistrement…' : 'Tout marquer comme consulté'
              }}
            </button>
            <p class="mt-2 text-xs text-muted">
              Inclut les activités plus anciennes, même non affichées. Les
              nouveautés reçues ensuite restent marquées.
            </p>
          </div>
          <p
            v-if="readingPending === 'mode'"
            role="status"
            class="mt-2 text-sm text-muted"
          >
            Enregistrement du mode…
          </p>
          <p v-if="readingError" role="alert" class="account-alert mt-3">
            {{ readingError }}
          </p>
        </div>
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
