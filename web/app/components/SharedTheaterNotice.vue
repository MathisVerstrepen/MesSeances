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
  <div class="flex flex-wrap items-center gap-x-4 gap-y-3 border-2 border-ink bg-surface px-4 py-3 text-sm font-bold text-ink" role="status" aria-live="polite">
    <p class="basis-96 grow">Cette page utilise les cinémas partagés, différents de votre sélection.</p>
    <button type="button" class="min-h-11 border-2 border-ink bg-surface px-[0.8rem] py-[0.6rem] font-mono text-[0.65rem] font-black tracking-[0.08em] uppercase enabled:hover:bg-[#ffcf3f] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-wait disabled:opacity-65" :disabled="pending" :aria-busy="pending" @click="restoreSavedTheaters">
      {{ pending ? 'Restauration…' : 'Utiliser mes cinémas' }}
    </button>
    <p v-if="errorMessage" class="basis-full text-primary" role="alert">{{ errorMessage }}</p>
  </div>
</template>
