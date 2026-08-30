<script setup lang="ts">
import { AlertTriangle, ArrowRight, CalendarClock, Film, LoaderCircle, LogOut, MapPin, RefreshCw } from '@lucide/vue'

definePageMeta({ middleware: 'admin-auth' })

const api = useMesSeancesApi()
const loggingOut = ref(false)
const errorMessage = ref('')

async function logout() {
  if (loggingOut.value) return
  loggingOut.value = true
  errorMessage.value = ''
  try {
    await api.adminLogout()
    await navigateTo('/admin/login')
  } catch (error) {
    errorMessage.value = getFrenchAdminApiError(error)
  } finally {
    loggingOut.value = false
  }
}

useHead({ title: 'Administration - MesSeances' })
</script>

<template>
  <main class="mx-auto max-w-5xl px-4 py-6 sm:px-6 sm:py-8 lg:px-8">
    <div class="flex items-center justify-between gap-4 border-b border-line pb-5">
      <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Administration</h1>
      <button type="button" class="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 text-sm font-semibold text-ink transition hover:border-line-hover disabled:cursor-not-allowed disabled:opacity-50" :disabled="loggingOut" @click="logout">
        <LoaderCircle v-if="loggingOut" :size="17" class="animate-spin" aria-hidden="true" />
        <LogOut v-else :size="17" aria-hidden="true" />
        {{ loggingOut ? 'Déconnexion…' : 'Se déconnecter' }}
      </button>
    </div>

    <div v-if="errorMessage" class="mt-6 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800" role="alert">
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <p>{{ errorMessage }}</p>
    </div>

    <section class="mt-6" aria-labelledby="admin-tools-title">
      <h2 id="admin-tools-title" class="sr-only">Outils d’administration</h2>
      <div class="grid max-w-xl gap-4">
        <NuxtLink to="/admin/tmdb-matches" class="group flex items-center gap-4 rounded-lg border border-line bg-surface p-5 shadow-sm transition hover:border-line-hover">
          <span class="grid size-11 shrink-0 place-items-center rounded-md bg-subtle text-accent">
            <Film :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-base font-semibold text-ink">Correspondances TMDB</span>
          <ArrowRight :size="20" class="shrink-0 text-muted transition group-hover:translate-x-0.5 group-hover:text-accent" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink to="/admin/movies" class="group flex items-center gap-4 rounded-lg border border-line bg-surface p-5 shadow-sm transition hover:border-line-hover">
          <span class="grid size-11 shrink-0 place-items-center rounded-md bg-subtle text-accent">
            <Film :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-base font-semibold text-ink">Métadonnées des films</span>
          <ArrowRight :size="20" class="shrink-0 text-muted transition group-hover:translate-x-0.5 group-hover:text-accent" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink to="/admin/sync" class="group flex items-center gap-4 rounded-lg border border-line bg-surface p-5 shadow-sm transition hover:border-line-hover">
          <span class="grid size-11 shrink-0 place-items-center rounded-md bg-subtle text-accent">
            <RefreshCw :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-base font-semibold text-ink">Synchronisation des séances</span>
          <ArrowRight :size="20" class="shrink-0 text-muted transition group-hover:translate-x-0.5 group-hover:text-accent" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink to="/admin/sync-schedules" class="group flex items-center gap-4 rounded-lg border border-line bg-surface p-5 shadow-sm transition hover:border-line-hover">
          <span class="grid size-11 shrink-0 place-items-center rounded-md bg-subtle text-accent">
            <CalendarClock :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-base font-semibold text-ink">Planification des synchronisations</span>
          <ArrowRight :size="20" class="shrink-0 text-muted transition group-hover:translate-x-0.5 group-hover:text-accent" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink to="/admin/theater-locations" class="group flex items-center gap-4 rounded-lg border border-line bg-surface p-5 shadow-sm transition hover:border-line-hover">
          <span class="grid size-11 shrink-0 place-items-center rounded-md bg-subtle text-accent">
            <MapPin :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-base font-semibold text-ink">Localisations des cinémas</span>
          <ArrowRight :size="20" class="shrink-0 text-muted transition group-hover:translate-x-0.5 group-hover:text-accent" aria-hidden="true" />
        </NuxtLink>
      </div>
    </section>
  </main>
</template>
