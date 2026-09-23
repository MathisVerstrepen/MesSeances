<script setup lang="ts">
import { accountErrorMessage } from '~/utils/accountState'

defineProps<{ title: string; hideExplore?: boolean }>()
const account = useAccountSession()
const route = useRoute()
const { session, status, errorMessage } = account
const signingOut = ref(false)
const signOutError = ref('')

async function logout() {
  if (signingOut.value) return
  signingOut.value = true
  signOutError.value = ''
  try {
    await account.logout()
    await navigateTo('/connexion', { external: true })
  } catch (error) {
    signOutError.value = accountErrorMessage(error)
  } finally {
    signingOut.value = false
  }
}

useHead({
  meta: [
    { name: 'robots', content: 'noindex,nofollow' },
    { name: 'referrer', content: 'no-referrer' },
  ],
})
</script>

<template>
  <main class="mx-auto w-full max-w-lg px-5 py-10 text-ink sm:py-16">
    <h1
      class="mb-8 border-b-2 border-ink pb-5 text-3xl font-black tracking-tight sm:text-4xl"
    >
      {{ title }}
    </h1>
    <div
      v-if="status === 'idle' || status === 'loading'"
      role="status"
      class="space-y-5 motion-safe:animate-pulse"
    >
      <span class="sr-only">Vérification de la session…</span>
      <div class="h-12 rounded bg-ink/10" />
      <div class="h-12 rounded bg-ink/10" />
      <div class="h-12 w-2/3 rounded bg-ink/10" />
    </div>
    <div v-else-if="status === 'error'" class="space-y-5">
      <p role="alert" class="border-l-4 border-primary pl-4 text-sm">
        {{ errorMessage }}
      </p>
      <button
        type="button"
        class="button-primary min-h-11"
        @click="account.refresh"
      >
        Réessayer
      </button>
    </div>
    <p
      v-else-if="session && !session.enabled"
      role="status"
      class="text-sm leading-relaxed"
    >
      Les comptes ne sont pas encore disponibles. Vous pouvez continuer à
      explorer les séances.
    </p>
    <div v-show="status === 'ready' && session?.enabled">
      <slot />
    </div>
    <div v-if="session?.account" class="mt-8 border-t border-ink/20 pt-4">
      <button
        type="button"
        class="min-h-11 text-sm font-semibold underline underline-offset-4 disabled:opacity-50"
        :disabled="signingOut"
        @click="logout"
      >
        {{ signingOut ? 'Déconnexion…' : 'Se déconnecter' }}
      </button>
      <p v-if="signOutError" role="alert" class="mt-3 text-sm text-primary">
        {{ signOutError }}
      </p>
    </div>
    <a
      v-if="!hideExplore && route.path.toLowerCase().startsWith('/compte')"
      href="/"
      class="mt-6 inline-flex min-h-11 items-center text-sm font-semibold underline underline-offset-4"
      >Explorer les séances</a
    >
  </main>
</template>
