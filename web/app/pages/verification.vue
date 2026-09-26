<script setup lang="ts">
import {
  AccountApiError,
  accountDestination,
  accountErrorMessage,
} from '~/utils/accountState'

definePageMeta({ middleware: 'account-auth' })
const api = useAccountApi()
const account = useAccountSession()
const startGoogle = useAccountGoogle()
const { token, ready, clear } = useAccountToken()
const email = ref(account.session.value?.account?.email ?? '')
const busy = ref(false)
const errorMessage = ref('')
const sent = ref(false)
const cooldown = ref(0)
const recovery = ref<'registration' | 'google' | null>(null)
const blocked = computed(() => busy.value || account.writesBlocked.value)
useAccountFlowDraft(email, token)
let timer: ReturnType<typeof setInterval> | undefined
const lifetime = useAccountLifetime(() => {
  if (timer) clearInterval(timer)
  busy.value = sent.value = false
  errorMessage.value = ''
  recovery.value = null
  cooldown.value = 0
})

function startCooldown(seconds = 60) {
  if (timer) clearInterval(timer)
  const deadline = Date.now() + seconds * 1000
  cooldown.value = seconds
  timer = setInterval(() => {
    cooldown.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
    if (cooldown.value === 0 && timer) clearInterval(timer)
  }, 1000)
}

async function resend() {
  if (blocked.value || cooldown.value > 0) return
  busy.value = true
  errorMessage.value = ''
  sent.value = false
  const current = lifetime.capture()
  try {
    await api.requestVerification(email.value.trim())
    if (!current()) return
    sent.value = true
    clear()
    startCooldown()
  } catch (error) {
    if (!current()) return
    errorMessage.value = accountErrorMessage(error)
    if (error instanceof AccountApiError && error.status === 429)
      startCooldown(error.retryAfter || 60)
  } finally {
    if (current()) busy.value = false
  }
}

async function confirm() {
  if (blocked.value || !token.value || recovery.value) return
  errorMessage.value = ''
  busy.value = true
  const initialState = account.session.value?.state
  const current = lifetime.capture()
  const active = lifetime.capture(false)
  try {
    const session = await api.confirmVerification(token.value)
    if (!current()) return
    clear()
    account.accept(session)
    account.notify()
    await navigateTo(accountDestination(session))
  } catch (error) {
    if (!current()) return
    errorMessage.value = accountErrorMessage(error)
    if (error instanceof AccountApiError) {
      if (error.code === 'verification_browser_required')
        recovery.value = 'registration'
      if (error.status === 401 && error.code === 'authentication_required') {
        recovery.value = 'google'
        errorMessage.value =
          'Votre session Google a expiré ou ne correspond pas à cette inscription. Connectez-vous avec le même compte Google, puis rouvrez ce lien.'
      }
    }
    // If the response was lost after committing, inspect state rather than POST again.
    if (
      error instanceof AccountApiError &&
      (error.status === 0 || error.status >= 500)
    ) {
      await account.refresh()
      if (!active()) return
      if (
        (initialState === 'anonymous' || initialState === 'pending_email') &&
        account.session.value?.state === 'pending_username'
      ) {
        clear()
        await navigateTo(accountDestination(account.session.value))
      }
    }
  } finally {
    if (active()) busy.value = false
  }
}

async function reconnectGoogle() {
  if (blocked.value) return
  busy.value = true
  const current = lifetime.capture()
  try {
    await startGoogle()
  } catch {
    if (!current()) return
    errorMessage.value =
      'La connexion Google est indisponible. Réessayez avec le même compte Google, puis rouvrez ce lien.'
    busy.value = false
  }
}

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
useHead({ title: 'Vérifier mon email - MesSeances' })
</script>

<template>
  <AccountShell title="Vérifier mon email">
    <p v-if="!ready" role="status" class="text-sm">Ouverture du lien…</p>
    <div v-else class="space-y-8">
      <form
        v-if="token && !recovery"
        class="space-y-5"
        :aria-busy="blocked"
        @submit.prevent="confirm"
      >
        <button
          type="submit"
          class="account-primary w-full"
          :disabled="blocked"
        >
          {{ busy ? 'Vérification…' : 'Confirmer mon email' }}
        </button>
      </form>
      <p v-else-if="!token" class="text-sm leading-relaxed">
        Ouvrez le lien reçu par email. Si vous avez actualisé cette page,
        rouvrez le lien ou demandez-en un nouveau.
      </p>
      <p v-if="errorMessage" role="alert" class="account-alert">
        {{ errorMessage }}
      </p>
      <button
        v-if="recovery === 'google'"
        type="button"
        :disabled="blocked"
        class="account-primary w-full"
        @click="reconnectGoogle"
      >
        {{ busy ? 'Connexion…' : 'Se connecter avec Google' }}
      </button>
      <form
        v-if="!token && !recovery"
        class="space-y-5 border-t-2 border-ink pt-6"
        :aria-busy="blocked"
        @submit.prevent="resend"
      >
        <h2 class="account-heading">Recevoir un nouveau lien</h2>
        <div>
          <label for="verification-email" class="account-label">Email</label>
          <input
            id="verification-email"
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
          Si cette demande est éligible, un nouvel email sera envoyé. Ouvrez le
          dernier lien dans le navigateur utilisé pour l’inscription.
        </p>
        <button
          type="submit"
          class="account-secondary w-full"
          :disabled="blocked || cooldown > 0"
        >
          {{
            cooldown > 0 ? `Renvoyer dans ${cooldown} s` : 'Renvoyer le lien'
          }}
        </button>
      </form>
      <NuxtLink
        v-if="recovery !== 'google'"
        to="/inscription"
        :prefetch="false"
        class="account-link"
        >Recommencer l’inscription</NuxtLink
      >
    </div>
  </AccountShell>
</template>
