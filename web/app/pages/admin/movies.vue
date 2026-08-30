<script setup lang="ts">
import { AlertTriangle, ArrowLeft, LoaderCircle, LogOut } from '@lucide/vue'

definePageMeta({ middleware: 'admin-auth' })

const api = useMesSeancesApi()
const loggingOut = ref(false)
const logoutError = ref('')

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

useHead({ title: 'Métadonnées des films - MesSeances' })
</script>

<template>
  <main class="mx-auto max-w-[1600px] px-4 py-6 sm:px-6 sm:py-8 lg:px-8">
    <div class="flex flex-col gap-4 border-b border-line pb-5 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <NuxtLink to="/admin" class="mb-1 inline-flex min-h-11 items-center gap-1 text-sm font-semibold text-muted hover:text-accent">
          <ArrowLeft :size="16" aria-hidden="true" /> Administration
        </NuxtLink>
        <h1 class="text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">Métadonnées des films</h1>
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

    <ClientOnly>
      <AdminMoviesGrid class="mt-5" />
      <template #fallback>
        <div class="state-panel mt-5" role="status" aria-live="polite">
          <LoaderCircle :size="28" class="animate-spin text-accent" aria-hidden="true" />
          <p>Chargement de l’éditeur…</p>
        </div>
      </template>
    </ClientOnly>
  </main>
</template>
