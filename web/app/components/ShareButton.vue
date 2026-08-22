<script setup lang="ts">
import { Check, Copy, LoaderCircle, Share2 } from '@lucide/vue'
import { withSharedTheaterSelection } from '~/utils/sharedTheaterSelection'
import { isValidShortLinkCode, isValidShortLinkTarget } from '~/utils/shortLinkTarget'

type ShareState = 'idle' | 'pending' | 'ready' | 'success' | 'error'

const props = withDefaults(defineProps<{
  appearance?: 'compact' | 'hero'
}>(), {
  appearance: 'compact'
})

const route = useRoute()
const api = useMesSeancesApi()
const pageSelection = usePageCinemaSelection()
const state = ref<ShareState>('idle')
const shortUrl = ref('')
const errorMessage = ref('')
const liveMessage = ref('')
const canNativeShare = ref(false)

const hasPreparedLink = computed(() => Boolean(shortUrl.value))
const buttonLabel = computed(() => {
  if (state.value === 'pending') return 'Préparation…'
  if (!hasPreparedLink.value) return state.value === 'error' ? 'Réessayer' : 'Partager'
  if (state.value === 'success') return canNativeShare.value ? 'Partager à nouveau' : 'Copier à nouveau'
  return canNativeShare.value ? 'Partager le lien' : 'Copier le lien'
})

onMounted(() => {
  canNativeShare.value = 'share' in navigator
})

watch([() => route.fullPath, () => pageSelection.activeTheaterIds.value.join(',')], () => reset())

function reset() {
  state.value = 'idle'
  shortUrl.value = ''
  errorMessage.value = ''
  liveMessage.value = ''
}

async function activate() {
  if (state.value === 'pending') return
  if (hasPreparedLink.value) {
    await shareOrCopy()
    return
  }
  await prepareLink()
}

async function prepareLink() {
  errorMessage.value = ''
  liveMessage.value = ''
  state.value = 'pending'

  await pageSelection.initialize()
  const target = withSharedTheaterSelection(route.fullPath, pageSelection.activeTheaterIds.value)
  if (!pageSelection.isInitialized.value || target === null) {
    state.value = 'error'
    errorMessage.value = 'Aucun cinéma ne peut être partagé. Vérifiez votre sélection puis réessayez.'
    return
  }
  if (!isValidShortLinkTarget(target)) {
    state.value = 'error'
    errorMessage.value = 'Cette page ne peut pas être partagée. Vérifiez l’adresse puis réessayez.'
    return
  }

  try {
    const response = await api.createShortLink(target)
    if (response.target !== target || !isValidShortLinkCode(response.code)) throw new Error('Invalid shortlink response')
    shortUrl.value = `${window.location.origin}/s/${response.code}`
    state.value = 'ready'
    liveMessage.value = canNativeShare.value ? 'Lien prêt à être partagé.' : 'Lien prêt à être copié.'
  } catch {
    state.value = 'error'
    errorMessage.value = 'Le lien n’a pas pu être préparé. Réessayez.'
  }
}

async function shareOrCopy() {
  errorMessage.value = ''
  liveMessage.value = ''
  try {
    if (canNativeShare.value) {
      await navigator.share({ title: document.title, url: shortUrl.value })
      liveMessage.value = 'Lien partagé.'
    } else {
      await navigator.clipboard.writeText(shortUrl.value)
      liveMessage.value = 'Lien copié.'
    }
    state.value = 'success'
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      state.value = 'ready'
      return
    }
    state.value = 'error'
    errorMessage.value = canNativeShare.value
      ? 'Le lien n’a pas pu être partagé. Réessayez.'
      : 'Le lien n’a pas pu être copié. Autorisez le presse-papiers puis réessayez.'
  }
}
</script>

<template>
  <div class="share-control" :class="`share-control--${props.appearance}`">
    <button type="button" class="share-control__button" :disabled="state === 'pending'" @click="activate">
      <LoaderCircle v-if="state === 'pending'" :size="17" class="animate-spin" aria-hidden="true" />
      <Check v-else-if="state === 'success'" :size="17" aria-hidden="true" />
      <Copy v-else-if="hasPreparedLink && !canNativeShare" :size="17" aria-hidden="true" />
      <Share2 v-else :size="17" aria-hidden="true" />
      <span>{{ buttonLabel }}</span>
    </button>
    <p class="sr-only" aria-live="polite">{{ liveMessage }}</p>
    <p v-if="errorMessage" class="share-control__error" role="alert">{{ errorMessage }}</p>
  </div>
</template>

<style scoped>
.share-control {
  display: inline-flex;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
}

.share-control__button {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border: 2px solid #27272a;
  background: #fff;
  padding: 0.65rem 0.8rem;
  color: #27272a;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.68rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  transition: background-color 150ms ease, color 150ms ease, transform 150ms ease;
}

.share-control__button:hover:not(:disabled) {
  background: #ffcf3f;
}

.share-control__button:disabled {
  cursor: wait;
  opacity: 0.65;
}

.share-control--hero .share-control__button {
  min-height: 3rem;
  border-radius: 0.35rem;
  padding: 0.75rem 1rem;
}

.share-control--hero .share-control__button:hover:not(:disabled) {
  transform: translateY(-2px);
}

.share-control__error {
  margin-top: 0.35rem;
  max-width: 18rem;
  color: #991b1b;
  font-size: 0.75rem;
  font-weight: 700;
  line-height: 1.25;
}

@media (prefers-reduced-motion: reduce) {
  .share-control__button {
    transition: none;
  }

  .share-control--hero .share-control__button:hover:not(:disabled) {
    transform: none;
  }
}
</style>
