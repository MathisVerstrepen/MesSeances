<script setup lang="ts">
import { X } from '@lucide/vue'
import { posterImageSources } from '~/utils/safeImageUrl'

const props = defineProps<{
  src: string | null | undefined
  movieTitle: string
  resetKey: string
}>()

const trigger = ref<HTMLButtonElement | null>(null)
const dialog = ref<HTMLDialogElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const thumbnailLoaded = ref(false)
const isOpen = ref(false)
const dialogId = useId()
const imageSources = computed(() => posterImageSources(props.src))
const canEnlarge = computed(
  () => Boolean(imageSources.value.src) && thumbnailLoaded.value,
)
let previousOverflow: string | null = null

function detectLoadedThumbnail() {
  const image = trigger.value?.querySelector('img')
  thumbnailLoaded.value = Boolean(image?.complete && image.naturalWidth > 0)
}

async function openModal() {
  if (!canEnlarge.value || isOpen.value) return
  isOpen.value = true
  await nextTick()
  if (!isOpen.value || !dialog.value) return

  dialog.value.showModal()
  previousOverflow = document.documentElement.style.overflow
  document.documentElement.style.overflow = 'hidden'
  closeButton.value?.focus({ preventScroll: true })
}

function closeModal({ restoreFocus = true } = {}) {
  if (!isOpen.value) return
  dialog.value?.close()
  isOpen.value = false
  if (previousOverflow !== null) {
    document.documentElement.style.overflow = previousOverflow
    previousOverflow = null
  }
  if (restoreFocus) {
    nextTick(() => {
      if (!isOpen.value && trigger.value?.isConnected)
        trigger.value.focus({ preventScroll: true })
    })
  }
}

function handleImageError() {
  thumbnailLoaded.value = false
  closeModal()
}

watch(
  [() => imageSources.value.src, () => props.resetKey, () => props.movieTitle],
  async () => {
    thumbnailLoaded.value = false
    closeModal({ restoreFocus: false })
    await nextTick()
    detectLoadedThumbnail()
  },
)

onMounted(() => nextTick(detectLoadedThumbnail))
onBeforeUnmount(() => closeModal({ restoreFocus: false }))
</script>

<template>
  <div class="h-full w-full">
    <button
      ref="trigger"
      type="button"
      class="block h-full w-full p-0 enabled:cursor-zoom-in focus-visible:outline-3 focus-visible:-outline-offset-4 focus-visible:outline-highlight"
      :disabled="!canEnlarge"
      :aria-label="canEnlarge ? `Agrandir l’affiche de ${movieTitle}` : `Affiche indisponible pour ${movieTitle}`"
      :aria-haspopup="canEnlarge ? 'dialog' : undefined"
      :aria-controls="canEnlarge ? dialogId : undefined"
      :aria-expanded="canEnlarge ? isOpen : undefined"
      @click="openModal"
    >
      <PosterImage
        :src="src"
        :alt="`Affiche de ${movieTitle}`"
        sizes="(min-width: 1024px) 220px, (min-width: 640px) 180px, 160px"
        :reset-key="resetKey"
        class="h-full w-full"
        image-class="h-full w-full object-cover"
        fallback-class="gap-2 px-3 text-center text-xs font-bold text-muted"
        :fallback-icon-size="32"
        @load="detectLoadedThumbnail"
        @error="handleImageError"
      />
    </button>

    <dialog
      v-if="isOpen && imageSources.src"
      :id="dialogId"
      ref="dialog"
      class="m-0 box-border h-dvh max-h-none w-screen max-w-none overflow-hidden border-0 bg-black/90 p-3 text-white backdrop:bg-transparent sm:p-6"
      :aria-label="`Affiche de ${movieTitle}`"
      @cancel.prevent="closeModal()"
      @click.self="closeModal()"
      @keydown.tab.prevent="closeButton?.focus({ preventScroll: true })"
    >
      <button
        ref="closeButton"
        type="button"
        class="absolute top-3 right-3 grid size-11 place-items-center border-2 border-ink bg-surface text-ink shadow-[3px_3px_0_#27272a] hover:bg-highlight focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-white sm:top-6 sm:right-6"
        aria-label="Fermer l’affiche"
        @click="closeModal()"
      >
        <X :size="22" aria-hidden="true" />
      </button>
      <div
        class="flex h-full items-center justify-center"
        @click.self="closeModal()"
      >
        <img
          :src="imageSources.src"
          :srcset="imageSources.srcset ?? undefined"
          sizes="100vw"
          :alt="`Affiche de ${movieTitle}`"
          width="500"
          height="750"
          loading="lazy"
          decoding="async"
          class="block h-auto max-h-[calc(100dvh_-_6rem)] w-auto max-w-full object-contain"
          @error="handleImageError"
        >
      </div>
    </dialog>
  </div>
</template>
