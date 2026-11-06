<script setup lang="ts">
import { Play, X } from '@lucide/vue'
import type { ResolvedTrailer } from '~/types/api'
import { providerTrailerPlayback } from '~/utils/providerTrailer'

const props = defineProps<{ movieTitle: string; trailer: ResolvedTrailer }>()
const playback = computed(() => providerTrailerPlayback(props.trailer))
const dialog = ref<HTMLDialogElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const isOpen = ref(false)
const failed = ref(false)
const titleId = useId()

async function open() {
  if (!playback.value || isOpen.value) return
  failed.value = false
  isOpen.value = true
  await nextTick()
  if (!isOpen.value) return
  dialog.value?.showModal()
  closeButton.value?.focus({ preventScroll: true })
}

function close(restoreFocus = true) {
  dialog.value?.close()
  isOpen.value = false
  if (restoreFocus) trigger.value?.focus({ preventScroll: true })
}

watch(playback, () => close(false))
onBeforeUnmount(() => close(false))
</script>

<template>
  <div v-if="playback" class="mt-5">
    <button
      ref="trigger"
      type="button"
      class="inline-flex min-h-11 items-center justify-center gap-2 border-2 border-ink bg-[#ffcf3f] px-4 py-2 font-mono text-xs font-black uppercase tracking-[0.08em] text-ink shadow-[4px_4px_0_#27272a] hover:bg-highlight focus-visible:ring-2 focus-visible:ring-highlight focus-visible:ring-offset-4"
      aria-haspopup="dialog"
      :aria-controls="`${titleId}-dialog`"
      :aria-expanded="isOpen"
      :aria-label="`Voir la bande-annonce de ${movieTitle}`"
      @click="open"
    >
      <Play :size="17" fill="currentColor" aria-hidden="true" />
      Bande-annonce
    </button>
    <dialog
      v-if="isOpen"
      :id="`${titleId}-dialog`"
      ref="dialog"
      class="m-0 box-border h-dvh max-h-none w-screen max-w-none overflow-auto border-0 bg-black/85 p-3 backdrop:bg-transparent sm:p-6"
      :aria-labelledby="titleId"
      @cancel.prevent="close()"
      @click.self="close()"
    >
      <div
        class="flex min-h-full items-center justify-center"
        @click.self="close()"
      >
        <div
          class="w-full max-w-6xl border-2 border-white bg-black shadow-[8px_8px_0_#ffcf3f]"
        >
          <header
            class="flex items-center justify-between gap-3 border-b-2 border-ink bg-[#f8f7f2] px-4 py-3 text-ink sm:px-5"
          >
            <h2
              :id="titleId"
              class="min-w-0 truncate text-lg font-black tracking-[-0.035em] sm:text-2xl"
            >
              Bande-annonce de {{ movieTitle }}
            </h2>
            <button
              ref="closeButton"
              type="button"
              class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-white hover:bg-highlight focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2"
              :aria-label="`Fermer la bande-annonce de ${movieTitle}`"
              @click="close()"
            >
              <X :size="22" aria-hidden="true" />
            </button>
          </header>
          <div class="aspect-video w-full bg-black">
            <p
              v-if="failed"
              role="status"
              class="flex h-full items-center justify-center p-6 text-white"
            >
              Cette bande-annonce est momentanément indisponible.
            </p>
            <video
              v-else-if="playback.type === 'mp4'"
              :src="playback.url"
              :aria-label="`Bande-annonce de ${movieTitle}`"
              class="size-full"
              controls
              autoplay
              playsinline
              preload="none"
              referrerpolicy="no-referrer"
              @error="failed = true"
            />
            <iframe
              v-else
              :src="playback.url"
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
