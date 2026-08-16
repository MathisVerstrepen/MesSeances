<script setup lang="ts">
import { AlertTriangle, LoaderCircle, LockKeyhole } from '@lucide/vue'

const api = useMovieFlowApi()
const password = ref('')
const checkingSession = ref(true)
const submitting = ref(false)
const errorMessage = ref('')

async function checkSession() {
  checkingSession.value = true
  errorMessage.value = ''
  try {
    const session = await api.adminSession()
    if (session.authenticated) await navigateTo('/admin')
  } catch (error) {
    errorMessage.value = getFrenchAdminApiError(error)
  } finally {
    checkingSession.value = false
  }
}

async function login() {
  if (!password.value || submitting.value) return
  submitting.value = true
  errorMessage.value = ''
  try {
    await api.adminLogin(password.value)
    password.value = ''
    await navigateTo('/admin')
  } catch (error) {
    errorMessage.value = getFrenchAdminApiError(error)
  } finally {
    submitting.value = false
  }
}

onMounted(checkSession)
useHead({ title: 'Connexion administrateur — MovieFlow' })
</script>

<template>
  <main class="mx-auto flex min-h-[calc(100vh-7rem)] max-w-md items-center px-4 py-10 sm:px-6 lg:min-h-screen">
    <section class="w-full rounded-lg border border-line bg-surface p-6 shadow-sm sm:p-8" aria-labelledby="admin-login-title">
      <div class="mb-6 flex items-center gap-3">
        <span class="grid size-10 place-items-center rounded-md bg-subtle text-accent">
          <LockKeyhole :size="20" aria-hidden="true" />
        </span>
        <h1 id="admin-login-title" class="text-xl font-semibold tracking-tight text-ink">Connexion administrateur</h1>
      </div>

      <div v-if="checkingSession" class="flex min-h-32 items-center justify-center gap-3 text-sm text-muted" role="status" aria-live="polite">
        <LoaderCircle :size="21" class="animate-spin text-accent" aria-hidden="true" />
        Vérification de la session…
      </div>

      <form v-else class="space-y-5" @submit.prevent="login">
        <div>
          <label for="admin-password" class="mb-2 block text-sm font-semibold text-ink">Mot de passe</label>
          <input
            id="admin-password"
            v-model="password"
            class="field"
            type="password"
            autocomplete="current-password"
            required
            autofocus
          />
        </div>

        <div v-if="errorMessage" class="flex items-start gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800" role="alert">
          <AlertTriangle :size="18" class="mt-0.5 shrink-0" aria-hidden="true" />
          <span>{{ errorMessage }}</span>
        </div>

        <button type="submit" class="button-primary w-full" :disabled="submitting || !password">
          <LoaderCircle v-if="submitting" :size="18" class="animate-spin" aria-hidden="true" />
          {{ submitting ? 'Connexion…' : 'Se connecter' }}
        </button>
      </form>
    </section>
  </main>
</template>
