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
const { posters, status, load, reset } = useAdminMoviePosters(
  api.adminMoviePosters,
)
const dialog = ref<HTMLDialogElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const isOpen = ref(false)
const titleId = useId()
const currentUrl = computed(() => safePosterUrl(props.currentPosterUrl))
let activeTrigger: HTMLButtonElement | null = null

async function openModal(event: MouseEvent) {
  if (
    props.disabled ||
    isOpen.value ||
    !(event.currentTarget instanceof HTMLButtonElement)
  )
    return
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
  if (restoreFocus)
    nextTick(() => {
      if (trigger?.isConnected) trigger.focus({ preventScroll: true })
    })
}

function selectPoster(url: string) {
  if (
    !isOpen.value ||
    props.disabled ||
    status.value !== 'ready' ||
    !posters.value.some((poster) => poster.url === url)
  )
    return
  emit('select', url)
  closeModal()
}

watch(
  () => props.movieId,
  () => closeModal({ restoreFocus: false }),
)
watch(
  () => props.disabled,
  (disabled) => {
    if (disabled) closeModal()
  },
)
onBeforeUnmount(() => closeModal({ restoreFocus: false }))
</script>

<template>
  <button
    type="button"
    class="editorial-button-outline min-h-8 px-2 py-1 max-sm:min-h-11"
    :disabled="disabled"
    aria-haspopup="dialog"
    :aria-controls="`${titleId}-dialog`"
    :aria-expanded="isOpen"
    @click="openModal"
  >
    <Images :size="16" aria-hidden="true" />
    Choisir sur TMDB
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
    <div
      class="flex min-h-full items-center justify-center"
      @click.self="closeModal()"
    >
      <section
        class="w-full max-w-4xl border-2 border-ink bg-surface text-ink shadow-[6px_6px_0_#27272a]"
      >
        <header
          class="sticky top-0 z-10 flex items-center justify-between gap-3 border-b-2 border-ink bg-highlight px-4 py-3 sm:px-6"
        >
          <h2 :id="titleId" class="editorial-heading min-w-0">
            Affiches TMDB - {{ movieTitle }}
          </h2>
          <button
            ref="closeButton"
            type="button"
            class="grid size-11 shrink-0 place-items-center border-2 border-ink bg-surface hover:bg-canvas"
            aria-label="Fermer les affiches"
            @click="closeModal()"
          >
            <X :size="22" aria-hidden="true" />
          </button>
        </header>
        <div class="p-4 sm:p-6" :aria-busy="status === 'loading'">
          <EditorialStatePanel
            v-if="status === 'loading'"
            semantic="status"
            size="compact"
            shadow="small"
          >
            <LoaderCircle
              :size="20"
              class="animate-spin text-accent"
              aria-hidden="true"
            />
            Chargement des affiches…
          </EditorialStatePanel>
          <EditorialStatePanel
            v-else-if="status === 'error'"
            semantic="alert"
            size="compact"
            shadow="small"
          >
            <p class="text-sm font-semibold text-primary">
              Impossible de charger les affiches TMDB.
            </p>
            <button
              type="button"
              class="editorial-button-outline mt-4"
              @click="load(movieId)"
            >
              <RefreshCw :size="16" aria-hidden="true" />
              Réessayer
            </button>
          </EditorialStatePanel>
          <EditorialStatePanel
            v-else-if="status === 'ready' && !posters.length"
            semantic="status"
            size="compact"
            shadow="small"
          >
            Aucune affiche TMDB disponible pour ce film.
          </EditorialStatePanel>
          <ul
            v-else
            class="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4"
            aria-label="Affiches disponibles"
          >
            <li v-for="(poster, index) in posters" :key="poster.url">
              <button
                type="button"
                class="h-full w-full overflow-hidden border-2 border-ink text-left focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
                :class="safePosterUrl(poster.url) === currentUrl ? 'bg-highlight shadow-[4px_4px_0_#27272a]' : 'bg-surface hover:bg-canvas'"
                :aria-pressed="safePosterUrl(poster.url) === currentUrl"
                :aria-label="`Choisir l’affiche ${index + 1}, ${poster.language ?? 'sans langue'}, ${poster.width} × ${poster.height}`"
                @click="selectPoster(poster.url)"
              >
                <PosterImage
                  :src="poster.url"
                  :alt="`Affiche ${index + 1} de ${movieTitle}`"
                  sizes="(min-width: 640px) 200px, 45vw"
                  class="aspect-[2/3] w-full bg-canvas"
                  image-class="size-full object-contain"
                  fallback-class="p-2 text-center text-xs text-muted"
                />
                <span
                  class="flex flex-wrap items-center justify-between gap-1 border-t-2 border-ink p-2 font-mono text-xs text-ink"
                >
                  <span
                    >{{ poster.language?.toUpperCase() ?? 'Sans langue' }}
                    · {{ poster.width }} × {{ poster.height }}</span
                  >
                  <span
                    v-if="safePosterUrl(poster.url) === currentUrl"
                    class="inline-flex items-center gap-1 font-bold text-ink"
                    ><Check :size="14" aria-hidden="true" />
                    Sélectionnée</span
                  >
                </span>
              </button>
            </li>
          </ul>
        </div>
      </section>
    </div>
  </dialog>
</template>
