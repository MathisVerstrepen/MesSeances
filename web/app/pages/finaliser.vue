<script setup lang="ts">
import {
  accountDestination,
  accountErrorMessage,
  normalizeAccountUsername,
  validAccountUsername,
} from '~/utils/accountState'

definePageMeta({ middleware: 'account-auth' })
const api = useAccountApi()
const account = useAccountSession()
const username = ref('')
const touched = ref(false)
const busy = ref(false)
const errorMessage = ref('')
const valid = computed(() => validAccountUsername(username.value))

async function submit() {
  touched.value = true
  if (!valid.value || busy.value) return
  busy.value = true
  errorMessage.value = ''
  username.value = normalizeAccountUsername(username.value)
  try {
    const session = await api.username(username.value)
    account.accept(session)
    account.notify()
    await navigateTo(accountDestination(session), { external: true })
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
    // Recover an ambiguous successful write without replaying the claim.
    await account.refresh()
    if (account.session.value?.state === 'complete')
      await navigateTo('/connexion', { external: true })
  } finally {
    busy.value = false
  }
}

useHead({ title: 'Choisir mon nom - MesSeances' })
</script>

<template>
  <AccountShell title="Choisir mon nom">
    <form
      v-if="account.session.value?.state === 'pending_username'"
      class="space-y-5"
      :aria-busy="busy"
      @submit.prevent="submit"
    >
      <div>
        <label for="account-username" class="account-label"
          >Nom d’utilisateur</label
        >
        <input
          id="account-username"
          v-model="username"
          type="text"
          autocomplete="username"
          autocapitalize="none"
          :spellcheck="false"
          required
          maxlength="30"
          :disabled="busy"
          :aria-invalid="touched && !valid || undefined"
          aria-describedby="username-rules username-fixed"
          class="account-input w-full"
          @blur="touched = true; username = normalizeAccountUsername(username)"
        >
        <p
          id="username-rules"
          class="mt-3 text-xs leading-relaxed"
          :class="touched && !valid ? 'text-primary' : ''"
        >
          3 à 30 caractères : lettres sans accent, chiffres et tirets bas.
          Commencez par une lettre. Les majuscules deviennent des minuscules.
        </p>
      </div>
      <p id="username-fixed" class="text-sm font-semibold">
        Ce nom est définitif. Il restera réservé si vous supprimez votre compte.
      </p>
      <p v-if="errorMessage" role="alert" class="account-alert">
        {{ errorMessage }}
      </p>
      <button type="submit" class="account-primary w-full" :disabled="busy">
        {{ busy ? 'Enregistrement…' : 'Confirmer mon nom' }}
      </button>
    </form>
    <a v-else href="/connexion" class="account-link">Reprendre la connexion</a>
  </AccountShell>
</template>
