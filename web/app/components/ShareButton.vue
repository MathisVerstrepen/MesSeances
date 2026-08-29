<script setup lang="ts">
import { Check, Copy, LoaderCircle, Share2 } from '@lucide/vue'
import { withSharedTheaterSelection } from '~/utils/sharedTheaterSelection'
import { isValidShortLinkCode, isValidShortLinkTarget } from '~/utils/shortLinkTarget'

type PreparationState = 'idle' | 'pending' | 'ready' | 'error'

const props = withDefaults(defineProps<{
  appearance?: 'compact' | 'hero'
  target?: string
  theaterIds?: readonly string[]
}>(), {
  appearance: 'compact'
})

const route = useRoute()
const api = useMesSeancesApi()
const pageSelection = usePageCinemaSelection()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const popup = ref<HTMLElement | null>(null)
const isOpen = ref(false)
const preparationState = ref<PreparationState>('idle')
const shortUrl = ref('')
const preparationError = ref('')
const copyError = ref('')
const copied = ref(false)
const liveMessage = ref('')
const popupLeft = ref(16)
const popupTop = ref(0)
const popupId = useId()
const target = computed(() => props.target ?? route.fullPath)
const theaterIds = computed(() => props.theaterIds ?? pageSelection.activeTheaterIds.value)
let requestSequence = 0
let popupFocusSequence = 0
let isInitializingForShare = false

const displayUrl = computed(() => {
  if (!shortUrl.value) return ''
  try {
    const url = new URL(shortUrl.value)
    return `${url.host}${url.pathname}${url.search}${url.hash}`
  } catch {
    return shortUrl.value.replace(/^https?:\/\//u, '')
  }
})

const popupStyle = computed(() => ({ left: `${popupLeft.value}px`, top: `${popupTop.value}px` }))

watch([() => route.fullPath, () => props.target, () => props.theaterIds?.join(',')], () => reset())
watch(() => pageSelection.activeTheaterIds.value.join(','), () => {
  if (!isInitializingForShare && props.theaterIds === undefined) reset()
})

function clearCopyFeedback() {
  copied.value = false
  copyError.value = ''
  liveMessage.value = ''
}

function reset() {
  requestSequence++
  popupFocusSequence++
  isOpen.value = false
  preparationState.value = 'idle'
  shortUrl.value = ''
  preparationError.value = ''
  clearCopyFeedback()
}

async function focusPopup(currentFocusSequence: number) {
  await nextTick()
  if (!isOpen.value || currentFocusSequence !== popupFocusSequence) return
  popup.value?.focus({ preventScroll: true })
}

async function updatePopupPosition() {
  if (!isOpen.value) return
  await nextTick()
  const rootRect = root.value?.getBoundingClientRect()
  const popupRect = popup.value?.getBoundingClientRect()
  if (!rootRect || !popupRect) return

  const gutter = 16
  const maximumLeft = Math.max(gutter, window.innerWidth - gutter - popupRect.width)
  const rightAlignedLeft = rootRect.right - popupRect.width
  popupLeft.value = Math.min(Math.max(rightAlignedLeft, gutter), maximumLeft)
  popupTop.value = rootRect.bottom + 8
}

function closePopup({ restoreFocus = false } = {}) {
  if (!isOpen.value) return
  popupFocusSequence++
  isOpen.value = false
  clearCopyFeedback()
  if (restoreFocus) nextTick(() => trigger.value?.focus())
}

function openPopup() {
  const currentFocusSequence = ++popupFocusSequence
  isOpen.value = true
  clearCopyFeedback()
  void focusPopup(currentFocusSequence)
  void updatePopupPosition()
  if (preparationState.value === 'idle') void prepareLink()
}

function togglePopup() {
  if (isOpen.value) closePopup()
  else openPopup()
}

async function prepareLink() {
  const currentRequest = ++requestSequence
  preparationState.value = 'pending'
  preparationError.value = ''
  clearCopyFeedback()

  isInitializingForShare = true
  try {
    await pageSelection.initialize()
  } finally {
    isInitializingForShare = false
  }
  if (currentRequest !== requestSequence) return

  const preparedTarget = withSharedTheaterSelection(target.value, theaterIds.value)
  if (!pageSelection.isInitialized.value || preparedTarget === null) {
    preparationState.value = 'error'
    preparationError.value = 'Aucun cinéma ne peut être partagé. Vérifiez votre sélection puis réessayez.'
    return
  }
  if (!isValidShortLinkTarget(preparedTarget)) {
    preparationState.value = 'error'
    preparationError.value = 'Cette page ne peut pas être partagée. Vérifiez l’adresse puis réessayez.'
    return
  }

  try {
    const response = await api.createShortLink(preparedTarget)
    if (currentRequest !== requestSequence) return
    if (response.target !== preparedTarget || !isValidShortLinkCode(response.code)) throw new Error('Invalid shortlink response')
    shortUrl.value = `${window.location.origin}/s/${response.code}`
    preparationState.value = 'ready'
    liveMessage.value = 'Lien prêt à être copié.'
    void updatePopupPosition()
  } catch (error) {
    if (currentRequest !== requestSequence) return
    preparationState.value = 'error'
    preparationError.value = getFrenchShortLinkPreparationError(error)
  }
}

async function copyLink() {
  if (preparationState.value !== 'ready' || !shortUrl.value) return
  copyError.value = ''
  liveMessage.value = ''
  try {
    await navigator.clipboard.writeText(shortUrl.value)
    copied.value = true
    liveMessage.value = 'Lien copié.'
  } catch {
    copied.value = false
    copyError.value = 'Le lien n’a pas pu être copié. Autorisez le presse-papiers puis réessayez.'
  }
}

function handleDocumentPointerDown(event: PointerEvent) {
  if (!isOpen.value || !(event.target instanceof Node)) return
  if (!root.value?.contains(event.target) && !popup.value?.contains(event.target)) closePopup()
}

function handleDocumentKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !isOpen.value) return
  event.preventDefault()
  closePopup({ restoreFocus: true })
}

function handleResize() {
  void updatePopupPosition()
}

onMounted(() => {
  document.addEventListener('pointerdown', handleDocumentPointerDown)
  document.addEventListener('keydown', handleDocumentKeydown)
  window.addEventListener('resize', handleResize)
  window.addEventListener('scroll', handleResize, true)
})

onBeforeUnmount(() => {
  requestSequence++
  popupFocusSequence++
  document.removeEventListener('pointerdown', handleDocumentPointerDown)
  document.removeEventListener('keydown', handleDocumentKeydown)
  window.removeEventListener('resize', handleResize)
  window.removeEventListener('scroll', handleResize, true)
})
</script>

<template>
  <div ref="root" class="share-control" :class="`share-control--${props.appearance}`">
    <button
      ref="trigger"
      type="button"
      class="share-control__trigger"
      aria-label="Partager cette page"
      aria-haspopup="dialog"
      :aria-controls="popupId"
      :aria-expanded="isOpen"
      @click="togglePopup"
    >
      <Share2 :size="18" aria-hidden="true" />
    </button>

    <Teleport to="body">
      <div
        v-if="isOpen"
        :id="popupId"
        ref="popup"
        class="share-popup"
        :style="popupStyle"
        role="dialog"
        aria-label="Partager cette page"
        tabindex="-1"
      >
        <div v-if="preparationState === 'pending'" class="share-popup__status" role="status" aria-live="polite">
          <LoaderCircle :size="18" class="share-popup__spinner animate-spin" aria-hidden="true" />
          <span>Préparation du lien…</span>
        </div>

        <div v-else-if="preparationState === 'error'" class="share-popup__error-state">
          <p role="alert">{{ preparationError }}</p>
          <button type="button" class="share-popup__retry" @click="prepareLink">Réessayer</button>
        </div>

        <template v-else-if="preparationState === 'ready'">
          <div class="share-popup__url-row">
            <span class="share-popup__url" :title="displayUrl">{{ displayUrl }}</span>
            <button
              type="button"
              class="share-popup__copy"
              :aria-label="copied ? 'Lien copié' : 'Copier le lien'"
              @click="copyLink"
            >
              <Check v-if="copied" :size="18" aria-hidden="true" />
              <Copy v-else :size="18" aria-hidden="true" />
            </button>
          </div>
          <p v-if="copyError" class="share-popup__copy-error" role="alert">{{ copyError }}</p>
        </template>

        <p class="sr-only" aria-live="polite">{{ liveMessage }}</p>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.share-control {
  position: relative;
  z-index: 20;
  display: inline-block;
  width: 2.75rem;
  height: 2.75rem;
  flex: 0 0 2.75rem;
}

.share-control__trigger,
.share-popup__copy,
.share-popup__retry {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 2px solid #27272a;
  background: #fff;
  color: #27272a;
}

.share-control__trigger {
  width: 100%;
  height: 100%;
  padding: 0;
  transition: background-color 150ms ease, color 150ms ease;
}

.share-control__trigger:hover,
.share-popup__copy:hover,
.share-popup__retry:hover {
  background: #ffcf3f;
}

.share-control__trigger:focus-visible,
.share-popup:focus-visible,
.share-popup__copy:focus-visible,
.share-popup__retry:focus-visible {
  outline: 3px solid #1f6f78;
  outline-offset: 2px;
}

.share-control--hero {
  width: 3rem;
  height: 3rem;
  flex-basis: 3rem;
}

.share-control--hero .share-control__trigger {
  border-radius: 0.35rem;
}

.share-popup {
  position: fixed;
  z-index: 50;
  width: 20rem;
  max-width: calc(100vw - 2rem);
  border: 2px solid #27272a;
  background: #fff;
  padding: 0.75rem;
  color: #27272a;
  box-shadow: 5px 5px 0 #27272a;
}

.share-popup__status {
  display: flex;
  min-height: 2.75rem;
  align-items: center;
  gap: 0.65rem;
  font-size: 0.8rem;
  font-weight: 800;
}

.share-popup__url-row {
  display: flex;
  min-width: 0;
  align-items: stretch;
}

.share-popup__url {
  display: block;
  min-width: 0;
  flex: 1 1 auto;
  overflow: hidden;
  border: 2px solid #27272a;
  border-right: 0;
  background: #f1efe8;
  padding: 0 0.7rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.72rem;
  font-weight: 800;
  line-height: 2.5rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.share-popup__copy {
  width: 2.75rem;
  height: 2.75rem;
  flex: 0 0 2.75rem;
  padding: 0;
}

.share-popup__error-state {
  display: grid;
  gap: 0.75rem;
  color: #991b1b;
  font-size: 0.78rem;
  font-weight: 800;
  line-height: 1.35;
}

.share-popup__retry {
  min-height: 2.75rem;
  width: fit-content;
  padding: 0.55rem 0.8rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.65rem;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.share-popup__copy-error {
  margin-top: 0.65rem;
  color: #991b1b;
  font-size: 0.75rem;
  font-weight: 800;
  line-height: 1.35;
}

@media (prefers-reduced-motion: reduce) {
  .share-control__trigger {
    transition: none;
  }

  .share-popup__spinner {
    animation: none;
  }
}
</style>
