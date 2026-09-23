<script setup lang="ts">
import {
  AccountApiError,
  accountErrorMessage,
  normalizeAccountEmail,
} from '~/utils/accountState'

definePageMeta({ middleware: 'account-auth' })
const api = useAccountApi()
const email = ref('')
const busy = ref(false)
const sent = ref(false)
const errorMessage = ref('')
const cooldown = ref(0)
let timer: ReturnType<typeof setInterval> | undefined

function startCooldown(seconds = 60) {
  if (timer) clearInterval(timer)
  const deadline = Date.now() + seconds * 1000
  cooldown.value = seconds
  timer = setInterval(() => {
    cooldown.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
    if (!cooldown.value && timer) clearInterval(timer)
  }, 1000)
}

async function submit() {
  if (busy.value || cooldown.value) return
  busy.value = true
  sent.value = false
  errorMessage.value = ''
  try {
    await api.requestPasswordReset(normalizeAccountEmail(email.value))
    sent.value = true
    startCooldown()
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
    if (error instanceof AccountApiError && error.status === 429)
      startCooldown(error.retryAfter || 60)
  } finally {
    busy.value = false
  }
}
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
useHead({ title: 'Mot de passe oublié - MesSeances' })
</script>

<template>
  <AccountShell title="Mot de passe oublié">
    <form class="space-y-5" :aria-busy="busy" @submit.prevent="submit">
      <div>
        <label for="reset-email" class="account-label">Email du compte</label>
        <input
          id="reset-email"
          v-model="email"
          type="email"
          inputmode="email"
          autocomplete="email"
          autocapitalize="none"
          :spellcheck="false"
          maxlength="254"
          required
          :disabled="busy"
          class="account-input w-full"
        >
      </div>
      <p v-if="sent" role="status" class="text-sm leading-relaxed">
        Si cette adresse correspond à un compte avec mot de passe, un lien de
        réinitialisation sera envoyé. Seul le dernier lien reste valable.
      </p>
      <p v-if="errorMessage" role="alert" class="account-alert">
        {{ errorMessage }}
      </p>
      <button
        type="submit"
        class="account-primary w-full"
        :disabled="busy || cooldown > 0"
      >
        {{
          cooldown ? `Renvoyer dans ${cooldown} s` : busy ? 'Demande en cours…' : 'Recevoir un lien'
        }}
      </button>
    </form>
    <p class="mt-6 text-sm leading-relaxed">
      Si vous utilisez uniquement Google, reconnectez-vous avec Google : ce lien
      n’ajoute pas de mot de passe.
    </p>
    <a href="/connexion" class="account-link mt-4">Revenir à la connexion</a>
  </AccountShell>
</template>
