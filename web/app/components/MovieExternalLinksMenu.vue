<script setup lang="ts">
import { ExternalLink } from '@lucide/vue'
import imdbLogo from '~/assets/imgs/IMDb_logo.svg?no-inline'
import letterboxdLogo from '~/assets/imgs/letterboxd_logo.svg?no-inline'
import tmdbLogo from '~/assets/imgs/logo_tmdb.svg?no-inline'
import type { MovieExternalLink } from '~/utils/movieExternalLinks'

const serviceLogos = {
  tmdb: tmdbLogo,
  letterboxd: letterboxdLogo,
  imdb: imdbLogo
} satisfies Record<MovieExternalLink['destination'], string>

const props = defineProps<{
  links: readonly MovieExternalLink[]
  movieTitle: string
}>()

const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const menuItems = ref<HTMLAnchorElement[]>([])
const isOpen = ref(false)
const menuId = useId()
const triggerLabel = computed(() => `Voir les liens externes de ${props.movieTitle}`)

function setMenuItem(element: Element | null, index: number) {
  if (element instanceof HTMLAnchorElement) menuItems.value[index] = element
}

async function openMenu(focus: 'first' | 'last' = 'first') {
  if (!props.links.length) return
  isOpen.value = true
  await nextTick()
  const index = focus === 'last' ? props.links.length - 1 : 0
  menuItems.value[index]?.focus()
}

function closeMenu({ restoreFocus = false } = {}) {
  if (!isOpen.value) return
  isOpen.value = false
  if (restoreFocus) nextTick(() => trigger.value?.focus())
}

function toggleMenu() {
  if (isOpen.value) closeMenu()
  else void openMenu()
}

function handleTriggerKeydown(event: KeyboardEvent) {
  if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
  event.preventDefault()
  void openMenu(event.key === 'ArrowUp' ? 'last' : 'first')
}

function focusedItemIndex(): number {
  return menuItems.value.findIndex((item) => item === document.activeElement)
}

function handleMenuKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    closeMenu({ restoreFocus: true })
    return
  }
  if (event.key === 'Tab') {
    closeMenu()
    return
  }

  const currentIndex = focusedItemIndex()
  let nextIndex: number | null = null
  if (event.key === 'ArrowDown') nextIndex = currentIndex >= props.links.length - 1 ? 0 : currentIndex + 1
  else if (event.key === 'ArrowUp') nextIndex = currentIndex <= 0 ? props.links.length - 1 : currentIndex - 1
  else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = props.links.length - 1
  if (nextIndex === null) return

  event.preventDefault()
  menuItems.value[nextIndex]?.focus()
}

function handleDocumentPointerDown(event: PointerEvent) {
  if (isOpen.value && event.target instanceof Node && !root.value?.contains(event.target)) closeMenu()
}

function handleDocumentKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !isOpen.value) return
  event.preventDefault()
  closeMenu({ restoreFocus: true })
}

function handleFocusOut(event: FocusEvent) {
  if (event.relatedTarget instanceof Node && root.value?.contains(event.relatedTarget)) return
  closeMenu()
}

onMounted(() => {
  document.addEventListener('pointerdown', handleDocumentPointerDown)
  document.addEventListener('keydown', handleDocumentKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', handleDocumentPointerDown)
  document.removeEventListener('keydown', handleDocumentKeydown)
})
</script>

<template>
  <div
    v-if="links.length"
    ref="root"
    class="absolute right-4 top-4 z-20 sm:right-6 sm:top-6 lg:right-8 lg:top-8"
    @focusout="handleFocusOut"
  >
    <button
      ref="trigger"
      type="button"
      class="grid size-11 place-items-center border-2 border-ink bg-surface text-ink shadow-[3px_3px_0_#27272a] hover:bg-highlight focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-highlight focus-visible:ring-offset-2 focus-visible:ring-offset-ink"
      :aria-label="triggerLabel"
      aria-haspopup="menu"
      :aria-controls="menuId"
      :aria-expanded="isOpen"
      @click="toggleMenu"
      @keydown="handleTriggerKeydown"
    >
      <ExternalLink :size="20" aria-hidden="true" />
    </button>

    <div
      v-if="isOpen"
      :id="menuId"
      role="menu"
      :aria-label="`Liens externes de ${movieTitle}`"
      class="absolute right-0 top-full mt-2 w-48 max-w-[calc(100vw-2rem)] border-2 border-ink bg-surface p-1 text-ink shadow-[5px_5px_0_#27272a]"
      @keydown="handleMenuKeydown"
    >
      <a
        v-for="(link, index) in links"
        :key="link.destination"
        :ref="(element) => setMenuItem(element as Element | null, index)"
        :href="link.url"
        target="_blank"
        rel="noopener noreferrer"
        role="menuitem"
        tabindex="-1"
        class="flex min-h-11 items-center justify-between gap-3 px-3 font-mono text-xs font-black uppercase tracking-[0.08em] hover:bg-[#e8e6de] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ink"
        :aria-label="`Voir ${link.label} dans un nouvel onglet`"
        @click="closeMenu({ restoreFocus: true })"
      >
        <span class="flex min-w-0 items-center gap-2">
          <span class="flex h-6 w-8 shrink-0 items-center justify-center" aria-hidden="true">
            <img :src="serviceLogos[link.destination]" alt="" class="max-h-5 max-w-8 object-contain" />
          </span>
          <span>{{ link.label }}</span>
        </span>
        <ExternalLink :size="15" aria-hidden="true" />
      </a>
    </div>
  </div>
</template>
