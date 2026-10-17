<script setup lang="ts">
import { Eye, EyeOff } from '@lucide/vue'
import { accountDestination } from '~/utils/accountState'
const props = defineProps<{ slug: string }>()
const account = useAccountSession()
const watched = useWatched()
const watchlist = useWatchlist()
const lifetime = useAccountLifetime()
const unknown = computed(
  () =>
    account.status.value !== 'ready' ||
    (!!watched.owner.value && !watched.ready.value),
)
const marked = computed(() => watched.slugs.value.has(props.slug))
const blocked = computed(
  () =>
    unknown.value ||
    (!!watched.owner.value &&
      (watched.writesBlocked.value ||
        (watched.preferences.value?.auto_remove_watchlist &&
          !marked.value &&
          !watchlist.ready.value))),
)
const label = computed(() =>
  unknown.value
    ? 'Films vus indisponibles pendant la vérification'
    : marked.value
      ? 'Marquer comme non vu'
      : 'Marquer comme vu',
)
async function toggle(event: MouseEvent) {
  if (blocked.value) return
  if (!watched.owner.value) {
    await navigateTo(
      account.session.value
        ? accountDestination(account.session.value)
        : '/connexion',
    )
    return
  }
  const active = lifetime.capture()
  const slug = props.slug
  const button = event.currentTarget
  if (!(button instanceof HTMLButtonElement)) return
  const keyboard = event.detail === 0
  await watched.save(slug, !marked.value)
  await nextTick()
  if (
    active() &&
    slug === props.slug &&
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
  <div v-if="account.session.value?.enabled !== false" class="text-ink">
    <button
      type="button"
      class="inline-flex size-12 items-center justify-center border-2 border-ink bg-surface enabled:hover:bg-subtle focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-current disabled:cursor-not-allowed disabled:opacity-60"
      :disabled="blocked"
      :aria-pressed="unknown ? undefined : marked"
      :aria-label="label"
      :title="label"
      :aria-busy="blocked"
      @click="toggle"
    >
      <EyeOff v-if="marked" :size="24" aria-hidden="true" />
      <Eye v-else :size="24" aria-hidden="true" />
    </button>
    <div
      v-if="watched.error.value"
      class="mt-2 max-w-sm border-2 border-ink bg-surface p-3 text-sm"
      role="alert"
    >
      <p>{{ watched.error.value }}</p>
      <button
        type="button"
        class="min-h-11 font-semibold underline"
        :disabled="watched.saving.value || account.writesBlocked.value"
        @click="watched.retry"
      >
        Réessayer
      </button>
    </div>
  </div>
</template>
