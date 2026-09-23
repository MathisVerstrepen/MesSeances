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

async function submit() {
  if (busy.value) return
  errorMessage.value = ''
  if (props.register) {
    const criteria = passwordCriteria(password.value)
    if (!criteria.minimum || !criteria.maximum) {
      errorMessage.value = 'Choisissez un mot de passe de 10 à 128 caractères.'
      return
    }
  }
  busy.value = true
  try {
    if (props.register) {
      await api.register(email.value.trim(), password.value)
      password.value = ''
      sent.value = true
    } else {
      const session = await api.login(email.value.trim(), password.value)
      password.value = ''
      account.accept(session)
      account.notify()
      await navigateTo(accountDestination(session), { external: true })
    }
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
  } finally {
    busy.value = false
  }
}

async function google() {
  if (busy.value) return
  busy.value = true
  errorMessage.value = ''
  try {
    password.value = ''
    await startGoogle()
  } catch {
    errorMessage.value =
      'La connexion Google est indisponible ou a été interrompue. Réessayez ou utilisez votre email.'
    busy.value = false
  }
}

function clearPassword() {
  password.value = ''
}
onMounted(() => window.addEventListener('pagehide', clearPassword))
onBeforeUnmount(() => {
  clearPassword()
  if (import.meta.client) window.removeEventListener('pagehide', clearPassword)
})
</script>

<template>
  <div v-if="sent" class="space-y-5">
    <p role="status" class="text-sm leading-relaxed">
      Si cette adresse peut être utilisée, un email de vérification sera envoyé.
      Ouvrez son lien dans ce navigateur pour confirmer votre adresse.
    </p>
    <div class="flex flex-wrap items-center gap-4">
      <a href="/verification" class="account-primary">Vérifier mon adresse</a>
      <a href="/connexion" class="account-link">Se connecter</a>
    </div>
  </div>
  <div v-else class="space-y-4">
    <form class="space-y-4" :aria-busy="busy" @submit.prevent="submit">
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
            <a href="/mot-de-passe-oublie" class="account-navigation-link"
              >Mot de passe oublié ?</a
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
      <button type="submit" class="account-primary w-full" :disabled="busy">
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
      :disabled="busy"
      class="account-secondary w-full"
      @click="google"
    >
      <!-- Google artwork: https://developers.google.com/static/identity/images/branding_guideline_sample_lt_sq_sl.svg -->
      <svg
        xmlns="http://www.w3.org/2000/svg"
        viewBox="10 10 20 20"
        class="size-5 shrink-0"
        aria-hidden="true"
        focusable="false"
      >
        <path
          d="M29.6 20.2273C29.6 19.5182 29.5364 18.8364 29.4182 18.1818H20V22.05H25.3818C25.15 23.3 24.4455 24.3591 23.3864 25.0682V27.5773H26.6182C28.5091 25.8364 29.6 23.2727 29.6 20.2273Z"
          fill="#4285F4"
        />
        <path
          d="M20 30C22.7 30 24.9636 29.1045 26.6181 27.5773L23.3863 25.0682C22.4909 25.6682 21.3454 26.0227 20 26.0227C17.3954 26.0227 15.1909 24.2636 14.4045 21.9H11.0636V24.4909C12.7091 27.7591 16.0909 30 20 30Z"
          fill="#34A853"
        />
        <path
          d="M14.4045 21.9C14.2045 21.3 14.0909 20.6591 14.0909 20C14.0909 19.3409 14.2045 18.7 14.4045 18.1V15.5091H11.0636C10.3864 16.8591 10 18.3864 10 20C10 21.6136 10.3864 23.1409 11.0636 24.4909L14.4045 21.9Z"
          fill="#FBBC04"
        />
        <path
          d="M20 13.9773C21.4681 13.9773 22.7863 14.4818 23.8227 15.4727L26.6909 12.6045C24.9591 10.9909 22.6954 10 20 10C16.0909 10 12.7091 12.2409 11.0636 15.5091L14.4045 18.1C15.1909 15.7364 17.3954 13.9773 20 13.9773Z"
          fill="#E94235"
        />
      </svg>
      Continuer avec Google
    </button>
    <a
      :href="register ? '/connexion' : '/inscription'"
      class="account-navigation-link"
      >{{
        register ? 'Déjà un compte ? Se connecter' : 'Créer un compte'
      }}</a
    >
  </div>
</template>
