<script setup lang="ts">
import {
  accountDestination,
  accountErrorMessage,
  accountWriteUncertain,
} from '~/utils/accountState'

definePageMeta({ middleware: 'account-auth' })
const api = useAccountApi()
const account = useAccountSession()
const {
  details,
  loading,
  errorMessage: detailsError,
  refresh,
} = useAccountDetails()
const { token, ready, clear } = useAccountToken()
const passwordAction = useAccountPasswordAction()
const startGoogle = useAccountGoogle()
const password = ref('')
const clearPassword = useAccountSecrets(password)
const busy = ref(false)
const done = ref(false)
const uncertain = ref(false)
const errorMessage = ref('')
const complete = computed(() => account.session.value?.state === 'complete')
watch(complete, (value) => {
  if (!value) clearPassword()
})

async function googleProof() {
  if (busy.value || !details.value?.pending_email || !complete.value) return
  busy.value = true
  errorMessage.value = ''
  const target = details.value.pending_email
  // This bearer must not survive OAuth. Ask for the original email link after proof.
  clear()
  clearPassword()
  try {
    await startGoogle({ mode: 'reauth', action: 'email_change', target })
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
  } finally {
    busy.value = false
  }
}

async function confirm() {
  if (
    busy.value ||
    !token.value ||
    !details.value?.pending_email ||
    !details.value.has_password ||
    !details.value.allowed_methods.includes('password') ||
    uncertain.value
  )
    return
  busy.value = true
  errorMessage.value = ''
  const confirmationToken = token.value
  const target = details.value.pending_email
  try {
    const applied = await passwordAction(
      password.value,
      'email_change',
      target,
      (grant) => api.confirmEmailChange(confirmationToken, grant),
    )
    if (!applied) {
      errorMessage.value =
        'La session a été revérifiée. Saisissez à nouveau votre mot de passe et recommencez.'
      return
    }
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
        'La réponse a été interrompue. Le changement a peut-être été confirmé. Essayez de vous connecter avec la nouvelle adresse ou consultez votre compte avant de demander un autre lien.'
    }
  } finally {
    busy.value = false
  }
}
useHead({ title: 'Confirmer mon nouvel email - MesSeances' })
</script>

<template>
  <AccountShell title="Confirmer mon nouvel email">
    <p v-if="!ready" role="status" class="text-sm">Ouverture du lien…</p>
    <p v-else-if="done" role="status" class="text-sm leading-relaxed">
      Votre email a été modifié. Toutes vos sessions ont été fermées.
      Connectez-vous avec votre nouvelle adresse ou votre compte Google associé.
    </p>
    <p v-else-if="uncertain" role="status" class="text-sm">
      L’action ne sera pas répétée automatiquement.
    </p>
    <div v-else-if="!complete" class="space-y-4">
      <p class="text-sm leading-relaxed">
        Connectez-vous au compte concerné avec votre adresse actuelle ou votre
        compte Google associé, puis rouvrez le lien reçu par email. Si votre
        inscription est incomplète, terminez-la d’abord.
      </p>
      <a
        :href="account.session.value ? accountDestination(account.session.value) : '/connexion'"
        class="inline-flex min-h-11 items-center text-sm font-semibold underline underline-offset-4"
        >Reprendre la connexion</a
      >
    </div>
    <p v-else-if="!token" class="text-sm leading-relaxed">
      Le lien n’est plus présent sur cette page. Rouvrez le dernier email reçu
      ou demandez un nouveau changement depuis votre compte.
    </p>
    <div
      v-else-if="loading || (!details && !detailsError)"
      role="status"
      class="space-y-5 motion-safe:animate-pulse"
    >
      <span class="sr-only">Chargement du compte…</span>
      <div class="h-12 rounded bg-ink/10" />
      <div class="h-12 rounded bg-ink/10" />
    </div>
    <div v-else-if="detailsError" class="space-y-4">
      <p
        role="alert"
        class="border-l-4 border-primary pl-3 text-sm text-primary"
      >
        {{ detailsError }}
      </p>
      <button type="button" class="button-primary min-h-11" @click="refresh">
        Réessayer
      </button>
    </div>
    <p v-else-if="!details?.pending_email" class="text-sm leading-relaxed">
      Aucun changement d’email n’est en attente. Ce lien a peut-être déjà été
      utilisé ou annulé. Consultez votre compte avant de demander un nouveau
      lien.
    </p>
    <div
      v-else-if="!details.has_password || !details.allowed_methods.includes('password')"
      class="space-y-5 text-sm leading-relaxed"
    >
      <p>
        Confirmez votre identité avec Google puis le lien envoyé à votre email
        actuel. Après cette vérification, choisissez « Confirmer le nouvel email
        » et collez le lien original reçu à
        <strong class="break-words">{{ details.pending_email }}</strong>.
      </p>
      <p>
        Ce lien ne sera pas conservé pendant la redirection. Gardez l’email
        original pour pouvoir copier son lien ensuite. Votre adresse actuelle
        reste inchangée jusqu’à confirmation.
      </p>
      <button
        type="button"
        class="button-primary min-h-12 w-full"
        :disabled="busy"
        @click="googleProof"
      >
        Confirmer mon identité avec Google
      </button>
    </div>
    <form v-else class="space-y-5" :aria-busy="busy" @submit.prevent="confirm">
      <dl class="space-y-3 text-sm">
        <div>
          <dt class="font-bold">Email actuel du compte</dt>
          <dd class="mt-1 break-words">{{ details.email }}</dd>
        </div>
        <div>
          <dt class="font-bold">Nouvel email à confirmer</dt>
          <dd class="mt-1 break-words">{{ details.pending_email }}</dd>
        </div>
      </dl>
      <AccountPasswordField
        id="confirm-email-password"
        v-model="password"
        label="Mot de passe actuel"
        :disabled="busy"
      />
      <p class="text-sm leading-relaxed">
        La confirmation déconnectera tous vos appareils. Votre ancienne adresse
        ne permettra plus de vous connecter avec un mot de passe. Votre compte
        Google associé ne change pas.
      </p>
      <button
        type="submit"
        class="button-primary min-h-12 w-full"
        :disabled="busy"
      >
        {{ busy ? 'Confirmation…' : 'Confirmer mon nouvel email' }}
      </button>
    </form>
    <p
      v-if="errorMessage"
      role="alert"
      class="mt-5 border-l-4 border-primary pl-3 text-sm text-primary"
    >
      {{ errorMessage }}
    </p>
    <a
      :href="done || uncertain ? '/connexion' : '/compte'"
      class="mt-6 inline-flex min-h-11 items-center text-sm font-semibold underline underline-offset-4"
      >{{
        done || uncertain ? 'Se connecter' : 'Revenir à mon compte'
      }}</a
    >
  </AccountShell>
</template>
