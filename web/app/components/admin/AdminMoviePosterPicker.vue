<script setup lang="ts">
import { Check, Images, LoaderCircle, RefreshCw, X } from '@lucide/vue'
import { safePosterUrl } from '~/utils/safeImageUrl'

const props = defineProps<{
  movieId: string
  movieTitle: string
  currentPosterUrl: string | null
  disabled?: boolean
}>()
const emit = defineEmits<{ select: [url: string] }>()
const api = useMesSeancesApi()
const { posters, status, load, reset } = useAdminMoviePosters(api.adminMoviePosters)
const dialog = ref<HTMLDialogElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const isOpen = ref(false)
const titleId = useId()
const currentUrl = computed(() => safePosterUrl(props.currentPosterUrl))
let activeTrigger: HTMLButtonElement | null = null

async function openModal(event: MouseEvent) {
  if (props.disabled || isOpen.value || !(event.currentTarget instanceof HTMLButtonElement)) return
  activeTrigger = event.currentTarget
  isOpen.value = true
  void load(props.movieId)
  await nextTick()
  if (!isOpen.value || !dialog.value) return
  dialog.value.showModal()
  closeButton.value?.focus({ preventScroll: true })
}

function closeModal({ restoreFocus = true } = {}) {
  reset()
  if (!isOpen.value) return
  isOpen.value = false
  dialog.value?.close()
  const trigger = activeTrigger
  activeTrigger = null
  if (restoreFocus) nextTick(() => {
    if (trigger?.isConnected) trigger.focus({ preventScroll: true })
  })
}

function selectPoster(url: string) {
  if (!isOpen.value || props.disabled || status.value !== 'ready' || !posters.value.some((poster) => poster.url === url)) return
  emit('select', url)
  closeModal()
}

watch(() => props.movieId, () => closeModal({ restoreFocus: false }))
watch(() => props.disabled, (disabled) => { if (disabled) closeModal() })
onBeforeUnmount(() => closeModal({ restoreFocus: false }))
</script>

<template>
  <button
    type="button"
    class="inline-flex min-h-8 items-center gap-1.5 rounded-md border border-line px-2 text-xs font-semibold text-accent hover:bg-accent-soft focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 disabled:opacity-50"
    :disabled="disabled"
    aria-haspopup="dialog"
    :aria-controls="`${titleId}-dialog`"
    :aria-expanded="isOpen"
    @click="openModal"
  >
    <Images :size="16" aria-hidden="true" /> Choisir sur TMDB
  </button>

  <dialog
    v-if="isOpen"
    :id="`${titleId}-dialog`"
    ref="dialog"
    class="m-0 box-border h-dvh max-h-none w-screen max-w-none overflow-auto border-0 bg-black/60 p-3 backdrop:bg-transparent sm:p-6"
    :aria-labelledby="titleId"
    @cancel.prevent="closeModal()"
    @click.self="closeModal()"
    @close="closeModal()"
  >
    <div class="flex min-h-full items-center justify-center" @click.self="closeModal()">
      <section class="w-full max-w-4xl rounded-lg bg-surface text-ink shadow-xl">
        <header class="sticky top-0 z-10 flex items-center justify-between gap-3 rounded-t-lg border-b border-line bg-surface px-4 py-3 sm:px-6">
          <h2 :id="titleId" class="min-w-0 text-lg font-semibold">Affiches TMDB - {{ movieTitle }}</h2>
          <button ref="closeButton" type="button" class="grid size-11 shrink-0 place-items-center rounded-md border border-line hover:bg-accent-soft focus-visible:ring-2 focus-visible:ring-accent" aria-label="Fermer les affiches" @click="closeModal()">
            <X :size="22" aria-hidden="true" />
          </button>
        </header>
        <div class="p-4 sm:p-6" :aria-busy="status === 'loading'">
          <p v-if="status === 'loading'" class="flex min-h-32 items-center justify-center gap-2 text-sm text-muted" role="status">
            <LoaderCircle :size="20" class="animate-spin text-accent" aria-hidden="true" /> Chargement des affiches…
          </p>
          <div v-else-if="status === 'error'" class="py-8 text-center">
            <p class="text-sm font-semibold text-red-700" role="alert">Impossible de charger les affiches TMDB.</p>
            <button type="button" class="mt-4 inline-flex min-h-11 items-center gap-2 rounded-md border border-line px-4 text-sm font-semibold text-accent focus-visible:ring-2 focus-visible:ring-accent" @click="load(movieId)">
              <RefreshCw :size="16" aria-hidden="true" /> Réessayer
            </button>
          </div>
          <p v-else-if="status === 'ready' && !posters.length" class="py-8 text-center text-sm text-muted" role="status">Aucune affiche TMDB disponible pour ce film.</p>
          <ul v-else class="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4" aria-label="Affiches disponibles">
            <li v-for="(poster, index) in posters" :key="poster.url">
              <button
                type="button"
                class="h-full w-full overflow-hidden rounded-md border-2 text-left focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2"
                :class="safePosterUrl(poster.url) === currentUrl ? 'border-accent bg-accent-soft' : 'border-line hover:border-accent'"
                :aria-pressed="safePosterUrl(poster.url) === currentUrl"
                :aria-label="`Choisir l’affiche ${index + 1}, ${poster.language ?? 'sans langue'}, ${poster.width} × ${poster.height}`"
                @click="selectPoster(poster.url)"
              >
                <PosterImage :src="poster.url" :alt="`Affiche ${index + 1} de ${movieTitle}`" sizes="(min-width: 640px) 200px, 45vw" class="aspect-[2/3] w-full bg-canvas" image-class="size-full object-contain" fallback-class="p-2 text-center text-xs text-muted" />
                <span class="flex flex-wrap items-center justify-between gap-1 p-2 text-xs text-muted">
                  <span>{{ poster.language?.toUpperCase() ?? 'Sans langue' }} · {{ poster.width }} × {{ poster.height }}</span>
                  <span v-if="safePosterUrl(poster.url) === currentUrl" class="inline-flex items-center gap-1 font-semibold text-accent"><Check :size="14" aria-hidden="true" /> Sélectionnée</span>
                </span>
              </button>
            </li>
          </ul>
        </div>
      </section>
    </div>
  </dialog>
</template>
