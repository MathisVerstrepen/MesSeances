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
  <div ref="root" class="share-control relative z-20 inline-block size-11 shrink-0 basis-11" :class="props.appearance === 'hero' ? 'size-12 basis-12' : ''">
    <button
      ref="trigger"
      type="button"
      class="share-control__trigger inline-flex size-full items-center justify-center border-2 border-ink bg-surface p-0 text-ink [transition:background-color_150ms_ease,color_150ms_ease] hover:bg-[#ffcf3f] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent motion-reduce:transition-none"
      :class="props.appearance === 'hero' ? 'rounded-[0.35rem]' : ''"
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
        class="share-popup fixed z-50 w-80 max-w-[calc(100vw-2rem)] border-2 border-ink bg-surface p-3 text-ink shadow-[5px_5px_0_#27272a] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent"
        :style="popupStyle"
        role="dialog"
        aria-label="Partager cette page"
        tabindex="-1"
      >
        <div v-if="preparationState === 'pending'" class="flex min-h-11 items-center gap-[0.65rem] text-[0.8rem] font-extrabold" role="status" aria-live="polite">
          <LoaderCircle :size="18" class="animate-spin motion-reduce:animate-none" aria-hidden="true" />
          <span>Préparation du lien…</span>
        </div>

        <div v-else-if="preparationState === 'error'" class="grid gap-3 text-[0.78rem] leading-[1.35] font-extrabold text-primary">
          <p role="alert">{{ preparationError }}</p>
          <button type="button" class="min-h-11 w-fit border-2 border-ink bg-surface px-[0.8rem] py-[0.55rem] font-mono text-[0.65rem] font-black tracking-[0.08em] text-ink uppercase hover:bg-[#ffcf3f] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent" @click="prepareLink">Réessayer</button>
        </div>

        <template v-else-if="preparationState === 'ready'">
          <div class="flex min-w-0 items-stretch">
            <span class="block min-w-0 flex-auto overflow-hidden border-2 border-r-0 border-ink bg-[#f1efe8] px-[0.7rem] font-mono text-[0.72rem] leading-10 font-extrabold text-ellipsis whitespace-nowrap" :title="displayUrl">{{ displayUrl }}</span>
            <button
              type="button"
              class="inline-flex size-11 shrink-0 basis-11 items-center justify-center border-2 border-ink bg-surface p-0 text-ink hover:bg-[#ffcf3f] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent"
              :aria-label="copied ? 'Lien copié' : 'Copier le lien'"
              @click="copyLink"
            >
              <Check v-if="copied" :size="18" aria-hidden="true" />
              <Copy v-else :size="18" aria-hidden="true" />
            </button>
          </div>
          <p v-if="copyError" class="mt-[0.65rem] text-xs leading-[1.35] font-extrabold text-primary" role="alert">{{ copyError }}</p>
        </template>

        <p class="sr-only" aria-live="polite">{{ liveMessage }}</p>
      </div>
    </Teleport>
  </div>
</template>
