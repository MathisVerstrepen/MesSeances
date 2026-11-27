<script setup lang="ts">
import { MailX } from '@lucide/vue'
import { FetchError } from 'ofetch'

// Reached from the digest footer. The signed token can only turn the digest
// off; an explicit click is required so link scanners never unsubscribe.
useHead({
  title: 'Se désabonner du résumé - MesSeances',
  meta: [
    { name: 'robots', content: 'noindex, nofollow' },
    { name: 'referrer', content: 'no-referrer' },
  ],
})
const route = useRoute()
// Only a single query value is a token; arrays and absent values are not.
const token = computed(() => {
  const value = route.query.token
  return Array.isArray(value) ? '' : (value ?? '')
})
const state = ref<'idle' | 'pending' | 'done' | 'error'>('idle')
const message = ref('')

async function unsubscribe() {
  if (!token.value || state.value === 'pending') return
  state.value = 'pending'
  message.value = ''
  try {
    await $fetch('/api/v1/notifications/unsubscribe', {
      method: 'POST',
      query: { token: token.value },
      body: 'List-Unsubscribe=One-Click',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      credentials: 'omit',
      cache: 'no-store',
      retry: false,
    })
    state.value = 'done'
    // Drop the capability from the address bar and history.
    if (import.meta.client) history.replaceState(null, '', location.pathname)
  } catch (cause) {
    state.value = 'error'
    const status = cause instanceof FetchError ? cause.status : 0
    message.value =
      status === 400
        ? 'Ce lien n’est plus valide. Vous pouvez gérer le résumé depuis votre compte.'
        : status === 429
          ? 'Trop de tentatives. Réessayez dans quelques minutes.'
          : 'Le service est indisponible. Réessayez plus tard.'
  }
}
</script>

<template>
  <main class="mx-auto w-full max-w-xl px-4 py-12 sm:py-20">
    <div
      class="space-y-5 border-2 border-ink bg-surface p-6 shadow-[6px_6px_0_#27272a] sm:p-9"
    >
      <MailX :size="28" aria-hidden="true" />
      <h1 class="editorial-title text-[clamp(1.8rem,5vw,2.6rem)]">
        Résumé par e-mail
      </h1>
      <template v-if="state === 'done'">
        <p role="status" class="text-base leading-relaxed">
          C’est fait : vous ne recevrez plus le résumé des alertes. Vos alertes
          et vos notifications dans l’application restent actives.
        </p>
        <NuxtLink
          to="/compte/notifications?onglet=alertes"
          class="editorial-button-outline"
          >Gérer mes alertes</NuxtLink
        >
      </template>
      <template v-else-if="!token">
        <p class="text-base leading-relaxed">
          Ce lien est incomplet. Vous pouvez désactiver le résumé depuis votre
          compte.
        </p>
        <NuxtLink
          to="/compte/notifications?onglet=alertes"
          class="editorial-button-outline"
          >Gérer mes alertes</NuxtLink
        >
      </template>
      <template v-else>
        <p class="text-base leading-relaxed">
          Ne plus recevoir le résumé des alertes MesSeances ? Vos alertes et vos
          notifications dans l’application ne changent pas.
        </p>
        <button
          type="button"
          class="editorial-button"
          :disabled="state === 'pending'"
          @click="unsubscribe"
        >
          {{ state === 'pending' ? 'Désabonnement…' : 'Me désabonner' }}
        </button>
        <p v-if="message" role="alert" class="editorial-alert p-3">
          {{ message }}
        </p>
      </template>
    </div>
  </main>
</template>
