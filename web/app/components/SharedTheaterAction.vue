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
  <slot :pending="pending" :error-message="errorMessage" :restore-saved-theaters="restoreSavedTheaters" />
</template>
