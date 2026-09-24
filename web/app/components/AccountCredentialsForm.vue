<script setup lang="ts">
import {
  accountDestination,
  accountErrorMessage,
  passwordCriteria,
} from '~/utils/accountState'

const props = defineProps<{ register?: boolean }>()
const api = useAccountApi()
const account = useAccountSession()
const startGoogle = useAccountGoogle()
const email = ref('')
const password = ref('')
const busy = ref(false)
const errorMessage = ref('')
const sent = ref(false)
const blocked = computed(() => busy.value || account.writesBlocked.value)
useAccountFlowDraft(email, password)
const lifetime = useAccountLifetime(() => {
  busy.value = false
  sent.value = false
  errorMessage.value = ''
})

async function submit() {
  if (blocked.value) return
  errorMessage.value = ''
  if (props.register) {
    const criteria = passwordCriteria(password.value)
    if (!criteria.minimum || !criteria.maximum) {
      errorMessage.value = 'Choisissez un mot de passe de 10 à 128 caractères.'
      return
    }
  }
  busy.value = true
  const current = lifetime.capture()
  try {
    if (props.register) {
      await api.register(email.value.trim(), password.value)
      if (!current()) return
      password.value = ''
      sent.value = true
    } else {
      const session = await api.login(email.value.trim(), password.value)
      if (!current()) return
      password.value = ''
      account.accept(session)
      account.notify()
      await navigateTo(accountDestination(session))
    }
  } catch (error) {
    if (!current()) return
    errorMessage.value = accountErrorMessage(error)
  } finally {
    if (current()) busy.value = false
  }
}

async function google() {
  const current = lifetime.capture()
  if (blocked.value) return
  busy.value = true
  errorMessage.value = ''
  try {
    password.value = ''
    await startGoogle()
  } catch {
    if (!current()) return
    errorMessage.value =
      'La connexion Google est indisponible ou a été interrompue. Réessayez ou utilisez votre email.'
    busy.value = false
  }
}
</script>

<template>
  <div v-if="sent" class="space-y-5">
    <p role="status" class="text-sm leading-relaxed">
      Si cette adresse peut être utilisée, un email de vérification sera envoyé.
      Ouvrez son lien dans ce navigateur pour confirmer votre adresse.
    </p>
    <div class="flex flex-wrap items-center gap-4">
      <NuxtLink to="/verification" :prefetch="false" class="account-primary"
        >Vérifier mon adresse</NuxtLink
      >
      <NuxtLink to="/connexion" :prefetch="false" class="account-link"
        >Se connecter</NuxtLink
      >
    </div>
  </div>
  <div v-else class="space-y-4">
    <form class="space-y-4" :aria-busy="blocked" @submit.prevent="submit">
      <div class="space-y-6">
        <div>
          <label for="account-email" class="account-label">Email</label>
          <input
            id="account-email"
            v-model="email"
            type="email"
            autocomplete="email"
            inputmode="email"
            autocapitalize="none"
            :spellcheck="false"
            maxlength="254"
            required
            :disabled="busy"
            class="account-input w-full"
          >
        </div>
        <AccountPasswordField
          id="account-password"
          v-model="password"
          :new-password="register"
          :disabled="busy"
        >
          <template v-if="!register" #label-action>
            <NuxtLink
              to="/mot-de-passe-oublie"
              :prefetch="false"
              class="account-navigation-link"
              >Mot de passe oublié ?</NuxtLink
            >
          </template>
        </AccountPasswordField>
      </div>
      <p
        v-if="errorMessage"
        id="account-credentials-error"
        role="alert"
        class="account-alert"
      >
        {{ errorMessage }}
      </p>
      <button type="submit" class="account-primary w-full" :disabled="blocked">
        {{
          busy ? 'Veuillez patienter…' : register ? 'Créer mon compte' : 'Se connecter'
        }}
      </button>
    </form>
    <div class="flex items-center gap-3 text-xs text-muted">
      <span class="h-px flex-1 bg-ink/20" aria-hidden="true" />
      <span>ou</span>
      <span class="h-px flex-1 bg-ink/20" aria-hidden="true" />
    </div>
    <button
      type="button"
      :disabled="blocked"
      class="account-secondary w-full"
      @click="google"
    >
      <GoogleIcon />
      Continuer avec Google
    </button>
    <NuxtLink
      :to="register ? '/connexion' : '/inscription'"
      :prefetch="false"
      class="account-navigation-link"
      >{{
        register ? 'Déjà un compte ? Se connecter' : 'Créer un compte'
      }}</NuxtLink
    >
  </div>
</template>
