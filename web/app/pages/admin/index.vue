<script setup lang="ts">
import {
  AlertTriangle,
  ArrowRight,
  CalendarClock,
  CalendarDays,
  FilePenLine,
  GitCompareArrows,
  Image,
  LoaderCircle,
  LogOut,
  MapPin,
  RefreshCw,
  Users,
} from '@lucide/vue'

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
  <main class="mx-auto max-w-5xl px-4 py-8 sm:px-6 sm:py-10 lg:px-8">
    <div
      class="flex flex-wrap items-center justify-between gap-4 border-b-2 border-ink pb-6"
    >
      <h1 class="editorial-title">Administration</h1>
      <button
        type="button"
        class="editorial-button-outline"
        :disabled="loggingOut"
        @click="logout"
      >
        <LoaderCircle
          v-if="loggingOut"
          :size="17"
          class="animate-spin"
          aria-hidden="true"
        />
        <LogOut v-else :size="17" aria-hidden="true" />
        {{ loggingOut ? 'Déconnexion…' : 'Se déconnecter' }}
      </button>
    </div>

    <div
      v-if="errorMessage"
      class="editorial-alert mt-6 flex items-start gap-3 p-4"
      role="alert"
    >
      <AlertTriangle :size="20" class="shrink-0" aria-hidden="true" />
      <p>{{ errorMessage }}</p>
    </div>

    <section class="mt-6" aria-labelledby="admin-tools-title">
      <h2 id="admin-tools-title" class="sr-only">Outils d’administration</h2>
      <div class="grid gap-5 sm:grid-cols-2">
        <NuxtLink
          to="/admin/upcoming-movies"
          class="group flex items-center gap-4 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] transition-colors hover:bg-highlight"
        >
          <span
            class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-canvas text-ink"
          >
            <CalendarDays :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-lg font-black leading-tight text-ink"
            >Revue des sorties à venir</span
          >
          <ArrowRight :size="20" class="shrink-0 text-ink" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink
          to="/admin/tmdb-matches"
          class="group flex items-center gap-4 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] transition-colors hover:bg-highlight"
        >
          <span
            class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-canvas text-ink"
          >
            <GitCompareArrows :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-lg font-black leading-tight text-ink"
            >Correspondances TMDB</span
          >
          <ArrowRight :size="20" class="shrink-0 text-ink" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink
          to="/admin/movies"
          class="group flex items-center gap-4 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] transition-colors hover:bg-highlight"
        >
          <span
            class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-canvas text-ink"
          >
            <FilePenLine :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-lg font-black leading-tight text-ink"
            >Métadonnées des films</span
          >
          <ArrowRight :size="20" class="shrink-0 text-ink" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink
          to="/admin/sync"
          class="group flex items-center gap-4 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] transition-colors hover:bg-highlight"
        >
          <span
            class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-canvas text-ink"
          >
            <RefreshCw :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-lg font-black leading-tight text-ink"
            >Synchronisation des séances</span
          >
          <ArrowRight :size="20" class="shrink-0 text-ink" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink
          to="/admin/sync-schedules"
          class="group flex items-center gap-4 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] transition-colors hover:bg-highlight"
        >
          <span
            class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-canvas text-ink"
          >
            <CalendarClock :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-lg font-black leading-tight text-ink"
            >Planification des synchronisations</span
          >
          <ArrowRight :size="20" class="shrink-0 text-ink" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink
          to="/admin/theater-locations"
          class="group flex items-center gap-4 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] transition-colors hover:bg-highlight"
        >
          <span
            class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-canvas text-ink"
          >
            <MapPin :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-lg font-black leading-tight text-ink"
            >Localisations des cinémas</span
          >
          <ArrowRight :size="20" class="shrink-0 text-ink" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink
          to="/admin/cinemas"
          class="group flex items-center gap-4 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] transition-colors hover:bg-highlight"
        >
          <span
            class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-canvas text-ink"
          >
            <Image :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-lg font-black leading-tight text-ink"
            >Images des cinémas</span
          >
          <ArrowRight :size="20" class="shrink-0 text-ink" aria-hidden="true" />
        </NuxtLink>
        <NuxtLink
          to="/admin/accounts"
          class="group flex items-center gap-4 border-2 border-ink bg-surface p-5 shadow-[5px_5px_0_#27272a] transition-colors hover:bg-highlight"
        >
          <span
            class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-canvas text-ink"
          >
            <Users :size="22" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1 text-lg font-black leading-tight text-ink"
            >Comptes</span
          >
          <ArrowRight :size="20" class="shrink-0 text-ink" aria-hidden="true" />
        </NuxtLink>
      </div>
    </section>
  </main>
</template>
