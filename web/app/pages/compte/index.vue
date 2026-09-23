<script setup lang="ts">
import type { AccountAction } from '~/types/account'
import {
  accountDestination,
  accountErrorMessage,
  accountWriteUncertain,
  normalizeAccountEmail,
  passwordCriteria,
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
const passwordAction = useAccountPasswordAction()
const startGoogle = useAccountGoogle()
const currentPassword = ref('')
const newPassword = ref('')
const emailPassword = ref('')
const email = ref('')
const googlePassword = ref('')
const deletionPassword = ref('')
const confirmation = ref('')
const clearSecrets = useAccountSecrets(
  currentPassword,
  newPassword,
  emailPassword,
  googlePassword,
  deletionPassword,
  confirmation,
)
const busy = ref('')
const passwordError = ref('')
const emailError = ref('')
const sessionError = ref('')
const googleError = ref('')
const deletionError = ref('')
const notice = ref('')
const complete = computed(() => account.session.value?.state === 'complete')
const passwordAvailable = computed(
  () =>
    details.value?.has_password &&
    details.value.allowed_methods.includes('password'),
)

watch(complete, (value) => {
  if (!value) clearSecrets()
})

async function changed(message: string) {
  clearSecrets()
  notice.value = message
  account.notify()
  await account.refresh()
}

async function recover() {
  await changed(
    'La réponse a été interrompue. L’état du compte est vérifié sans répéter l’action. Pour un mot de passe, vérifiez la connexion avant toute nouvelle modification.',
  )
}

async function changePassword() {
  if (busy.value || !passwordAvailable.value) return
  const criteria = passwordCriteria(newPassword.value)
  passwordError.value = ''
  notice.value = ''
  if (!criteria.minimum || !criteria.maximum) {
    passwordError.value = 'Choisissez un mot de passe de 10 à 128 caractères.'
    return
  }
  busy.value = 'password'
  // Capture the confirmed value before asynchronous proof; inputs are disabled.
  const password = newPassword.value
  try {
    const applied = await passwordAction(
      currentPassword.value,
      'password_change',
      undefined,
      (grant) => api.changePassword(password, grant),
    )
    if (applied)
      await changed(
        'Votre mot de passe a été modifié. Vos autres sessions ont été fermées.',
      )
    else
      passwordError.value =
        'La session a été revérifiée. Saisissez à nouveau votre mot de passe et recommencez.'
  } catch (error) {
    passwordError.value = accountErrorMessage(error)
    if (accountWriteUncertain(error)) await recover()
  } finally {
    busy.value = ''
  }
}

async function requestEmail() {
  if (busy.value) return
  emailError.value = ''
  notice.value = ''
  const target = normalizeAccountEmail(email.value)
  if (target === details.value?.email) {
    emailError.value =
      'Cette adresse est déjà celle de votre compte. Saisissez une autre adresse.'
    return
  }
  busy.value = 'email'
  try {
    if (!passwordAvailable.value) {
      clearSecrets()
      await startGoogle({ mode: 'reauth', action: 'email_change', target })
      return
    }
    const applied = await passwordAction(
      emailPassword.value,
      'email_change',
      target,
      (grant) => api.requestEmailChange(target, grant),
    )
    if (applied)
      await changed(
        'La demande de changement d’email a été acceptée. Ouvrez le lien envoyé à la nouvelle adresse pour confirmer ; votre email actuel reste inchangé.',
      )
    else
      emailError.value =
        'La session a été revérifiée. Saisissez à nouveau votre mot de passe et recommencez.'
  } catch (error) {
    emailError.value = accountErrorMessage(error)
    if (accountWriteUncertain(error)) await recover()
  } finally {
    busy.value = ''
  }
}

async function googleProof(action: AccountAction) {
  if (busy.value) return
  busy.value = action
  googleError.value = ''
  notice.value = ''
  clearSecrets()
  try {
    await startGoogle({ mode: 'reauth', action })
  } catch (error) {
    googleError.value = accountErrorMessage(error)
  } finally {
    busy.value = ''
  }
}

async function changeGoogle() {
  if (busy.value || !passwordAvailable.value || !details.value) return
  busy.value = 'google'
  googleError.value = ''
  notice.value = ''
  const unlink = details.value.google_linked
  try {
    const applied = await passwordAction(
      googlePassword.value,
      unlink ? 'google_unlink' : 'google_link',
      undefined,
      async (grant) => {
        if (unlink) await api.googleUnlink(grant)
        else {
          clearSecrets()
          await startGoogle({ mode: 'link', grant })
        }
      },
    )
    if (applied && unlink)
      await changed(
        'Google a été dissocié. Vos autres sessions ont été fermées.',
      )
    if (!applied)
      googleError.value =
        'La session a changé. Recommencez la confirmation d’identité.'
  } catch (error) {
    googleError.value = accountErrorMessage(error)
    if (accountWriteUncertain(error)) await recover()
  } finally {
    busy.value = ''
  }
}

async function deleteAccount() {
  if (busy.value || !passwordAvailable.value) return
  deletionError.value = ''
  notice.value = ''
  if (confirmation.value !== 'SUPPRIMER') {
    deletionError.value =
      'Saisissez exactement SUPPRIMER pour confirmer la suppression définitive.'
    return
  }
  busy.value = 'delete'
  try {
    const applied = await passwordAction(
      deletionPassword.value,
      'delete_account',
      undefined,
      (grant) => api.deleteAccount(grant, 'SUPPRIMER'),
    )
    if (applied) {
      account.clear()
      await changed(
        'Votre compte a été supprimé définitivement. Votre nom d’utilisateur reste réservé.',
      )
    } else
      deletionError.value =
        'La session a changé. Recommencez la confirmation d’identité.'
  } catch (error) {
    deletionError.value = accountErrorMessage(error)
    if (accountWriteUncertain(error)) await recover()
  } finally {
    busy.value = ''
  }
}

async function cancelEmail() {
  if (busy.value) return
  busy.value = 'cancel'
  emailError.value = ''
  notice.value = ''
  try {
    await api.cancelEmailChange()
    await changed(
      'Le changement d’email a été annulé. Le lien de confirmation ne peut plus être utilisé.',
    )
  } catch (error) {
    emailError.value = accountErrorMessage(error)
    if (accountWriteUncertain(error)) await recover()
  } finally {
    busy.value = ''
  }
}

async function logoutAll() {
  if (busy.value) return
  busy.value = 'sessions'
  sessionError.value = ''
  notice.value = ''
  try {
    await api.logoutAll()
    await changed('Toutes vos sessions ont été fermées, y compris celle-ci.')
  } catch (error) {
    sessionError.value = accountErrorMessage(error)
    if (accountWriteUncertain(error)) await recover()
  } finally {
    busy.value = ''
  }
}
useHead({ title: 'Mon compte - MesSeances' })
</script>

<template>
  <AccountShell title="Mon compte" hide-explore class="account-shell-wide">
    <p v-if="notice" role="status" class="mb-6 text-sm leading-relaxed">
      {{ notice }}
    </p>
    <div v-if="!complete" class="space-y-4">
      <p class="text-sm">
        Votre session n’est plus active ou votre inscription reste à terminer.
      </p>
      <a
        :href="account.session.value ? accountDestination(account.session.value) : '/connexion'"
        class="account-link"
        >Reprendre la connexion</a
      >
    </div>
    <div
      v-else-if="loading || (!details && !detailsError)"
      role="status"
      class="space-y-5 motion-safe:animate-pulse"
    >
      <span class="sr-only">Chargement du compte…</span>
      <div class="h-12 border-2 border-ink/20 bg-ink/10" />
      <div class="h-12 border-2 border-ink/20 bg-ink/10" />
    </div>
    <div v-else-if="detailsError" class="space-y-4">
      <p role="alert" class="account-alert">
        {{ detailsError }}
      </p>
      <button type="button" class="account-primary" @click="refresh">
        Réessayer
      </button>
    </div>
    <div
      v-else-if="details"
      class="account-settings space-y-8"
      :aria-busy="!!busy"
    >
      <section aria-labelledby="account-identity" class="space-y-4">
        <h2 id="account-identity" class="account-heading">Identité</h2>
        <dl class="space-y-3 text-sm">
          <div>
            <dt class="account-label">Nom d’utilisateur</dt>
            <dd class="mt-1 break-words">{{ details.username }}</dd>
          </div>
          <div>
            <dt class="account-label">Email du compte</dt>
            <dd class="mt-1 break-words">{{ details.email }}</dd>
          </div>
          <div v-if="details.google_linked">
            <dt class="account-label">Email communiqué par Google</dt>
            <dd class="mt-1 break-words">
              {{ details.google_email || 'Non communiqué' }}
            </dd>
          </div>
        </dl>
        <p class="text-sm">Le nom d’utilisateur est définitif.</p>
      </section>

      <section aria-labelledby="account-password" class="space-y-5">
        <h2 id="account-password" class="account-heading">Mot de passe</h2>
        <form
          v-if="passwordAvailable"
          class="space-y-5"
          @submit.prevent="changePassword"
        >
          <AccountPasswordField
            id="current-password"
            v-model="currentPassword"
            label="Mot de passe actuel"
            :disabled="!!busy"
          />
          <AccountPasswordField
            id="new-password"
            v-model="newPassword"
            label="Nouveau mot de passe"
            new-password
            :disabled="!!busy"
          />
          <p class="text-sm">Vos autres appareils seront déconnectés.</p>
          <p v-if="passwordError" role="alert" class="account-alert">
            {{ passwordError }}
          </p>
          <button
            type="submit"
            class="account-primary w-full"
            :disabled="!!busy"
          >
            {{
              busy === 'password' ? 'Modification…' : 'Modifier mon mot de passe'
            }}
          </button>
        </form>
        <div v-else class="space-y-4">
          <p class="text-sm leading-relaxed">
            Confirmez votre identité avec Google puis un lien envoyé à l’email
            actuel du compte. Vous choisirez ensuite votre mot de passe.
          </p>
          <button
            type="button"
            class="account-primary w-full"
            :disabled="!!busy"
            @click="googleProof('password_add')"
          >
            Ajouter un mot de passe avec Google
          </button>
        </div>
      </section>

      <section aria-labelledby="account-email" class="space-y-5">
        <h2 id="account-email" class="account-heading">Changer d’email</h2>
        <div v-if="details.pending_email" class="space-y-3 text-sm">
          <p class="break-words">
            Confirmation en attente :
            <strong>{{ details.pending_email }}</strong>
          </p>
          <p>
            Ouvrez le dernier lien reçu à cette adresse. Une nouvelle
            confirmation d’identité sera demandée.
          </p>
          <button
            type="button"
            class="account-secondary"
            :disabled="!!busy"
            @click="cancelEmail"
          >
            {{
              busy === 'cancel' ? 'Annulation…' : 'Annuler le changement d’email'
            }}
          </button>
        </div>
        <form class="space-y-5" @submit.prevent="requestEmail">
          <div>
            <label for="new-email" class="account-label">Nouvel email</label>
            <input
              id="new-email"
              v-model="email"
              type="email"
              inputmode="email"
              autocomplete="email"
              autocapitalize="none"
              :spellcheck="false"
              maxlength="254"
              required
              :disabled="!!busy"
              class="account-input w-full"
            >
          </div>
          <AccountPasswordField
            v-if="passwordAvailable"
            id="email-password"
            v-model="emailPassword"
            label="Mot de passe actuel"
            :disabled="!!busy"
          />
          <p class="text-sm leading-relaxed">
            Votre adresse actuelle reste active jusqu’à confirmation. Une
            nouvelle demande remplace le lien précédent.
          </p>
          <p v-if="!passwordAvailable" class="text-sm leading-relaxed">
            Google puis un lien envoyé à votre email actuel seront nécessaires.
            Aucun secret saisi ne sera conservé pendant la redirection.
          </p>
          <button
            type="submit"
            class="account-primary w-full"
            :disabled="!!busy"
          >
            {{
              busy === 'email' ? 'Demande en cours…' : passwordAvailable ? 'Recevoir le lien de confirmation' : 'Confirmer mon identité avec Google'
            }}
          </button>
        </form>
        <p v-if="emailError" role="alert" class="account-alert">
          {{ emailError }}
        </p>
      </section>

      <section aria-labelledby="account-google" class="space-y-5">
        <h2 id="account-google" class="account-heading">Google</h2>
        <p class="text-sm">
          {{
            details.google_linked ? 'Votre compte Google est associé.' : 'Aucun compte Google associé.'
          }}
        </p>
        <form
          v-if="passwordAvailable"
          class="space-y-4"
          @submit.prevent="changeGoogle"
        >
          <AccountPasswordField
            id="google-password"
            v-model="googlePassword"
            label="Mot de passe actuel"
            :disabled="!!busy"
          />
          <p class="text-sm leading-relaxed">
            Vos autres sessions seront fermées après modification. L’association
            ouvre Google et efface les secrets saisis sur cette page.
          </p>
          <button
            type="submit"
            class="account-secondary w-full"
            :disabled="!!busy"
          >
            {{
              details.google_linked ? 'Dissocier Google' : 'Associer un compte Google'
            }}
          </button>
        </form>
        <p v-else class="text-sm leading-relaxed">
          Google est votre seul moyen de connexion. Ajoutez un mot de passe
          avant de le dissocier.
        </p>
        <p v-if="googleError" role="alert" class="account-alert">
          {{ googleError }}
        </p>
      </section>
      <section aria-labelledby="account-delete" class="space-y-5">
        <h2 id="account-delete" class="account-heading text-primary">
          Supprimer mon compte
        </h2>
        <AccountDeletionWarning />
        <form
          v-if="passwordAvailable"
          class="space-y-5"
          @submit.prevent="deleteAccount"
        >
          <AccountPasswordField
            id="deletion-password"
            v-model="deletionPassword"
            label="Mot de passe actuel"
            :disabled="!!busy"
          />
          <div>
            <label for="deletion-confirmation" class="account-label"
              >Saisissez SUPPRIMER</label
            >
            <input
              id="deletion-confirmation"
              v-model="confirmation"
              autocomplete="off"
              :spellcheck="false"
              required
              :disabled="!!busy"
              class="account-input account-input-danger w-full"
            >
          </div>
          <button
            type="submit"
            class="account-danger w-full"
            :disabled="!!busy"
          >
            {{
              busy === 'delete' ? 'Suppression…' : 'Supprimer définitivement mon compte'
            }}
          </button>
        </form>
        <div v-else class="space-y-4">
          <p class="text-sm leading-relaxed">
            Confirmez d’abord votre identité avec Google et un lien envoyé à
            votre email actuel. La suppression devra ensuite être confirmée en
            saisissant SUPPRIMER.
          </p>
          <button
            type="button"
            class="account-danger w-full"
            :disabled="!!busy"
            @click="googleProof('delete_account')"
          >
            Vérifier mon identité avant suppression
          </button>
        </div>
        <p v-if="deletionError" role="alert" class="account-alert">
          {{ deletionError }}
        </p>
      </section>

      <section aria-labelledby="account-sessions" class="space-y-5">
        <h2 id="account-sessions" class="account-heading">Sessions</h2>
        <p class="text-sm">
          Fermez toutes vos sessions, y compris celle-ci. Vos cinémas
          sélectionnés restent enregistrés sur cet appareil.
        </p>
        <button
          type="button"
          class="account-secondary w-full"
          :disabled="!!busy"
          @click="logoutAll"
        >
          {{
            busy === 'sessions' ? 'Déconnexion…' : 'Se déconnecter de tous les appareils'
          }}
        </button>
      </section>
    </div>
    <p v-if="sessionError" role="alert" class="account-alert mt-4">
      {{ sessionError }}
    </p>
  </AccountShell>
</template>
