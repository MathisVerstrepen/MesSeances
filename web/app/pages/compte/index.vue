<script setup lang="ts">
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
const blocked = computed(
  () => account.writesBlocked.value || loading.value || !details.value,
)
const passwordError = ref('')
const emailError = ref('')
const sessionError = ref('')
const googleError = ref('')
const deletionError = ref('')
const notice = ref('')
type Editor = 'email' | 'password' | 'google' | 'delete'
const editor = ref<Editor | null>(null)
let restoreFocus: Editor | null = null
const inputs = {
  email: 'new-email',
  password: 'current-password',
  google: 'google-password',
  delete: 'deletion-password',
} satisfies Record<Editor, string>

function clearEditor() {
  clearSecrets()
  email.value = ''
  passwordError.value = ''
  emailError.value = ''
  googleError.value = ''
  deletionError.value = ''
}

async function toggleEditor(next: Editor) {
  if (busy.value) return
  restoreFocus = null
  const opening = editor.value !== next
  clearEditor()
  notice.value = ''
  editor.value = opening ? next : null
  await nextTick()
  const target = opening
    ? document.getElementById(inputs[next]) ||
      document.querySelector<HTMLElement>(`#editor-${next} button`)
    : document.getElementById(`trigger-${next}`)
  target?.focus()
}

const complete = computed(() => account.session.value?.state === 'complete')
const passwordAvailable = computed(
  () =>
    details.value?.has_password &&
    details.value.allowed_methods.includes('password'),
)

// Ordinary focus keeps this identity present; destructive refresh clears drafts.
let identity = ''
watch(
  account.session,
  (session) => {
    const next =
      session?.state === 'complete' && session.account
        ? `${session.account.username}:${session.account.email}`
        : ''
    if (!next || (identity && next !== identity)) {
      clearEditor()
      editor.value = null
      // A refresh may supersede the successful action's own refresh. Only its
      // non-secret focus target survives loading, never its editor or drafts.
      if (session) restoreFocus = null
    }
    if (session) identity = next
  },
  { immediate: true, flush: 'sync' },
)

watch(
  account.status,
  (status) => {
    if (status === 'idle' || status === 'error') {
      identity = ''
      restoreFocus = null
    }
  },
  { flush: 'sync' },
)

watch(
  details,
  (value) => {
    // Capture before the overview unmounts during overlapping session rechecks.
    if (import.meta.client && !value) {
      const target = document.activeElement?.id.replace('trigger-', '')
      if (
        target === 'email' ||
        target === 'password' ||
        target === 'google' ||
        target === 'delete'
      )
        restoreFocus = target
    }
  },
  { flush: 'sync' },
)

watch(
  [details, busy, blocked],
  ([value, pending, blocked]) => {
    if (!import.meta.client || !value || pending || blocked || !restoreFocus)
      return
    const button = document.getElementById(`trigger-${restoreFocus}`)
    if (!(button instanceof HTMLButtonElement) || button.disabled) return
    button.focus()
    if (document.activeElement === button) restoreFocus = null
  },
  // A sync watcher can run before Vue even queues the overview's DOM update.
  { flush: 'post' },
)

async function changed(message: string, close = true) {
  clearSecrets()
  if (close) {
    restoreFocus = editor.value
    editor.value = null
    clearEditor()
  }
  notice.value = message
  account.notify()
  await account.refresh()
}

async function recover() {
  await changed(
    'La réponse a été interrompue. L’état du compte est vérifié sans répéter l’action. Pour un mot de passe, vérifiez la connexion avant toute nouvelle modification.',
    false,
  )
}

async function changePassword() {
  if (blocked.value || busy.value || !passwordAvailable.value) return
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
  if (blocked.value || busy.value) return
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

async function googleProof(action: 'password_add' | 'delete_account') {
  if (blocked.value || busy.value) return
  busy.value = action
  const errorMessage = action === 'password_add' ? passwordError : deletionError
  errorMessage.value = ''
  notice.value = ''
  clearSecrets()
  try {
    await startGoogle({ mode: 'reauth', action })
  } catch (error) {
    errorMessage.value = accountErrorMessage(error)
  } finally {
    busy.value = ''
  }
}

async function changeGoogle() {
  if (blocked.value || busy.value || !passwordAvailable.value || !details.value)
    return
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
  if (blocked.value || busy.value || !passwordAvailable.value) return
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
  if (blocked.value || busy.value) return
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
  if (blocked.value || busy.value) return
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

async function logout() {
  if (blocked.value || busy.value) return
  busy.value = 'logout'
  sessionError.value = ''
  clearEditor()
  editor.value = null
  try {
    await account.logout()
    await navigateTo('/connexion', { external: true })
  } catch (error) {
    sessionError.value = accountErrorMessage(error)
  } finally {
    busy.value = ''
  }
}
useHead({ title: 'Mon compte - MesSeances' })
</script>

<template>
  <AccountShell
    title="Paramètres"
    hide-explore
    hide-logout
    account-area
    class="account-overview"
  >
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
      class="account-overview-sections space-y-6"
      :aria-busy="!!busy || blocked"
    >
      <section aria-labelledby="account-identity" class="space-y-4">
        <h2 id="account-identity" class="account-heading">Identité</h2>
        <div class="min-w-0 space-y-5">
          <dl class="space-y-2 text-sm">
            <div>
              <dt class="overview-label">Nom d’utilisateur</dt>
              <dd class="mt-1 break-words">{{ details.username }}</dd>
              <dd class="mt-1 text-ink/70">Nom d’utilisateur définitif.</dd>
            </div>
            <div class="overview-row">
              <dt class="overview-label">Email du compte</dt>
              <dd class="overview-row-action">
                <button
                  id="trigger-email"
                  type="button"
                  class="account-link overview-link"
                  :disabled="!!busy"
                  :aria-expanded="editor === 'email'"
                  aria-controls="editor-email"
                  aria-label="Modifier mon email"
                  @click="toggleEditor('email')"
                >
                  Modifier
                </button>
              </dd>
              <dd class="overview-row-value break-words">
                {{ details.email }}
              </dd>
            </div>
          </dl>
          <div
            v-if="details.pending_email"
            class="space-y-2 border-l-2 border-ink pl-4 text-sm"
          >
            <p class="break-words">
              Confirmation en attente :
              <strong>{{ details.pending_email }}</strong>
            </p>
            <p>
              Ouvrez le dernier lien reçu à cette adresse. Votre email actuel
              reste actif.
            </p>
            <button
              type="button"
              class="account-link overview-link"
              :disabled="!!busy || blocked"
              @click="cancelEmail"
            >
              {{
                busy === 'cancel' ? 'Annulation…' : 'Annuler le changement d’email'
              }}
            </button>
          </div>
          <form
            v-if="editor === 'email'"
            id="editor-email"
            class="space-y-4"
            @submit.prevent="requestEmail"
          >
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
              Confirmez votre identité avec Google puis un lien envoyé à votre
              email actuel.
            </p>
            <div class="overview-actions">
              <button
                type="submit"
                class="account-primary"
                :disabled="!!busy || blocked"
              >
                {{
                  busy === 'email' ? 'Demande en cours…' : passwordAvailable ? 'Recevoir le lien de confirmation' : 'Continuer avec Google'
                }}
              </button>
              <button
                type="button"
                class="account-link"
                :disabled="!!busy"
                @click="toggleEditor('email')"
              >
                Annuler
              </button>
            </div>
          </form>
          <p v-if="emailError" role="alert" class="account-alert">
            {{ emailError }}
          </p>
        </div>
      </section>

      <section aria-labelledby="account-connection" class="space-y-4">
        <h2 id="account-connection" class="account-heading">Connexion</h2>
        <div class="min-w-0 space-y-5">
          <div class="overview-row">
            <h3 id="account-password" class="overview-label">Mot de passe</h3>
            <button
              id="trigger-password"
              type="button"
              class="account-link overview-link overview-row-action"
              :disabled="!!busy"
              :aria-expanded="editor === 'password'"
              aria-controls="editor-password"
              :aria-label="passwordAvailable ? 'Modifier mon mot de passe' : 'Ajouter un mot de passe'"
              @click="toggleEditor('password')"
            >
              {{ passwordAvailable ? 'Modifier' : 'Ajouter' }}
            </button>
            <p class="overview-row-value text-sm text-ink/70">
              {{ passwordAvailable ? 'Défini' : 'Non défini' }}
            </p>
          </div>
          <p v-if="!passwordAvailable" class="text-sm leading-relaxed">
            Google est votre seul moyen de connexion. Ajoutez un mot de passe
            avant de le dissocier.
          </p>
          <div
            v-if="editor === 'password'"
            id="editor-password"
            class="space-y-4"
          >
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
              <button
                type="submit"
                class="account-primary"
                :disabled="!!busy || blocked"
              >
                {{
                  busy === 'password' ? 'Modification…' : 'Enregistrer le mot de passe'
                }}
              </button>
            </form>
            <div v-else class="space-y-4">
              <p class="text-sm leading-relaxed">
                Confirmez votre identité avec Google puis un lien envoyé à
                l’email actuel du compte. Vous choisirez ensuite votre mot de
                passe.
              </p>
              <button
                type="button"
                class="account-primary"
                :disabled="!!busy || blocked"
                @click="googleProof('password_add')"
              >
                Continuer avec Google
              </button>
            </div>
            <p v-if="passwordError" role="alert" class="account-alert">
              {{ passwordError }}
            </p>
            <button
              type="button"
              class="account-link"
              :disabled="!!busy"
              @click="toggleEditor('password')"
            >
              Annuler
            </button>
          </div>
          <div class="overview-row border-t border-ink/20 pt-5">
            <h3 id="account-google" class="overview-label">
              <GoogleIcon class="mr-2 inline-block align-text-bottom" />
              Google
            </h3>
            <button
              v-if="passwordAvailable"
              id="trigger-google"
              type="button"
              class="account-link overview-link overview-row-action"
              :disabled="!!busy"
              :aria-expanded="editor === 'google'"
              aria-controls="editor-google"
              :aria-label="details.google_linked ? 'Dissocier Google' : 'Associer Google'"
              @click="toggleEditor('google')"
            >
              {{ details.google_linked ? 'Dissocier' : 'Associer' }}
            </button>
            <div class="overview-row-value">
              <p class="text-sm text-ink/70">
                {{ details.google_linked ? 'Associé' : 'Non associé' }}
              </p>
              <p v-if="details.google_linked" class="mt-1 break-words text-sm">
                <span class="sr-only">Email communiqué par Google : </span>
                {{ details.google_email || 'Email non communiqué' }}
              </p>
            </div>
          </div>
          <form
            v-if="editor === 'google' && passwordAvailable"
            id="editor-google"
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
              Vos autres sessions seront fermées après modification.
            </p>
            <button
              type="submit"
              class="account-primary"
              :disabled="!!busy || blocked"
            >
              {{
                details.google_linked ? 'Dissocier Google' : 'Associer un compte Google'
              }}
            </button>
            <p v-if="googleError" role="alert" class="account-alert">
              {{ googleError }}
            </p>
            <button
              type="button"
              class="account-link ml-4"
              :disabled="!!busy"
              @click="toggleEditor('google')"
            >
              Annuler
            </button>
          </form>
        </div>
      </section>
      <section aria-labelledby="account-sessions" class="space-y-4">
        <h2 id="account-sessions" class="account-heading">Sessions</h2>
        <div class="min-w-0 space-y-3">
          <div class="overview-actions overview-session-actions">
            <button
              type="button"
              class="account-secondary overview-secondary"
              :disabled="!!busy || blocked"
              @click="logout"
            >
              {{ busy === 'logout' ? 'Déconnexion…' : 'Se déconnecter' }}
            </button>
            <div class="min-w-0 space-y-2">
              <button
                type="button"
                class="account-secondary overview-secondary"
                :disabled="!!busy || blocked"
                aria-describedby="logout-all-consequence"
                @click="logoutAll"
              >
                {{
                  busy === 'sessions' ? 'Déconnexion…' : 'Déconnecter tous les appareils'
                }}
              </button>
              <p id="logout-all-consequence" class="text-sm leading-relaxed">
                Vous serez aussi déconnecté de cet appareil.
              </p>
            </div>
          </div>
          <p v-if="sessionError" role="alert" class="account-alert">
            {{ sessionError }}
          </p>
        </div>
      </section>
      <section aria-labelledby="account-delete" class="space-y-5">
        <h2 id="account-delete" class="account-heading">Suppression</h2>
        <div class="min-w-0 space-y-4">
          <button
            id="trigger-delete"
            type="button"
            class="account-link overview-link text-primary"
            :disabled="!!busy"
            :aria-expanded="editor === 'delete'"
            aria-controls="editor-delete"
            @click="toggleEditor('delete')"
          >
            Supprimer mon compte
          </button>
          <div v-if="editor === 'delete'" id="editor-delete" class="space-y-5">
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
                :disabled="!!busy || blocked"
              >
                {{
                  busy === 'delete' ? 'Suppression…' : 'Supprimer définitivement mon compte'
                }}
              </button>
            </form>
            <div v-else class="space-y-4">
              <p class="text-sm leading-relaxed">
                Confirmez d’abord votre identité avec Google et un lien envoyé à
                votre email actuel. La suppression devra ensuite être confirmée
                en saisissant SUPPRIMER.
              </p>
              <button
                type="button"
                class="account-danger w-full"
                :disabled="!!busy || blocked"
                @click="googleProof('delete_account')"
              >
                Vérifier mon identité avant suppression
              </button>
            </div>
            <p v-if="deletionError" role="alert" class="account-alert">
              {{ deletionError }}
            </p>
            <button
              type="button"
              class="account-link"
              :disabled="!!busy"
              @click="toggleEditor('delete')"
            >
              Annuler
            </button>
          </div>
        </div>
      </section>
    </div>
  </AccountShell>
</template>

<style scoped>
@reference "../../assets/css/main.css";

.account-overview :deep(h1) {
  @apply mb-6 pb-5 text-[2rem] sm:text-[2.75rem];
}
.account-overview-sections > section {
  @apply min-w-0 border-t-2 border-ink pt-6 md:grid md:grid-cols-[10rem_minmax(0,1fr)] md:items-baseline md:gap-x-8 md:space-y-0;
}
.account-overview-sections > section:first-child {
  @apply border-t-0 pt-0;
}
.overview-row {
  @apply grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-x-4;
}
.account-overview-sections [id^="editor-"] {
  @apply max-w-lg;
}
.overview-label {
  @apply min-w-0 text-sm font-semibold leading-relaxed;
}
.overview-row-action {
  @apply col-start-2 row-start-1;
}
.overview-row-value {
  @apply col-span-2 min-w-0;
}
.overview-actions {
  @apply flex flex-wrap items-center gap-x-5 gap-y-2;
}
.overview-session-actions {
  @apply items-start;
}
</style>
