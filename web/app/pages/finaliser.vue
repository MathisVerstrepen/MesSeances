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
const blocked = computed(() => busy.value || account.writesBlocked.value)
useAccountFlowDraft(username)
const lifetime = useAccountLifetime(() => {
  busy.value = false
  errorMessage.value = ''
  touched.value = false
})

async function submit() {
  if (blocked.value) return
  touched.value = true
  if (!valid.value) return
  busy.value = true
  errorMessage.value = ''
  username.value = normalizeAccountUsername(username.value)
  const current = lifetime.capture()
  const active = lifetime.capture(false)
  try {
    const session = await api.username(username.value)
    if (!current()) return
    account.accept(session)
    account.notify()
    await navigateTo(accountDestination(session))
  } catch (error) {
    if (!current()) return
    errorMessage.value = accountErrorMessage(error)
    // Recover an ambiguous successful write without replaying the claim.
    await account.refresh()
    if (!active()) return
    if (account.session.value?.state === 'complete')
      await navigateTo('/connexion')
  } finally {
    if (active()) busy.value = false
  }
}

useHead({ title: 'Choisir mon nom - MesSeances' })
</script>

<template>
  <AccountShell title="Choisir mon nom">
    <form
      v-if="account.session.value?.state === 'pending_username'"
      class="space-y-5"
      :aria-busy="blocked"
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
      <button type="submit" class="account-primary w-full" :disabled="blocked">
        {{ busy ? 'Enregistrement…' : 'Confirmer mon nom' }}
      </button>
    </form>
    <NuxtLink v-else to="/connexion" :prefetch="false" class="account-link"
      >Reprendre la connexion</NuxtLink
    >
  </AccountShell>
</template>
