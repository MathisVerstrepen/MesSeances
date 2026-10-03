<script setup lang="ts">
import { AlertTriangle, LoaderCircle } from '@lucide/vue'
import {
  activityFullDate,
  activityHistoryDate,
  activityShortDate,
  activityShowtimesTarget,
  activityTypeLabel,
} from '~/utils/cinemaActivity'
import { cinemaMovieTarget } from '~/utils/cinemaMovieTarget'

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
      <div v-if="loading" class="space-y-6 motion-safe:animate-pulse">
        <p role="status" class="sr-only">Chargement de l’activité…</p>
        <div
          v-for="row in 3"
          :key="row"
          aria-hidden="true"
          class="flex gap-4 border-b border-ink/20 pb-6"
        >
          <div class="h-18 w-12 shrink-0 bg-ink/10" />
          <div class="min-w-0 flex-1 space-y-3">
            <div class="h-5 w-3/4 bg-ink/10" />
            <div class="h-4 w-1/2 bg-ink/10" />
          </div>
        </div>
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
          <ol v-else aria-label="Activité des cinémas suivis">
            <li
              v-for="group in groups"
              :key="group.day"
              class="border-b border-ink/15 py-6 first:pt-0 lg:grid lg:grid-cols-[160px_minmax(0,1fr)] lg:gap-6"
            >
              <h2 class="mb-4 text-base font-bold lg:mb-0">
                <time :datetime="group.day"
                  ><span class="lg:hidden">{{
                    activityFullDate(group.day)
                  }}</span><span class="hidden lg:inline">{{
                    activityHistoryDate(group.day)
                  }}</span></time
                >
              </h2>
              <ul class="space-y-6">
                <li
                  v-for="item in group.items"
                  :key="item.event_id"
                  class="flex items-start gap-4"
                  :data-event-id="item.event_id"
                >
                  <NuxtLink
                    :to="cinemaMovieTarget(item.movie.slug, item.theater.id)"
                    :aria-label="item.movie.title"
                    class="block w-12 shrink-0 sm:w-16"
                  >
                    <PosterImage
                      :src="item.movie.poster_url"
                      alt=""
                      sizes="(min-width: 640px) 64px, 48px"
                      :reset-key="item.event_id"
                      fallback-variant="icon-only"
                      :fallback-icon-size="24"
                      class="aspect-[2/3] bg-subtle"
                      image-class="size-full object-cover"
                      fallback-class="text-muted"
                    />
                  </NuxtLink>
                  <div class="min-w-0 flex-1">
                    <NuxtLink
                      :to="`/cinema/${item.theater.slug}?view=activity`"
                      :aria-label="item.theater.name"
                      class="flex w-fit max-w-full items-start text-sm font-semibold leading-5 underline underline-offset-4 hover:text-primary"
                    >
                      <TheaterName
                        :name="item.theater.name"
                        :provider="item.theater.provider"
                        decorative
                        class="min-w-0 break-words [overflow-wrap:anywhere]"
                      />
                    </NuxtLink>
                    <h3 class="editorial-heading my-1 break-words">
                      <NuxtLink
                        :to="cinemaMovieTarget(item.movie.slug, item.theater.id)"
                        class="block w-fit max-w-full hover:underline underline-offset-4"
                        >{{
                          item.movie.title
                        }}</NuxtLink
                      >
                    </h3>
                    <p class="leading-5">
                      <span
                        class="inline-flex border px-2 py-px align-top text-xs font-medium leading-4"
                        :class="item.type === 'return_to_program' ? 'border-accent/40 bg-accent-soft text-accent' : 'border-ink/30 text-ink'"
                        >{{
                          activityTypeLabel(item.type)
                        }}</span
                      >
                    </p>
                    <p class="mt-1 text-sm leading-5 text-muted">
                      Première séance annoncée ·
                      <time :datetime="item.first_screening_date">{{
                        activityShortDate(item.first_screening_date)
                      }}</time>
                    </p>
                    <p
                      v-if="item.type === 'return_to_program' && item.previous_program_end_date"
                      class="text-sm leading-5 text-muted"
                    >
                      Programmation précédente · jusqu’au
                      <time :datetime="item.previous_program_end_date">{{
                        activityShortDate(item.previous_program_end_date)
                      }}</time>
                    </p>
                    <NuxtLink
                      v-if="activityShowtimesTarget(item, item.theater.id)"
                      :to="activityShowtimesTarget(item, item.theater.id)!"
                      class="inline-flex min-h-11 items-center text-sm font-bold underline underline-offset-4 hover:text-primary"
                      >Voir les séances</NuxtLink
                    >
                  </div>
                </li>
              </ul>
            </li>
          </ol>
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
