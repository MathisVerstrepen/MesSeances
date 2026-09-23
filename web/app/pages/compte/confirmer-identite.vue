<script setup lang="ts">
import type { AccountContinuation } from '~/types/account'
import {
  AccountApiError,
  accountErrorMessage,
  accountWriteUncertain,
  passwordCriteria,
} from '~/utils/accountState'
import {
  accountEmailConfirmationToken,
  validAccountContinuation,
} from '~/utils/accountGoogle'

definePageMeta({ middleware: 'account-auth' })
const api = useAccountApi()
const account = useAccountSession()
const { token, ready, clear: clearToken } = useAccountToken()
const continuation = ref<AccountContinuation | null>(null)
const loading = ref(false)
const busy = ref('')
const errorMessage = ref('')
const notice = ref('')
const grant = ref('')
const password = ref('')
const confirmation = ref('')
const originalLink = ref('')
// Both operations use email_change server-side; the user selects locally after OAuth.
const emailOperation = ref<'request' | 'confirm' | ''>('')
const done = ref(false)
const uncertain = ref(false)
const restartRequired = ref(false)
const cooldown = ref(0)
const now = ref(Date.now())
let grantDeadline = 0
let cooldownDeadline = 0
let revision = 0
let timer: ReturnType<typeof setInterval> | undefined
const clearSecrets = useAccountSecrets(
  grant,
  password,
  confirmation,
  originalLink,
)
const complete = computed(() => account.session.value?.state === 'complete')
const expired = computed(
  () =>
    continuation.value &&
    Date.parse(continuation.value.expires_at) <= now.value,
)
const supported = computed(
  () =>
    continuation.value &&
    ['password_add', 'email_change', 'delete_account'].includes(
      continuation.value.action,
    ),
)
const actionLabel = computed(
  () =>
    ({
      password_add: 'Ajouter un mot de passe',
      email_change: 'Changer d’email',
      delete_account: 'Supprimer mon compte',
      password_change: '',
      google_link: '',
      google_unlink: '',
    })[continuation.value?.action ?? 'password_change'],
)
const initialIdentity = JSON.stringify(account.session.value?.account)

function invalidate() {
  revision++
  clearSecrets()
  if (!done.value && !uncertain.value) notice.value = ''
  continuation.value = null
  emailOperation.value = ''
}
// Every recheck rejects pending proof. An already issued single-use proof keeps
// its original deadline, never gains authority from a matching session DTO.
// Server session/revision/target validation remains mandatory on every write.
watch(
  account.revision,
  () => {
    revision++
  },
  { flush: 'sync' },
)
watch(
  account.session,
  (value) => {
    revision++
    if (
      !value ||
      value.state !== 'complete' ||
      JSON.stringify(value.account) !== initialIdentity
    )
      invalidate()
  },
  { flush: 'sync' },
)
watch(account.status, (value) => {
  if (value === 'error') invalidate()
})
watch(emailOperation, () => {
  originalLink.value = ''
})

async function load() {
  if (
    account.writesBlocked.value ||
    !complete.value ||
    loading.value ||
    busy.value
  )
    return
  const current = revision
  loading.value = true
  errorMessage.value = ''
  try {
    const value = await api.continuation()
    if (current !== revision) return
    if (!validAccountContinuation(value))
      throw new AccountApiError(403, 'recent_auth_required')
    continuation.value = value
  } catch (error) {
    if (current === revision) errorMessage.value = accountErrorMessage(error)
  } finally {
    loading.value = false
  }
}

async function requestChallenge() {
  if (
    account.writesBlocked.value ||
    busy.value ||
    restartRequired.value ||
    !complete.value ||
    !continuation.value ||
    expired.value ||
    cooldown.value > 0
  )
    return
  busy.value = 'request'
  errorMessage.value = ''
  notice.value = ''
  try {
    await api.requestIdentityEmail()
    clearToken()
    cooldownDeadline = Date.now() + 60000
    cooldown.value = 60
    notice.value =
      'La demande a été acceptée. Ouvrez le dernier lien reçu à l’email actuel du compte, dans ce navigateur. Seul ce lien restera valable.'
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
    if (error instanceof AccountApiError && error.status === 429) {
      cooldown.value = error.retryAfter || 60
      cooldownDeadline = Date.now() + cooldown.value * 1000
    }
  } finally {
    busy.value = ''
  }
}

async function confirmChallenge() {
  if (
    account.writesBlocked.value ||
    busy.value ||
    restartRequired.value ||
    !complete.value ||
    !token.value ||
    !continuation.value ||
    expired.value
  )
    return
  busy.value = 'proof'
  errorMessage.value = ''
  notice.value = ''
  const current = revision
  const scope = continuation.value
  try {
    const proof = await api.confirmIdentityEmail(
      token.value,
      scope.action,
      scope.target ?? undefined,
    )
    try {
      if (current !== revision) return
      grant.value = proof.grant
      grantDeadline = Math.min(
        Date.now() + 300000,
        Date.parse(scope.expires_at),
      )
      clearToken()
      notice.value =
        'Identité confirmée. Choisissez et confirmez maintenant l’action à effectuer.'
    } finally {
      proof.grant = ''
    }
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
    if (accountWriteUncertain(error)) {
      restartRequired.value = true
      clearToken()
      errorMessage.value =
        'La réponse de vérification a été interrompue. Recommencez depuis votre compte ; aucune modification ne sera répétée automatiquement.'
    }
  } finally {
    busy.value = ''
  }
}

async function applyAction() {
  if (
    account.writesBlocked.value ||
    busy.value ||
    restartRequired.value ||
    !complete.value ||
    !grant.value ||
    !continuation.value ||
    expired.value ||
    uncertain.value
  )
    return
  errorMessage.value = ''
  notice.value = ''
  const scope = continuation.value
  if (scope.action === 'password_add') {
    const criteria = passwordCriteria(password.value)
    if (!criteria.minimum || !criteria.maximum) {
      errorMessage.value = 'Choisissez un mot de passe de 10 à 128 caractères.'
      return
    }
  }
  if (scope.action === 'delete_account' && confirmation.value !== 'SUPPRIMER') {
    errorMessage.value =
      'Saisissez exactement SUPPRIMER pour confirmer la suppression définitive.'
    return
  }
  let emailToken = ''
  if (scope.action === 'email_change') {
    if (!emailOperation.value) {
      errorMessage.value =
        'Choisissez entre demander le lien et confirmer le nouvel email.'
      return
    }
    if (emailOperation.value === 'confirm') {
      emailToken = accountEmailConfirmationToken(
        originalLink.value,
        window.location.origin,
      )
      if (!emailToken) {
        errorMessage.value =
          'Ce lien n’est pas valide. Copiez le lien original de confirmation du nouvel email sur ce site, ou son jeton, sans le modifier.'
        return
      }
    }
  }
  if (Date.now() >= grantDeadline) {
    restartRequired.value = true
    clearSecrets()
    errorMessage.value = 'La preuve a expiré. Recommencez depuis votre compte.'
    return
  }
  busy.value = 'action'
  try {
    if (scope.action === 'password_add') {
      await api.changePassword(password.value, grant.value)
      notice.value =
        'Votre mot de passe a été ajouté. Vos autres sessions ont été fermées.'
    } else if (scope.action === 'delete_account') {
      await api.deleteAccount(grant.value, 'SUPPRIMER')
      notice.value =
        'Votre compte a été supprimé définitivement. Votre nom d’utilisateur reste réservé.'
    } else if (scope.action === 'email_change' && scope.target) {
      if (emailOperation.value === 'confirm') {
        await api.confirmEmailChange(emailToken, grant.value)
        notice.value =
          'Votre email a été modifié. Toutes vos sessions ont été fermées. Connectez-vous avec votre nouvelle adresse ou Google.'
      } else {
        await api.requestEmailChange(scope.target, grant.value)
        notice.value =
          'La demande a été acceptée. Ouvrez le lien envoyé au nouvel email. Votre adresse actuelle reste inchangée ; une nouvelle vérification d’identité sera nécessaire pour confirmer.'
      }
    } else throw new AccountApiError(403)
    done.value = true
    clearSecrets()
    account.notify()
    await account.refresh()
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
    // A grant may have been consumed even when the response was lost. Never replay.
    clearSecrets()
    if (accountWriteUncertain(error)) {
      uncertain.value = true
      errorMessage.value =
        'La réponse a été interrompue. L’action a peut-être abouti. Consultez votre compte ou reconnectez-vous avant toute nouvelle demande ; aucune action ne sera répétée automatiquement.'
      account.notify()
      await account.refresh()
    }
    restartRequired.value = true
  } finally {
    emailToken = ''
    busy.value = ''
  }
}

onMounted(() => {
  void load()
  window.addEventListener('pagehide', invalidate)
  timer = setInterval(() => {
    now.value = Date.now()
    cooldown.value = Math.max(
      0,
      Math.ceil((cooldownDeadline - now.value) / 1000),
    )
    if (grant.value && now.value >= grantDeadline) {
      restartRequired.value = true
      clearSecrets()
      notice.value = ''
      errorMessage.value =
        'La preuve a expiré. Recommencez depuis votre compte.'
    }
  }, 1000)
})
onBeforeUnmount(() => {
  invalidate()
  if (timer) clearInterval(timer)
  if (import.meta.client) window.removeEventListener('pagehide', invalidate)
})
useHead({ title: 'Confirmer mon identité - MesSeances' })
</script>

<template>
  <AccountShell title="Confirmer mon identité">
    <p v-if="notice" role="status" class="mb-5 text-sm leading-relaxed">
      {{ notice }}
    </p>
    <p v-if="done || uncertain" class="text-sm">
      Aucune action ne sera répétée depuis cette page.
    </p>
    <p v-else-if="restartRequired" class="text-sm">
      La preuve n’est plus utilisable. Recommencez la vérification depuis votre
      compte.
    </p>
    <p v-else-if="!complete" class="text-sm leading-relaxed">
      Connectez-vous au compte concerné, puis recommencez la vérification depuis
      ses réglages. L’accès à Google et à l’email actuel du compte est
      nécessaire.
    </p>
    <div
      v-else-if="loading || !ready"
      role="status"
      class="space-y-5 motion-safe:animate-pulse"
    >
      <span class="sr-only">Chargement de la vérification…</span>
      <div class="h-12 border-2 border-ink/20 bg-ink/10" />
      <div class="h-12 border-2 border-ink/20 bg-ink/10" />
    </div>
    <div v-else-if="!continuation" class="space-y-4">
      <p class="text-sm">
        Aucune vérification active n’est chargée. Recommencez depuis votre
        compte ou réessayez si la connexion a été interrompue.
      </p>
      <button type="button" class="account-secondary" @click="load">
        Réessayer le chargement
      </button>
    </div>
    <p v-else-if="expired" class="text-sm">
      Cette vérification a expiré. Recommencez depuis votre compte.
    </p>
    <p v-else-if="!supported" class="text-sm">
      Cette action nécessite un mot de passe ou retirerait votre dernier moyen
      de connexion. Revenez à votre compte pour gérer vos moyens de connexion.
    </p>
    <div
      v-else
      class="space-y-5"
      :aria-busy="!!busy || account.writesBlocked.value"
    >
      <h2 class="account-heading">{{ actionLabel }}</h2>
      <p v-if="continuation.target" class="break-words text-sm">
        Nouvel email : <strong>{{ continuation.target }}</strong>
      </p>
      <template v-if="!grant">
        <p class="text-sm leading-relaxed">
          Google a été confirmé. Vérifiez aussi l’accès à l’email actuel de
          votre compte. La vérification expire au plus tard dix minutes après
          son démarrage.
        </p>
        <form v-if="token" @submit.prevent="confirmChallenge">
          <button
            type="submit"
            class="account-primary w-full"
            :disabled="!!busy || account.writesBlocked.value"
          >
            {{
              busy === 'proof' ? 'Vérification…' : 'Confirmer mon identité avec ce lien'
            }}
          </button>
        </form>
        <button
          type="button"
          class="account-secondary w-full"
          :disabled="!!busy || account.writesBlocked.value || cooldown > 0"
          @click="requestChallenge"
        >
          {{
            cooldown > 0 ? `Renvoyer dans ${cooldown} s` : 'Recevoir le lien de vérification'
          }}
        </button>
      </template>
      <form v-else class="space-y-5" @submit.prevent="applyAction">
        <template v-if="continuation.action === 'password_add'">
          <AccountPasswordField
            id="add-password"
            v-model="password"
            new-password
            label="Nouveau mot de passe"
            :disabled="!!busy"
          />
          <p class="text-sm">
            Vos autres sessions seront fermées. Google restera associé.
          </p>
        </template>
        <template v-else-if="continuation.action === 'email_change'">
          <fieldset :disabled="!!busy" class="space-y-3">
            <legend class="account-label">Action sur le nouvel email</legend>
            <label class="account-choice"
              ><input
                v-model="emailOperation"
                type="radio"
                value="request"
                name="email-operation"
                required
              >Demander le lien au nouvel email</label
            >
            <label class="account-choice"
              ><input
                v-model="emailOperation"
                type="radio"
                value="confirm"
                name="email-operation"
                required
              >Confirmer le nouvel email avec le lien reçu</label
            >
          </fieldset>
          <div v-if="emailOperation === 'confirm'" class="space-y-3">
            <label for="original-email-link" class="account-label"
              >Lien original de confirmation du nouvel email, ou jeton</label
            >
            <input
              id="original-email-link"
              v-model="originalLink"
              type="text"
              autocomplete="off"
              autocapitalize="none"
              :spellcheck="false"
              required
              :disabled="!!busy"
              class="account-input w-full"
            >
            <p class="text-sm leading-relaxed">
              Copiez le lien de l’email reçu à la nouvelle adresse, pas celui de
              vérification d’identité. Il reste uniquement en mémoire sur cette
              page. La confirmation fermera toutes vos sessions ; Google restera
              associé.
            </p>
          </div>
          <p
            v-else-if="emailOperation === 'request'"
            class="text-sm leading-relaxed"
          >
            L’adresse actuelle restera active jusqu’à confirmation. Une nouvelle
            demande remplacera le lien précédent.
          </p>
        </template>
        <template v-else-if="continuation.action === 'delete_account'">
          <AccountDeletionWarning />
          <div>
            <label for="delete-confirmation" class="account-label"
              >Saisissez SUPPRIMER</label
            >
            <input
              id="delete-confirmation"
              v-model="confirmation"
              autocomplete="off"
              :spellcheck="false"
              required
              :disabled="!!busy"
              class="account-input account-input-danger w-full"
            >
          </div>
        </template>
        <button
          type="submit"
          class="w-full"
          :class="continuation.action === 'delete_account' ? 'account-danger' : 'account-primary'"
          :disabled="!!busy || account.writesBlocked.value"
        >
          {{
            busy === 'action' ? 'Action en cours…' : continuation.action === 'delete_account' ? 'Supprimer définitivement mon compte' : continuation.action === 'password_add' ? 'Ajouter mon mot de passe' : emailOperation === 'confirm' ? 'Confirmer mon nouvel email' : 'Demander le lien au nouvel email'
          }}
        </button>
      </form>
    </div>
    <p v-if="errorMessage" role="alert" class="account-alert mt-5">
      {{ errorMessage }}
    </p>
    <a :href="complete ? '/compte' : '/connexion'" class="account-link mt-6">{{
      complete ? 'Revenir à mon compte' : 'Se connecter'
    }}</a>
  </AccountShell>
</template>
