<script setup lang="ts">
import { isNavigationFailure } from 'vue-router'
import { mergeOwnedQuery } from '~/utils/routeQuery'
import { SHARED_THEATERS_QUERY_KEY } from '~/utils/sharedTheaterSelection'

const route = useRoute()
const router = useRouter()
const pending = ref(false)
const errorMessage = ref('')

async function restoreSavedTheaters() {
  if (pending.value) return
  errorMessage.value = ''
  pending.value = true
  const query = mergeOwnedQuery(route.query, [SHARED_THEATERS_QUERY_KEY], {})
  try {
    const failure = await router.replace({ path: route.path, query, hash: route.hash })
    if (isNavigationFailure(failure)) throw failure
  } catch {
    errorMessage.value = 'Vos cinémas n’ont pas pu être restaurés. Réessayez.'
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <div class="shared-notice border-2 border-ink bg-surface px-4 py-3 text-sm font-bold text-ink" role="status" aria-live="polite">
    <p>Cette page utilise les cinémas partagés, différents de votre sélection.</p>
    <button type="button" class="shared-notice__action" :disabled="pending" :aria-busy="pending" @click="restoreSavedTheaters">
      {{ pending ? 'Restauration…' : 'Utiliser mes cinémas' }}
    </button>
    <p v-if="errorMessage" class="shared-notice__error" role="alert">{{ errorMessage }}</p>
  </div>
</template>

<style scoped>
.shared-notice {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem 1rem;
}

.shared-notice > p:first-child {
  flex: 1 1 24rem;
}

.shared-notice__action {
  min-height: 2.75rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 0.6rem 0.8rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.shared-notice__action:hover:not(:disabled) {
  background: #ffcf3f;
}

.shared-notice__action:focus-visible {
  outline: 3px solid #1f6f78;
  outline-offset: 2px;
}

.shared-notice__action:disabled {
  cursor: wait;
  opacity: 0.65;
}

.shared-notice__error {
  flex-basis: 100%;
  color: #991b1b;
}
</style>
