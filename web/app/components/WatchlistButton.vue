<script setup lang="ts">
import { accountDestination } from '~/utils/accountState'

const props = withDefaults(
  defineProps<{ slug: string; showError?: boolean }>(),
  { showError: true },
)
const account = useAccountSession()
const watchlist = useWatchlist()
const saved = computed(
  () => watchlist.ready.value && watchlist.slugs.value.has(props.slug),
)
const unknown = computed(
  () =>
    account.status.value !== 'ready' ||
    (!!watchlist.owner.value && !watchlist.ready.value),
)
const label = computed(() =>
  unknown.value
    ? 'Watchlist indisponible pendant la vérification'
    : saved.value
      ? 'Retirer de la watchlist'
      : 'Ajouter à la watchlist',
)
async function toggle() {
  if (unknown.value) return
  if (!watchlist.owner.value) {
    await navigateTo(
      account.session.value
        ? accountDestination(account.session.value)
        : '/connexion',
    )
    return
  }
  await watchlist.save(props.slug, !saved.value)
}
</script>

<template>
  <div v-if="account.session.value?.enabled !== false" class="text-ink">
    <button
      type="button"
      class="inline-flex size-12 items-center justify-center border-2 border-ink bg-surface enabled:hover:bg-subtle focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-current disabled:cursor-not-allowed disabled:opacity-60"
      :disabled="unknown || watchlist.saving.value"
      :aria-pressed="unknown ? undefined : saved"
      :aria-label="label"
      :title="label"
      :aria-busy="unknown || watchlist.saving.value"
      @click="toggle"
    >
      <WatchlistIcon :variant="saved ? 'remove' : 'add'" :size="24" />
    </button>
    <div
      v-if="showError && watchlist.error.value"
      class="mt-2 max-w-sm border-2 border-ink bg-surface p-3 text-sm"
      role="alert"
    >
      <p>{{ watchlist.error.value }}</p>
      <button
        type="button"
        class="min-h-11 font-semibold underline"
        :disabled="watchlist.saving.value"
        @click="watchlist.retry"
      >
        Réessayer
      </button>
    </div>
  </div>
</template>
