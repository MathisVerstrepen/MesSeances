<script setup lang="ts">
definePageMeta({ middleware: 'account-auth' })
useHead({ title: 'Films vus - MesSeances' })
const watched = useWatched()
const account = useAccountSession()
</script>

<template>
  <AccountShell
    title="Films vus"
    account-area
    back-to-account
    hide-explore
    hide-logout
  >
    <div
      v-if="watched.error.value"
      role="alert"
      class="account-alert mb-6 space-y-3"
    >
      <p>{{ watched.error.value }}</p>
      <button
        class="account-secondary"
        type="button"
        :disabled="account.writesBlocked.value || watched.saving.value"
        @click="watched.retry"
      >
        Réessayer
      </button>
    </div>
    <div v-if="!watched.snapshot.value" :aria-busy="watched.loading.value">
      <p role="status">
        {{
          watched.error.value ? 'Les films vus ne sont pas disponibles.' : 'Chargement des films vus…'
        }}
      </p>
      <div
        v-if="!watched.error.value"
        class="mt-5 h-48 bg-subtle motion-safe:animate-pulse"
        aria-hidden="true"
      />
    </div>
    <template v-else>
      <WatchedPreferences />
      <section
        aria-labelledby="watched-films-heading"
        class="mt-10 border-t-2 border-ink pt-6"
      >
        <h2 id="watched-films-heading" class="account-heading">
          Mes films vus ({{ watched.items.value.length }})
        </h2>
        <ul v-if="watched.items.value.length" aria-label="Films vus">
          <WatchlistMovieRow
            v-for="movie in watched.items.value"
            :key="movie.slug"
            :slug="movie.slug"
            :title="movie.title"
            :poster-url="movie.poster_url"
            :release-date="movie.release_date"
          >
            <button
              type="button"
              class="account-secondary shrink-0"
              :disabled="watched.writesBlocked.value"
              :aria-label="`Marquer ${movie.title} comme non vu`"
              @click="watched.save(movie.slug, false)"
            >
              Non vu
            </button>
          </WatchlistMovieRow>
        </ul>
        <div v-else class="mt-4 space-y-4">
          <p>Aucun film marqué comme vu.</p>
          <NuxtLink to="/films" class="account-secondary"
            >Explorer les films</NuxtLink
          >
        </div>
      </section>
    </template>
  </AccountShell>
</template>
