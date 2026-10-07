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
  groups,
  refresh,
  loadMore,
} = useAccountActivity()
</script>

<template>
  <AccountShell
    title="Activité"
    account-area
    back-to-account
    hide-explore
    hide-logout
  >
    <div :aria-busy="loading || morePending">
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
            label="Activité des cinémas suivis"
          />
          <div v-if="response.next_cursor" class="mt-8 space-y-4">
            <p v-if="moreError" role="alert" class="account-alert">
              Impossible de charger la suite. {{ moreError }}
            </p>
            <button
              type="button"
              class="account-secondary"
              :disabled="morePending || account.writesBlocked.value"
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
