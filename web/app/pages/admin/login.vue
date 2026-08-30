<script setup lang="ts">
import { AlertTriangle, LoaderCircle, LockKeyhole } from '@lucide/vue'

const api = useMesSeancesApi()
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
useHead({ title: 'Connexion administrateur - MesSeances' })
</script>

<template>
  <main class="flex min-h-[calc(100vh-7rem)] items-center bg-[#f8f7f2] px-4 py-12 [background-image:linear-gradient(rgba(39,39,42,0.08)_1px,transparent_1px),linear-gradient(90deg,rgba(39,39,42,0.08)_1px,transparent_1px)] [background-size:28px_28px] sm:px-6 sm:py-16 lg:px-10 lg:py-20">
    <section class="mx-auto w-full max-w-2xl border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a] sm:shadow-[9px_9px_0_#27272a]" aria-labelledby="admin-login-title">
      <header class="border-b-2 border-ink bg-[#ffcf3f] p-5 sm:p-7">
        <div class="mb-8 flex items-center justify-between gap-4 sm:mb-10">
          <p class="font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-ink sm:text-[11px]">Administration / Connexion</p>
          <span class="grid size-10 shrink-0 rotate-2 place-items-center rounded-[3px] border-2 border-ink bg-surface shadow-[3px_3px_0_#27272a] sm:size-11">
            <LockKeyhole :size="20" stroke-width="2.5" aria-hidden="true" />
          </span>
        </div>
        <h1 id="admin-login-title" class="max-w-xl text-[clamp(2rem,10vw,4.6rem)] font-black leading-[0.88] tracking-[-0.065em] text-ink">
          Connexion<br /><span class="inline-block bg-surface px-1.5 pb-1">administrateur</span>
        </h1>
      </header>

      <div class="p-5 sm:p-7">
        <div v-if="checkingSession" class="flex min-h-44 items-center justify-center gap-3 font-mono text-[10px] font-bold uppercase tracking-[0.08em] text-ink sm:text-xs sm:tracking-[0.1em]" role="status" aria-live="polite">
          <LoaderCircle :size="21" class="animate-spin text-primary" aria-hidden="true" />
          Vérification de la session…
        </div>

        <form v-else class="space-y-6" :aria-busy="submitting" @submit.prevent="login">
          <div>
            <label for="admin-password" class="mb-2.5 block font-mono text-[11px] font-bold uppercase tracking-[0.14em] text-ink">Mot de passe</label>
            <input
              id="admin-password"
              v-model="password"
              class="h-12 w-full rounded-[3px] border-2 border-ink bg-[#f8f7f2] px-3.5 text-base font-semibold text-ink transition-shadow hover:shadow-[3px_3px_0_#ffcf3f] focus:bg-surface focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60"
              type="password"
              autocomplete="current-password"
              required
              autofocus
              :disabled="submitting"
              :aria-invalid="errorMessage ? 'true' : undefined"
              :aria-describedby="errorMessage ? 'admin-login-error' : undefined"
            />
          </div>

          <div v-if="errorMessage" id="admin-login-error" class="flex items-start gap-2.5 border-2 border-primary bg-primary-soft p-3.5 text-sm font-bold text-primary" role="alert">
            <AlertTriangle :size="19" class="mt-0.5 shrink-0" stroke-width="2.5" aria-hidden="true" />
            <span>{{ errorMessage }}</span>
          </div>

          <button type="submit" class="flex min-h-12 w-full items-center justify-center gap-2 border-2 border-ink bg-ink px-5 font-mono text-xs font-extrabold uppercase tracking-[0.1em] text-white shadow-[5px_5px_0_#ffcf3f] transition hover:-translate-y-0.5 hover:bg-primary active:translate-x-1 active:translate-y-1 active:shadow-none disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:translate-y-0" :disabled="submitting || !password">
            <LoaderCircle v-if="submitting" :size="18" class="animate-spin" aria-hidden="true" />
            {{ submitting ? 'Connexion…' : 'Se connecter' }}
          </button>
        </form>
      </div>
    </section>
  </main>
</template>
