<script setup lang="ts">
import {
  accountErrorMessage,
  accountWriteUncertain,
  passwordCriteria,
} from '~/utils/accountState'

definePageMeta({ middleware: 'account-auth' })
const api = useAccountApi()
const account = useAccountSession()
const { token, ready, clear } = useAccountToken()
const password = ref('')
const clearPassword = useAccountFlowDraft(password, token)
const busy = ref(false)
const done = ref(false)
const uncertain = ref(false)
const errorMessage = ref('')
const blocked = computed(() => busy.value || account.writesBlocked.value)

async function confirm() {
  if (blocked.value || !token.value || uncertain.value) return
  const criteria = passwordCriteria(password.value)
  if (!criteria.minimum || !criteria.maximum) {
    errorMessage.value = 'Choisissez un mot de passe de 10 à 128 caractères.'
    return
  }
  busy.value = true
  errorMessage.value = ''
  try {
    await api.confirmPasswordReset(token.value, password.value)
    clear()
    clearPassword()
    done.value = true
    account.clear()
    account.notify()
    await account.refresh()
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
    if (accountWriteUncertain(error)) {
      uncertain.value = true
      clear()
      clearPassword()
      account.notify()
      await account.refresh()
      errorMessage.value =
        'La réponse a été interrompue. Le mot de passe a peut-être été modifié. Essayez de vous connecter avec le nouveau mot de passe ; sinon, demandez un nouveau lien.'
    }
  } finally {
    busy.value = false
  }
}
useHead({ title: 'Réinitialiser mon mot de passe - MesSeances' })
</script>

<template>
  <AccountShell title="Réinitialiser mon mot de passe">
    <p v-if="!ready" role="status" class="text-sm">Ouverture du lien…</p>
    <p v-else-if="done" role="status" class="text-sm leading-relaxed">
      Votre mot de passe a été modifié. Toutes vos sessions ont été fermées.
      Connectez-vous avec votre nouveau mot de passe.
    </p>
    <form
      v-else-if="token && !uncertain"
      class="space-y-5"
      :aria-busy="blocked"
      @submit.prevent="confirm"
    >
      <AccountPasswordField
        id="reset-password"
        v-model="password"
        new-password
        :disabled="busy"
        label="Nouveau mot de passe"
      />
      <p class="text-sm leading-relaxed">
        Cette action déconnecte tous vos appareils.
      </p>
      <button type="submit" class="account-primary w-full" :disabled="blocked">
        {{ busy ? 'Modification…' : 'Modifier mon mot de passe' }}
      </button>
    </form>
    <p v-else-if="!uncertain" class="text-sm leading-relaxed">
      Ouvrez le lien reçu par email. Si vous avez actualisé cette page, rouvrez
      le lien ou demandez-en un nouveau.
    </p>
    <p v-if="errorMessage" role="alert" class="account-alert mt-5">
      {{ errorMessage }}
    </p>
    <div class="mt-6 flex flex-col items-start gap-2">
      <a href="/connexion" class="account-link">Se connecter</a>
      <a v-if="!done" href="/mot-de-passe-oublie" class="account-link"
        >Recevoir un nouveau lien</a
      >
    </div>
  </AccountShell>
</template>
