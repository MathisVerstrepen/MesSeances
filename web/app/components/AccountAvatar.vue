<script setup lang="ts">
import { UserRound } from '@lucide/vue'
import { avatarFileError } from '~/utils/accountAvatar'
import {
  AccountApiError,
  accountErrorMessage,
  accountWriteUncertain,
} from '~/utils/accountState'
import type { AccountAvatarResult } from '~/types/account'

const props = defineProps<{ url: string | null; blocked: boolean }>()
const emit = defineEmits<{
  busy: [value: boolean]
  changed: [result: AccountAvatarResult | null, uncertain: boolean]
}>()
const api = useAccountApi()
const account = useAccountSession()
const preview = useAccountAvatarPreview(toRef(props, 'url'))
const input = ref<HTMLInputElement | null>(null)
const selected = shallowRef<File | null>(null)
const error = ref('')
const pending = ref(false)
const progress = ref<number | null>(0)
const uploading = ref(false)
let transport: AbortController | undefined
let pickerCurrent: (() => boolean) | undefined
const disabled = computed(
  () => props.blocked || pending.value || account.writesBlocked.value,
)
const owner = computed(() => {
  const session = account.session.value
  return session?.enabled && session.state === 'complete' && session.account
    ? `${session.account.username}:${session.account.email}`
    : ''
})
// Returning from an already-open native picker may trigger focus validation
// before change. Keep that input enabled, but never loosen server-write guards.
const selectionDisabled = computed(
  () =>
    pending.value ||
    !owner.value ||
    account.status.value !== 'ready' ||
    (props.blocked && !account.revalidating.value),
)

function cancel() {
  pickerCurrent = undefined
  selected.value = null
  if (input.value) input.value.value = ''
  error.value = ''
}
const lifetime = useAccountLifetime(() => {
  transport?.abort()
  cancel()
  pending.value = false
  emit('busy', false)
})
watch(owner, () => lifetime.invalidate(), { flush: 'sync' })

function openPicker() {
  if (disabled.value || selectionDisabled.value) return
  // Soft validation changes the session revision, not this owner/lifetime.
  // Identity changes and departure invalidate the ticket through cleanup.
  pickerCurrent = lifetime.capture(false)
  input.value?.click()
}

function choose(event: Event) {
  if (!(event.target instanceof HTMLInputElement)) return
  const current = pickerCurrent
  pickerCurrent = undefined
  if (!current?.() || selectionDisabled.value) {
    event.target.value = ''
    return
  }
  const file = event.target.files?.[0]
  if (!file) return
  cancel()
  error.value = avatarFileError(file)
  if (!error.value) selected.value = file
}

async function save(remove = false) {
  if (disabled.value || (!remove && !selected.value)) return
  const current = lifetime.capture()
  const active = lifetime.capture(false)
  pending.value = true
  uploading.value = !remove
  progress.value = 0
  error.value = ''
  emit('busy', true)
  const controller = new AbortController()
  transport = controller
  try {
    const result = remove
      ? await api.removeAvatar(controller.signal)
      : await api.uploadAvatar(selected.value!, controller.signal, (value) => {
          if (current()) progress.value = value
        })
    if (!current()) return
    cancel()
    preview.clear()
    emit('changed', result, false)
  } catch (cause) {
    if (!current()) return
    error.value = accountErrorMessage(cause)
    if (
      cause instanceof AccountApiError &&
      (cause.status === 401 || cause.code === 'onboarding_required')
    ) {
      account.clear()
      return
    }
    if (
      accountWriteUncertain(cause) ||
      (cause instanceof AccountApiError && cause.status === 409)
    ) {
      cancel()
      preview.clear()
      emit('changed', null, true)
    }
  } finally {
    if (active()) {
      pending.value = false
      emit('busy', false)
    }
  }
}
</script>

<template>
  <div class="min-w-0 space-y-3" :aria-busy="pending || preview.loading.value">
    <h3 class="text-sm font-semibold">Photo</h3>
    <div class="flex flex-wrap items-center gap-4">
      <div
        class="flex size-20 shrink-0 items-center justify-center overflow-hidden border-2 border-ink/30 bg-ink/5"
      >
        <img
          v-if="preview.image.value"
          :src="preview.image.value"
          alt="Photo de profil"
          width="80"
          height="80"
          class="size-full object-cover"
          @error="preview.failed"
        >
        <UserRound v-else class="size-9 text-ink/60" aria-hidden="true" />
      </div>
      <div class="min-w-0 flex-1 space-y-2">
        <input
          id="avatar-file"
          ref="input"
          type="file"
          accept="image/jpeg,image/png,image/webp,.jpg,.jpeg,.png,.webp"
          class="hidden"
          aria-label="Choisir une photo"
          :disabled="selectionDisabled"
          @change="choose"
        >
        <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
          <button
            id="trigger-avatar"
            type="button"
            class="account-secondary min-h-11"
            :disabled="disabled"
            aria-describedby="avatar-help"
            @click="openPicker"
          >
            {{ url ? 'Changer la photo' : 'Ajouter une photo' }}
          </button>
          <button
            v-if="url"
            type="button"
            class="account-link min-h-11"
            :disabled="disabled"
            @click="save(true)"
          >
            Supprimer la photo
          </button>
        </div>
        <p id="avatar-help" class="text-sm text-ink/70">
          JPEG, PNG ou WebP non animé · 5 Mio maximum.
        </p>
      </div>
    </div>
    <div v-if="selected" class="space-y-2">
      <p class="break-all text-sm">{{ selected.name }}</p>
      <div class="flex flex-wrap items-center gap-4">
        <button
          type="button"
          class="account-primary min-h-11"
          :disabled="disabled"
          @click="save()"
        >
          Enregistrer
        </button>
        <button
          type="button"
          class="account-link min-h-11"
          :disabled="disabled"
          @click="cancel"
        >
          Annuler
        </button>
      </div>
    </div>
    <div v-if="pending" role="status" class="space-y-2 text-sm">
      <template v-if="uploading">
        <progress
          v-if="progress !== 100"
          :value="progress ?? undefined"
          max="100"
          aria-label="Envoi de la photo"
          class="h-2 w-full accent-primary"
        />
        <p>
          {{
            progress === 100 ? 'Traitement…' : progress === null ? 'Envoi de la photo…' : `Envoi de la photo : ${progress} %`
          }}
        </p>
      </template>
      <p v-else>Suppression…</p>
    </div>
    <p v-if="error" role="alert" class="account-alert">{{ error }}</p>
    <div v-if="preview.error.value" class="space-y-2">
      <p role="alert" class="account-alert">{{ preview.error.value }}</p>
      <button
        type="button"
        class="account-link min-h-11"
        :disabled="disabled || preview.loading.value"
        @click="preview.refresh"
      >
        Recharger la photo
      </button>
    </div>
    <p v-if="url" class="text-sm text-ink/70">
      Une photo supprimée ne sera pas réimportée depuis Google.
    </p>
  </div>
</template>
