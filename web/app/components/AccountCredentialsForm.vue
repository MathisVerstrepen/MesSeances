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
    <a href="/verification" class="button-primary min-h-12"
      >Vérifier mon adresse</a
    >
    <a
      href="/connexion"
      class="flex min-h-11 items-center text-sm font-semibold underline underline-offset-4"
      >Se connecter</a
    >
  </div>
  <div v-else class="space-y-6">
    <form class="space-y-5" :aria-busy="busy" @submit.prevent="submit">
      <div>
        <label for="account-email" class="mb-2 block text-sm font-bold"
          >Email</label
        >
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
          class="min-h-12 w-full rounded border border-ink/60 bg-surface px-3 disabled:opacity-60"
        >
      </div>
      <AccountPasswordField
        id="account-password"
        v-model="password"
        :new-password="register"
        :disabled="busy"
      />
      <p
        v-if="errorMessage"
        id="account-credentials-error"
        role="alert"
        class="border-l-4 border-primary pl-3 text-sm leading-relaxed text-primary"
      >
        {{ errorMessage }}
      </p>
      <button
        type="submit"
        class="button-primary min-h-12 w-full"
        :disabled="busy"
      >
        {{
          busy ? 'Veuillez patienter…' : register ? 'Créer mon compte' : 'Se connecter'
        }}
      </button>
    </form>
    <button
      type="button"
      :disabled="busy"
      class="min-h-12 w-full rounded border border-ink/60 bg-surface px-4 text-sm font-semibold disabled:opacity-50"
      @click="google"
    >
      Continuer avec Google
    </button>
    <a
      :href="register ? '/connexion' : '/inscription'"
      class="inline-flex min-h-11 items-center text-sm font-semibold underline underline-offset-4"
      >{{
        register ? 'Déjà un compte ? Se connecter' : 'Créer un compte'
      }}</a
    >
  </div>
</template>
