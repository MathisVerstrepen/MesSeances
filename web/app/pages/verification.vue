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
let timer: ReturnType<typeof setInterval> | undefined

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
  if (busy.value || cooldown.value > 0) return
  busy.value = true
  errorMessage.value = ''
  sent.value = false
  try {
    await api.requestVerification(email.value.trim())
    sent.value = true
    clear()
    startCooldown()
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
    if (error instanceof AccountApiError && error.status === 429)
      startCooldown(error.retryAfter || 60)
  } finally {
    busy.value = false
  }
}

async function confirm() {
  if (busy.value || !token.value || recovery.value) return
  errorMessage.value = ''
  busy.value = true
  const initialState = account.session.value?.state
  try {
    const session = await api.confirmVerification(token.value)
    clear()
    account.accept(session)
    account.notify()
    await navigateTo(accountDestination(session), { external: true })
  } catch (error) {
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
      if (
        (initialState === 'anonymous' || initialState === 'pending_email') &&
        account.session.value?.state === 'pending_username'
      ) {
        clear()
        await navigateTo(accountDestination(account.session.value), {
          external: true,
        })
      }
    }
  } finally {
    busy.value = false
  }
}

async function reconnectGoogle() {
  if (busy.value) return
  busy.value = true
  try {
    await startGoogle()
  } catch {
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
        :aria-busy="busy"
        @submit.prevent="confirm"
      >
        <button
          type="submit"
          class="button-primary min-h-12 w-full"
          :disabled="busy"
        >
          {{ busy ? 'Vérification…' : 'Confirmer mon email' }}
        </button>
      </form>
      <p v-else-if="!token" class="text-sm leading-relaxed">
        Ouvrez le lien reçu par email. Si vous avez actualisé cette page,
        rouvrez le lien ou demandez-en un nouveau.
      </p>
      <p
        v-if="errorMessage"
        role="alert"
        class="border-l-4 border-primary pl-3 text-sm leading-relaxed text-primary"
      >
        {{ errorMessage }}
      </p>
      <button
        v-if="recovery === 'google'"
        type="button"
        :disabled="busy"
        class="button-primary min-h-12 w-full"
        @click="reconnectGoogle"
      >
        {{ busy ? 'Connexion…' : 'Se connecter avec Google' }}
      </button>
      <form
        v-if="!recovery"
        class="space-y-5 border-t border-ink/20 pt-5"
        :aria-busy="busy"
        @submit.prevent="resend"
      >
        <h2 class="text-lg font-bold">Recevoir un nouveau lien</h2>
        <div>
          <label for="verification-email" class="mb-2 block text-sm font-bold"
            >Email</label
          >
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
            class="min-h-12 w-full rounded border border-ink/60 bg-surface px-3"
          >
        </div>
        <p v-if="sent" role="status" class="text-sm leading-relaxed">
          Si cette demande est éligible, un nouvel email sera envoyé. Ouvrez le
          dernier lien dans le navigateur utilisé pour l’inscription.
        </p>
        <button
          type="submit"
          class="min-h-12 w-full rounded border border-ink/60 bg-surface px-4 text-sm font-semibold disabled:opacity-50"
          :disabled="busy || cooldown > 0"
        >
          {{
            cooldown > 0 ? `Renvoyer dans ${cooldown} s` : 'Renvoyer le lien'
          }}
        </button>
      </form>
      <a
        v-if="recovery !== 'google'"
        href="/inscription"
        class="inline-flex min-h-11 items-center text-sm font-semibold underline underline-offset-4"
        >Recommencer l’inscription</a
      >
    </div>
  </AccountShell>
</template>
