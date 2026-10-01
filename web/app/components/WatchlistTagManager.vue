<script setup lang="ts">
import { Pencil, Plus, Tags, Trash2, X } from '@lucide/vue'
import type { WatchlistTag, WatchlistTagColor } from '~/types/watchlist'
import {
  sortWatchlistTags,
  tagNameError,
  watchlistTagStyle,
} from '~/utils/watchlistTags'

const props = defineProps<{
  tags: WatchlistTag[]
  ready: boolean
  blocked: boolean
  compactTrigger?: boolean
}>()
const watchlist = useWatchlist()
const open = ref(false)
const creating = ref(false)
const name = ref('')
const color = ref<WatchlistTagColor>('neutral')
const nameError = ref('')
const editDraft = ref('')
const editColor = ref<WatchlistTagColor>('neutral')
const editError = ref('')
const target = ref<(WatchlistTag & { action: 'update' | 'delete' }) | null>(
  null,
)
const pending = ref<'create' | 'update' | 'delete' | null>(null)
const trigger = useTemplateRef('trigger')
const dialog = useTemplateRef('dialog')
const closeButton = useTemplateRef('closeButton')
const createButton = useTemplateRef('createButton')
const panelHeight = ref(0)
const panelTop = ref(0)
let bodyOverflow: string | null = null
let targetOpener: HTMLButtonElement | null = null
const tags = computed(() => sortWatchlistTags(props.tags))
let active = true
let interaction = 0
let createEdit = 0
let targetEdit = 0
let lifetime = 0
let focusInteraction = 0
watch([name, color], () => createEdit++, { flush: 'sync' })
watch([editDraft, editColor], () => targetEdit++, { flush: 'sync' })
function interact() {
  focusInteraction++
}

function positionPanel() {
  panelHeight.value = window.visualViewport?.height ?? window.innerHeight
  panelTop.value = window.visualViewport?.offsetTop ?? 0
}

function lockScroll() {
  if (bodyOverflow !== null) return
  bodyOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
}

function unlockScroll() {
  if (bodyOverflow === null) return
  document.body.style.overflow = bodyOverflow
  bodyOverflow = null
}

function backdrop(event: MouseEvent) {
  const modal = event.currentTarget
  if (!(modal instanceof HTMLDialogElement) || event.target !== modal) return
  const rect = modal.getBoundingClientRect()
  if (
    event.clientX < rect.left ||
    event.clientX > rect.right ||
    event.clientY < rect.top ||
    event.clientY > rect.bottom
  )
    closeModal()
}

async function openModal() {
  if (!props.ready || open.value) return
  const scope = lifetime
  open.value = true
  await nextTick()
  if (!active || scope !== lifetime || !open.value || !dialog.value) return
  positionPanel()
  dialog.value.showModal()
  lockScroll()
  closeButton.value?.focus({ preventScroll: true })
}

function closeModal() {
  if (!open.value) return
  const scope = lifetime
  const focusVersion = focusInteraction
  open.value = false
  creating.value = false
  cancel()
  dialog.value?.close()
  unlockScroll()
  void nextTick(() => {
    if (
      active &&
      scope === lifetime &&
      focusVersion === focusInteraction &&
      !open.value &&
      trigger.value?.isConnected &&
      !trigger.value.disabled
    )
      trigger.value.focus({ preventScroll: true })
  })
}

function clearPrivate() {
  lifetime++
  interaction++
  open.value = false
  creating.value = false
  targetOpener = null
  name.value = ''
  color.value = 'neutral'
  nameError.value = ''
  editDraft.value = ''
  editColor.value = 'neutral'
  editError.value = ''
  target.value = null
  pending.value = null
  unlockScroll()
}
watch(watchlist.scopeKey, clearPrivate, { flush: 'sync' })
onMounted(() => {
  window.addEventListener('pagehide', clearPrivate)
  document.addEventListener('pointerdown', interact)
  document.addEventListener('keydown', interact)
  window.addEventListener('resize', positionPanel)
  window.addEventListener('scroll', positionPanel, true)
  window.visualViewport?.addEventListener('resize', positionPanel)
  window.visualViewport?.addEventListener('scroll', positionPanel)
})

function focusAfterRender(resolve: () => HTMLElement | null | undefined) {
  const scope = lifetime
  const current = interaction
  const focusVersion = focusInteraction
  void nextTick(() => {
    if (
      !active ||
      !open.value ||
      scope !== lifetime ||
      current !== interaction ||
      focusVersion !== focusInteraction
    )
      return
    const element = resolve()
    if (element?.isConnected && !element.matches(':disabled')) element.focus()
  })
}

function openCreate() {
  if (props.blocked || pending.value) return
  cancel()
  creating.value = true
  focusAfterRender(() => dialog.value?.querySelector('#watchlist-tag-name'))
}

function cancelCreate() {
  interaction++
  creating.value = false
  focusAfterRender(() =>
    createButton.value?.disabled ? closeButton.value : createButton.value,
  )
}

function cancelAndFocus() {
  const opener = targetOpener
  cancel()
  focusAfterRender(() =>
    opener?.isConnected && !opener.disabled ? opener : closeButton.value,
  )
}

function cancel() {
  interaction++
  target.value = null
  targetOpener = null
  editError.value = ''
}

function edit(tag: WatchlistTag, action: 'update' | 'delete', event?: Event) {
  if (props.blocked || pending.value) return
  cancel()
  creating.value = false
  targetOpener =
    event?.currentTarget instanceof HTMLButtonElement
      ? event.currentTarget
      : null
  target.value = { ...tag, action }
  editDraft.value = tag.name
  editColor.value = tag.color
  focusAfterRender(() =>
    dialog.value?.querySelector(
      action === 'update'
        ? '#watchlist-tag-edit'
        : '#watchlist-tag-delete-cancel',
    ),
  )
}

function staleTarget() {
  if (
    target.value &&
    !props.tags.some(
      (tag) =>
        tag.id === target.value?.id &&
        tag.name === target.value.name &&
        tag.color === target.value.color,
    )
  )
    cancel()
}
watch(
  () => props.tags,
  () => {
    if (!pending.value) staleTarget()
  },
  { flush: 'sync' },
)

async function create() {
  if (props.blocked || pending.value) return
  nameError.value = tagNameError(name.value)
  if (nameError.value) return
  const submitted = name.value
  const submittedColor = color.value
  const scope = lifetime
  const current = interaction
  const focusVersion = focusInteraction
  const focused = document.activeElement
  const edit = createEdit
  pending.value = 'create'
  const success = await watchlist.createTag(submitted, submittedColor)
  if (!active || scope !== lifetime) return
  pending.value = null
  if (
    success &&
    current === interaction &&
    edit === createEdit &&
    name.value === submitted &&
    color.value === submittedColor
  ) {
    name.value = ''
    color.value = 'neutral'
    creating.value = false
    if (focusVersion === focusInteraction)
      focusAfterRender(() =>
        focused &&
        !focused.isConnected &&
        (document.activeElement === document.body ||
          document.activeElement === dialog.value)
          ? createButton.value
          : null,
      )
  }
  staleTarget()
}

async function submitTarget() {
  const selected = target.value
  const opener = targetOpener
  if (!selected || props.blocked || pending.value) return
  if (selected.action === 'update') {
    editError.value = tagNameError(editDraft.value)
    if (editError.value) return
  }
  const current = interaction
  const scope = lifetime
  const focusVersion = focusInteraction
  const focused = document.activeElement
  const draft = editDraft.value
  const draftColor = editColor.value
  const edit = targetEdit
  pending.value = selected.action
  const success =
    selected.action === 'update'
      ? await watchlist.updateTag(selected.id, draft, draftColor)
      : await watchlist.deleteTag(selected.id)
  if (!active || scope !== lifetime) return
  pending.value = null
  if (current !== interaction) return
  if (
    success &&
    edit === targetEdit &&
    draft === editDraft.value &&
    draftColor === editColor.value
  ) {
    cancel()
    editDraft.value = ''
    editColor.value = 'neutral'
  } else if (success && selected.action === 'update') {
    // A newer same-owner draft survives our own commit, rebased to its snapshot.
    const committed = props.tags.find((tag) => tag.id === selected.id)
    if (committed) target.value = { ...committed, action: 'update' }
    else cancel()
  } else staleTarget()
  await nextTick()
  if (
    active &&
    open.value &&
    scope === lifetime &&
    focusVersion === focusInteraction &&
    focused &&
    !focused.isConnected &&
    (document.activeElement === document.body ||
      document.activeElement === dialog.value)
  )
    (opener?.isConnected && !opener.disabled
      ? opener
      : closeButton.value
    )?.focus({ preventScroll: true })
}

onBeforeUnmount(() => {
  active = false
  clearPrivate()
  window.removeEventListener('pagehide', clearPrivate)
  document.removeEventListener('pointerdown', interact)
  document.removeEventListener('keydown', interact)
  window.removeEventListener('resize', positionPanel)
  window.removeEventListener('scroll', positionPanel, true)
  window.visualViewport?.removeEventListener('resize', positionPanel)
  window.visualViewport?.removeEventListener('scroll', positionPanel)
})
</script>

<template>
  <div class="min-w-0">
    <button
      ref="trigger"
      type="button"
      class="account-secondary inline-flex min-h-11 w-full items-center justify-center gap-2 whitespace-nowrap max-sm:px-2! max-sm:font-sans! max-sm:text-xs! max-sm:font-semibold! max-sm:normal-case! max-sm:tracking-normal! sm:w-auto"
      :disabled="!ready"
      :aria-expanded="open"
      aria-haspopup="dialog"
      aria-controls="watchlist-tag-manager"
      @click="openModal"
    >
      <Tags :size="18" aria-hidden="true" />
      {{ compactTrigger ? 'Tags' : 'Gérer les tags' }}
    </button>
    <dialog
      v-if="open"
      id="watchlist-tag-manager"
      ref="dialog"
      class="m-auto flex w-[calc(100%-2rem)] max-w-2xl flex-col overflow-hidden border-2 border-ink bg-surface p-0 text-ink shadow-lg backdrop:bg-black/60"
      :style="{ maxHeight: panelHeight ? `${panelHeight - 32}px` : 'calc(100dvh - 2rem)', top: `${panelTop + panelHeight / 2}px`, bottom: 'auto', transform: 'translateY(-50%)' }"
      aria-labelledby="watchlist-tag-manager-title"
      @cancel.prevent="closeModal"
      @click="backdrop"
      @close="closeModal"
    >
      <header
        class="flex shrink-0 items-center justify-between gap-3 border-b border-ink/20 p-4"
      >
        <h2 id="watchlist-tag-manager-title" class="account-heading">
          Gérer les tags
        </h2>
        <button
          ref="closeButton"
          type="button"
          class="flex size-11 shrink-0 items-center justify-center hover:bg-subtle"
          aria-label="Fermer la gestion des tags"
          @click="closeModal"
        >
          <X :size="20" aria-hidden="true" />
        </button>
      </header>
      <div
        class="min-h-0 overflow-y-auto overscroll-contain p-4"
        :aria-busy="!!pending"
      >
        <div
          v-if="watchlist.error.value"
          role="alert"
          class="account-alert mb-4"
        >
          <p>{{ watchlist.error.value }}</p>
          <button
            type="button"
            class="account-link mt-2 min-h-11"
            :disabled="!!pending || watchlist.saving.value"
            @click="watchlist.retry"
          >
            Réessayer
          </button>
        </div>
        <button
          ref="createButton"
          type="button"
          class="account-secondary inline-flex min-h-11 items-center gap-2"
          :disabled="blocked || !!pending"
          :aria-expanded="creating"
          aria-controls="watchlist-tag-create"
          @click="openCreate"
        >
          <Plus :size="18" aria-hidden="true" />
          Créer un tag
        </button>
        <form
          v-if="creating"
          id="watchlist-tag-create"
          class="mt-4"
          @submit.prevent="create"
        >
          <label for="watchlist-tag-name" class="account-label"
            >Nom du tag</label
          >
          <div class="flex flex-wrap gap-3">
            <input
              id="watchlist-tag-name"
              v-model="name"
              class="account-input min-w-0 flex-1"
              autocomplete="off"
              :aria-invalid="!!nameError"
              :aria-describedby="nameError ? 'watchlist-tag-name-error' : undefined"
              @blur="nameError = name ? tagNameError(name) : ''"
            >
          </div>
          <p
            v-if="nameError"
            id="watchlist-tag-name-error"
            role="alert"
            class="mt-2 text-sm text-primary"
          >
            {{ nameError }}
          </p>
          <WatchlistTagColorPicker v-model="color" />
          <div class="mt-3 flex flex-wrap gap-3">
            <button
              type="submit"
              class="account-primary min-h-11"
              :disabled="blocked || !!pending"
            >
              {{ pending === 'create' ? 'Création…' : 'Créer' }}
            </button>
            <button
              type="button"
              class="account-link min-h-11"
              @click="cancelCreate"
            >
              Annuler
            </button>
          </div>
        </form>
        <p v-if="!tags.length" class="mt-3 text-sm">Aucun tag.</p>
        <ul
          v-else
          class="mt-4 divide-y divide-ink/20 border-t border-ink/20 pt-2"
        >
          <li v-for="tag in tags" :key="tag.id" class="py-2">
            <div class="flex items-center gap-2">
              <div class="min-w-0 flex-1">
                <span
                  class="inline-block max-w-full rounded-md border px-2 py-0.5 text-sm font-medium [overflow-wrap:anywhere]"
                  :style="watchlistTagStyle(tag.color)"
                  >{{
                    tag.name
                  }}</span
                >
              </div>
              <button
                type="button"
                class="flex size-11 shrink-0 items-center justify-center hover:bg-subtle focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink disabled:opacity-50"
                :aria-label="`Modifier ${tag.name}`"
                :disabled="blocked || !!pending"
                :aria-expanded="target?.id === tag.id && target.action === 'update'"
                @click="edit(tag, 'update', $event)"
              >
                <Pencil :size="20" aria-hidden="true" focusable="false" />
              </button>
              <button
                type="button"
                class="flex size-11 shrink-0 items-center justify-center hover:bg-subtle focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ink disabled:opacity-50"
                :aria-label="`Supprimer ${tag.name}`"
                :disabled="blocked || !!pending"
                :aria-expanded="target?.id === tag.id && target.action === 'delete'"
                @click="edit(tag, 'delete', $event)"
              >
                <Trash2 :size="20" aria-hidden="true" focusable="false" />
              </button>
            </div>
            <form
              v-if="target?.id === tag.id && target.action === 'update'"
              class="mt-2 max-w-lg"
              @submit.prevent="submitTarget"
            >
              <label for="watchlist-tag-edit" class="account-label"
                >Nom du tag</label
              >
              <input
                id="watchlist-tag-edit"
                v-model="editDraft"
                class="account-input w-full"
                autocomplete="off"
                :aria-invalid="!!editError"
                :aria-describedby="editError ? 'watchlist-tag-edit-error' : undefined"
                @blur="editError = tagNameError(editDraft)"
              >
              <p
                v-if="editError"
                id="watchlist-tag-edit-error"
                role="alert"
                class="mt-2 text-sm text-primary"
              >
                {{ editError }}
              </p>
              <WatchlistTagColorPicker v-model="editColor" />
              <div class="mt-3 flex flex-wrap gap-3">
                <button
                  type="submit"
                  class="account-primary min-h-11"
                  :disabled="blocked || !!pending"
                >
                  {{ pending === 'update' ? 'Enregistrement…' : 'Enregistrer' }}
                </button>
                <button
                  type="button"
                  class="account-link min-h-11"
                  @click="cancelAndFocus"
                >
                  Annuler
                </button>
              </div>
            </form>
            <div
              v-else-if="target?.id === tag.id && target.action === 'delete'"
              class="mt-2"
            >
              <p class="text-sm">
                Ce tag sera retiré des films. Les films seront conservés.
              </p>
              <div class="mt-3 flex flex-wrap gap-3">
                <button
                  type="button"
                  class="account-secondary"
                  :disabled="blocked || !!pending"
                  @click="submitTarget"
                >
                  {{
                    pending === 'delete' ? 'Suppression…' : 'Supprimer le tag'
                  }}
                </button>
                <button
                  id="watchlist-tag-delete-cancel"
                  type="button"
                  class="account-link min-h-11"
                  @click="cancelAndFocus"
                >
                  Annuler
                </button>
              </div>
            </div>
          </li>
        </ul>
      </div>
    </dialog>
  </div>
</template>
