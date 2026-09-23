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

// refresh() temporarily clears the session. Do not treat that loading gap as
// revocation or discard a draft whenever the window receives focus.
let identity = ''
watch(
  [account.session, account.status],
  ([session, status]) => {
    if (status === 'loading') return
    const next =
      session?.state === 'complete' && session.account
        ? JSON.stringify(session.account)
        : ''
    if (!next || (identity && next !== identity)) {
      clearEditor()
      editor.value = null
      restoreFocus = null
    }
    identity = next
  },
  { immediate: true },
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
  [details, busy],
  ([value, pending]) => {
    if (!import.meta.client || !value || pending || !restoreFocus) return
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

async function googleProof(action: 'password_add' | 'delete_account') {
  if (busy.value) return
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

async function logout() {
  if (busy.value) return
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
    title="Mon compte"
    hide-explore
    hide-logout
    class="account-shell-wide account-overview"
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
      :aria-busy="!!busy"
    >
      <section aria-labelledby="account-identity" class="space-y-4">
        <h2 id="account-identity" class="account-heading">Identité</h2>
        <div class="min-w-0 space-y-5">
          <dl class="space-y-4 text-sm">
            <div>
              <dt class="account-label">Nom d’utilisateur</dt>
              <dd class="mt-1 break-words">{{ details.username }}</dd>
              <dd class="mt-1 text-ink/70">
                Le nom d’utilisateur est définitif.
              </dd>
            </div>
            <div class="overview-row">
              <div class="min-w-0 basis-full sm:basis-auto">
                <dt class="account-label">Email du compte</dt>
                <dd class="break-words">{{ details.email }}</dd>
              </div>
              <dd>
                <button
                  id="trigger-email"
                  type="button"
                  class="account-link"
                  :disabled="!!busy"
                  :aria-expanded="editor === 'email'"
                  aria-controls="editor-email"
                  aria-label="Modifier mon email"
                  @click="toggleEditor('email')"
                >
                  Modifier
                </button>
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
              class="account-link"
              :disabled="!!busy"
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
              <button type="submit" class="account-primary" :disabled="!!busy">
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
            <div>
              <h3 id="account-password" class="font-semibold">Mot de passe</h3>
              <p class="mt-1 text-sm text-ink/70">
                {{ passwordAvailable ? 'Activé' : 'Non défini' }}
              </p>
            </div>
            <button
              id="trigger-password"
              type="button"
              class="account-link"
              :disabled="!!busy"
              :aria-expanded="editor === 'password'"
              aria-controls="editor-password"
              :aria-label="passwordAvailable ? 'Modifier mon mot de passe' : 'Ajouter un mot de passe'"
              @click="toggleEditor('password')"
            >
              {{ passwordAvailable ? 'Modifier' : 'Ajouter' }}
            </button>
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
              <button type="submit" class="account-primary" :disabled="!!busy">
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
                :disabled="!!busy"
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
            <div class="min-w-0">
              <h3 id="account-google" class="font-semibold">Google</h3>
              <p class="mt-1 text-sm text-ink/70">
                {{ details.google_linked ? 'Associé' : 'Non associé' }}
              </p>
              <p v-if="details.google_linked" class="mt-1 break-words text-sm">
                <span class="sr-only">Email communiqué par Google : </span>
                {{ details.google_email || 'Email non communiqué' }}
              </p>
            </div>
            <button
              v-if="passwordAvailable"
              id="trigger-google"
              type="button"
              class="account-link"
              :disabled="!!busy"
              :aria-expanded="editor === 'google'"
              aria-controls="editor-google"
              :aria-label="details.google_linked ? 'Dissocier Google' : 'Associer Google'"
              @click="toggleEditor('google')"
            >
              {{ details.google_linked ? 'Dissocier' : 'Associer' }}
            </button>
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
            <button type="submit" class="account-primary" :disabled="!!busy">
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
          <div class="overview-actions">
            <button
              type="button"
              class="account-secondary"
              :disabled="!!busy"
              @click="logout"
            >
              {{ busy === 'logout' ? 'Déconnexion…' : 'Se déconnecter' }}
            </button>
            <button
              type="button"
              class="account-link"
              :disabled="!!busy"
              @click="logoutAll"
            >
              {{
                busy === 'sessions' ? 'Déconnexion…' : 'Se déconnecter de tous les appareils'
              }}
            </button>
          </div>
          <p class="text-sm leading-relaxed">
            La déconnexion de tous les appareils inclut celui-ci. Vos cinémas
            sélectionnés restent sur cet appareil.
          </p>
          <p v-if="sessionError" role="alert" class="account-alert">
            {{ sessionError }}
          </p>
        </div>
      </section>
      <section aria-labelledby="account-delete" class="space-y-5">
        <h2 id="account-delete" class="account-heading text-primary">
          Suppression du compte
        </h2>
        <div class="min-w-0 space-y-4">
          <button
            id="trigger-delete"
            type="button"
            class="account-link text-primary"
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
                votre email actuel. La suppression devra ensuite être confirmée
                en saisissant SUPPRIMER.
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

.account-overview {
  @apply py-6 sm:py-10 lg:py-12;
}
.account-overview :deep(h1) {
  @apply mb-6 pb-5 text-[2rem] sm:text-[2.75rem];
}
.account-overview-sections > section {
  @apply min-w-0 border-t-2 border-ink pt-6 md:grid md:grid-cols-[10rem_minmax(0,1fr)] md:gap-x-8 md:space-y-0;
}
.account-overview-sections > section:first-child {
  @apply border-t-0 pt-0;
}
.overview-row {
  @apply flex flex-wrap items-center justify-between gap-x-5 gap-y-2;
}
.overview-row > :first-child {
  @apply min-w-0 flex-1;
}
.overview-actions {
  @apply flex flex-wrap items-center gap-x-5 gap-y-2;
}
</style>
