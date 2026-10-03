<script setup lang="ts">
import { BookmarkCheck, BookmarkPlus } from '@lucide/vue'
import { accountDestination } from '~/utils/accountState'

const props = defineProps<{ theaterId: string }>()
const emit = defineEmits<{ feedback: [message: string] }>()
const account = useAccountSession()
const follows = useCinemaFollows()
const lifetime = useAccountLifetime()
const followed = computed(
  () => follows.ready.value && follows.ids.value.has(props.theaterId),
)
const known = computed(() => !!follows.owner.value && follows.ready.value)
const unknown = computed(
  () =>
    account.status.value !== 'ready' ||
    (!!follows.owner.value && !follows.ready.value),
)
const blocked = computed(
  () => unknown.value || account.writesBlocked.value || follows.saving.value,
)
const label = computed(() =>
  unknown.value
    ? 'Suivi indisponible pendant la vérification'
    : followed.value
      ? 'Ne plus suivre ce cinéma'
      : 'Suivre ce cinéma',
)

async function toggle(event: MouseEvent) {
  if (blocked.value) return
  if (!follows.owner.value) {
    await navigateTo(
      account.session.value
        ? accountDestination(account.session.value)
        : '/connexion',
    )
    return
  }
  const active = lifetime.capture()
  const theaterId = props.theaterId
  // SAFETY: This handler is attached only to the follow button's click event.
  const button = event.currentTarget as HTMLButtonElement
  const keyboard = event.detail === 0
  emit('feedback', '')
  const message = await follows.save(theaterId, !followed.value)
  if (!active() || theaterId !== props.theaterId) return
  emit('feedback', message)
  await nextTick()
  if (
    active() &&
    keyboard &&
    button.isConnected &&
    !button.disabled &&
    button.getClientRects().length &&
    (document.activeElement === document.body ||
      document.activeElement === button)
  )
    button.focus()
}
</script>

<template>
  <button
    v-if="account.session.value?.enabled !== false"
    type="button"
    class="inline-flex size-11 shrink-0 items-center justify-center border-2 border-ink bg-surface text-ink enabled:hover:bg-highlight focus-visible:ring-3 focus-visible:ring-surface focus-visible:outline-3 focus-visible:outline-solid focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-60"
    :disabled="blocked"
    :aria-label="label"
    :title="label"
    :aria-pressed="known ? followed : undefined"
    :aria-busy="follows.saving.value || account.revalidating.value"
    @click="toggle"
  >
    <component
      :is="followed ? BookmarkCheck : BookmarkPlus"
      :size="20"
      aria-hidden="true"
    />
  </button>
</template>
