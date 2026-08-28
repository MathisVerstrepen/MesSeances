<script setup lang="ts">
import { Play, X } from '@lucide/vue'
import { validatedYouTubeKey, youtubeNoCookieTrailerEmbedUrl } from '~/utils/youtubeTrailer'

const props = defineProps<{
  movieTitle: string
  youtubeKey?: string | null
}>()

const trigger = ref<HTMLButtonElement | null>(null)
const dialog = ref<HTMLDialogElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const isOpen = ref(false)
const dialogTitleId = useId()

const youtubeKey = computed(() => validatedYouTubeKey(props.youtubeKey))
const embedUrl = computed(() => isOpen.value ? youtubeNoCookieTrailerEmbedUrl(youtubeKey.value) : null)

async function openModal() {
  if (!youtubeKey.value || isOpen.value) return

  isOpen.value = true
  await nextTick()
  if (!isOpen.value || !dialog.value) return

  dialog.value.showModal()
  closeButton.value?.focus({ preventScroll: true })
}

function closeModal({ restoreFocus = true } = {}) {
  if (!isOpen.value) return

  dialog.value?.close()
  isOpen.value = false
  if (restoreFocus) nextTick(() => trigger.value?.focus({ preventScroll: true }))
}

watch(youtubeKey, (key, previousKey) => {
  if (isOpen.value && key !== previousKey) closeModal({ restoreFocus: false })
})

onBeforeUnmount(() => closeModal({ restoreFocus: false }))
</script>

<template>
  <div v-if="youtubeKey" class="mt-5">
    <button
      ref="trigger"
      type="button"
      class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-[#ffcf3f] px-4 py-2 font-mono text-xs font-black uppercase tracking-[0.08em] text-ink shadow-[4px_4px_0_#27272a] hover:bg-highlight focus-visible:ring-2 focus-visible:ring-highlight focus-visible:ring-offset-4"
      aria-haspopup="dialog"
      :aria-controls="`${dialogTitleId}-dialog`"
      :aria-expanded="isOpen"
      :aria-label="`Voir la bande-annonce de ${movieTitle}`"
      @click="openModal"
    >
      <Play :size="17" fill="currentColor" aria-hidden="true" />
      Bande-annonce
    </button>

    <dialog
      v-if="isOpen && embedUrl"
      :id="`${dialogTitleId}-dialog`"
      ref="dialog"
      class="trailer-dialog"
      :aria-labelledby="dialogTitleId"
      @cancel.prevent="closeModal()"
      @click.self="closeModal()"
    >
      <div class="flex min-h-full items-center justify-center" @click.self="closeModal()">
        <div class="w-full max-w-6xl border-2 border-white bg-black shadow-[8px_8px_0_#ffcf3f]">
          <header class="flex items-center justify-between gap-4 border-b-2 border-ink bg-[#f8f7f2] px-4 py-3 text-ink sm:px-5">
            <h2 :id="dialogTitleId" class="min-w-0 truncate text-lg font-black tracking-[-0.035em] sm:text-2xl">
              Bande-annonce de {{ movieTitle }}
            </h2>
            <button
              ref="closeButton"
              type="button"
              class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-white hover:bg-highlight focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2"
              aria-label="Fermer la bande-annonce"
              @click="closeModal()"
            >
              <X :size="22" aria-hidden="true" />
            </button>
          </header>
          <div class="aspect-video w-full bg-black">
            <iframe
              :src="embedUrl"
              :title="`Bande-annonce de ${movieTitle} sur YouTube`"
              class="size-full border-0"
              allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
              referrerpolicy="strict-origin-when-cross-origin"
              allowfullscreen
            />
          </div>
        </div>
      </div>
    </dialog>
  </div>
</template>

<style scoped>
.trailer-dialog {
  box-sizing: border-box;
  width: 100vw;
  max-width: none;
  height: 100dvh;
  max-height: none;
  margin: 0;
  border: 0;
  background: rgb(0 0 0 / 85%);
  overflow: auto;
  padding: 0.75rem;
}

.trailer-dialog::backdrop {
  background: transparent;
}

@media (min-width: 640px) {
  .trailer-dialog {
    padding: 1.5rem;
  }
}
</style>
