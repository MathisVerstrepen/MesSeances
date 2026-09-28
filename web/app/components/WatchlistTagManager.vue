<script setup lang="ts">
import { Tags, X } from '@lucide/vue'
import type { WatchlistTag } from '~/types/watchlist'
import { sortWatchlistTags, tagNameError } from '~/utils/watchlistTags'

const props = defineProps<{
  tags: WatchlistTag[]
  ready: boolean
  blocked: boolean
}>()
const watchlist = useWatchlist()
const open = ref(false)
const name = ref('')
const nameError = ref('')
const renameDraft = ref('')
const renameError = ref('')
const target = ref<{
  id: string
  name: string
  action: 'rename' | 'delete'
} | null>(null)
const pending = ref<'create' | 'rename' | 'delete' | null>(null)
const trigger = useTemplateRef('trigger')
const dialog = useTemplateRef('dialog')
const closeButton = useTemplateRef('closeButton')
const tags = computed(() => sortWatchlistTags(props.tags))
let active = true
let interaction = 0
let createEdit = 0
let renameEdit = 0
let lifetime = 0
let focusInteraction = 0
function interact() {
  focusInteraction++
}

async function openModal() {
  if (!props.ready || open.value) return
  const scope = lifetime
  open.value = true
  await nextTick()
  if (!active || scope !== lifetime || !open.value || !dialog.value) return
  dialog.value.showModal()
  closeButton.value?.focus({ preventScroll: true })
}

function closeModal() {
  if (!open.value) return
  const scope = lifetime
  const focusVersion = focusInteraction
  open.value = false
  dialog.value?.close()
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
  name.value = ''
  nameError.value = ''
  renameDraft.value = ''
  renameError.value = ''
  target.value = null
  pending.value = null
}
watch(watchlist.scopeKey, clearPrivate, { flush: 'sync' })
onMounted(() => {
  window.addEventListener('pagehide', clearPrivate)
  document.addEventListener('pointerdown', interact)
  document.addEventListener('keydown', interact)
})

function cancelAndFocus() {
  cancel()
  closeButton.value?.focus({ preventScroll: true })
}

function cancel() {
  interaction++
  target.value = null
  renameError.value = ''
}

function edit(tag: WatchlistTag, action: 'rename' | 'delete') {
  cancel()
  target.value = { ...tag, action }
  renameDraft.value = tag.name
}

function staleTarget() {
  if (
    target.value &&
    !props.tags.some(
      (tag) => tag.id === target.value?.id && tag.name === target.value.name,
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
  const scope = lifetime
  const edit = createEdit
  pending.value = 'create'
  const success = await watchlist.createTag(submitted)
  if (!active || scope !== lifetime) return
  pending.value = null
  if (success && edit === createEdit && name.value === submitted)
    name.value = ''
  staleTarget()
}

async function submitTarget() {
  const selected = target.value
  if (!selected || props.blocked || pending.value) return
  if (selected.action === 'rename') {
    renameError.value = tagNameError(renameDraft.value)
    if (renameError.value) return
  }
  const current = interaction
  const scope = lifetime
  const focusVersion = focusInteraction
  const focused = document.activeElement
  const draft = renameDraft.value
  const edit = renameEdit
  pending.value = selected.action
  const success =
    selected.action === 'rename'
      ? await watchlist.renameTag(selected.id, draft)
      : await watchlist.deleteTag(selected.id)
  if (!active || scope !== lifetime) return
  pending.value = null
  if (current !== interaction) return
  if (success && edit === renameEdit && draft === renameDraft.value) {
    cancel()
    renameDraft.value = ''
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
    closeButton.value?.focus({ preventScroll: true })
}

onBeforeUnmount(() => {
  active = false
  clearPrivate()
  window.removeEventListener('pagehide', clearPrivate)
  document.removeEventListener('pointerdown', interact)
  document.removeEventListener('keydown', interact)
})
</script>

<template>
  <div class="min-w-0">
    <button
      ref="trigger"
      type="button"
      class="account-secondary inline-flex min-h-11 w-full items-center justify-center gap-2 sm:w-auto"
      :disabled="!ready"
      :aria-expanded="open"
      aria-haspopup="dialog"
      aria-controls="watchlist-tag-manager"
      @click="openModal"
    >
      <Tags :size="18" aria-hidden="true" />
      Gérer les tags
    </button>
    <dialog
      v-if="open"
      id="watchlist-tag-manager"
      ref="dialog"
      class="m-0 box-border h-dvh max-h-none w-screen max-w-none overflow-hidden border-0 bg-black/60 p-3 backdrop:bg-transparent sm:p-6"
      aria-labelledby="watchlist-tag-manager-title"
      @cancel.prevent="closeModal"
      @click.self="closeModal"
      @close="closeModal"
    >
      <div
        class="flex h-full items-center justify-center"
        @click.self="closeModal"
      >
        <section
          class="flex max-h-full w-full max-w-xl flex-col overflow-hidden rounded-lg bg-surface text-ink shadow-xl"
        >
          <header
            class="flex shrink-0 items-center justify-between gap-3 border-b border-ink/20 px-4 py-3 sm:px-6"
          >
            <h2 id="watchlist-tag-manager-title" class="text-lg font-bold">
              Gérer les tags
            </h2>
            <button
              ref="closeButton"
              type="button"
              class="flex size-11 shrink-0 items-center justify-center rounded-md hover:bg-subtle focus-visible:outline-2 focus-visible:outline-offset-2"
              aria-label="Fermer la gestion des tags"
              @click="closeModal"
            >
              <X :size="22" aria-hidden="true" />
            </button>
          </header>
          <div
            class="min-h-0 overflow-y-auto overscroll-contain p-4 sm:p-6"
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
            <form class="max-w-lg" @submit.prevent="create">
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
                  @input="createEdit++"
                  @blur="nameError = name ? tagNameError(name) : ''"
                >
                <button
                  type="submit"
                  class="account-secondary"
                  :disabled="blocked || !!pending"
                >
                  {{ pending === 'create' ? 'Création…' : 'Créer' }}
                </button>
              </div>
              <p
                v-if="nameError"
                id="watchlist-tag-name-error"
                role="alert"
                class="mt-2 text-sm text-primary"
              >
                {{ nameError }}
              </p>
            </form>
            <p v-if="!tags.length" class="mt-3 text-sm">Aucun tag.</p>
            <ul v-else class="mt-6">
              <li
                v-for="tag in tags"
                :key="tag.id"
                class="border-t border-ink/20 py-2"
              >
                <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
                  <span
                    class="min-w-0 basis-full font-medium [overflow-wrap:anywhere] sm:flex-1 sm:basis-auto"
                    >{{
                      tag.name
                    }}</span
                  >
                  <button
                    type="button"
                    class="account-link min-h-11"
                    :aria-label="`Renommer ${tag.name}`"
                    :disabled="blocked || !!pending"
                    @click="edit(tag, 'rename')"
                  >
                    Renommer
                  </button>
                  <button
                    type="button"
                    class="account-link min-h-11"
                    :aria-label="`Supprimer ${tag.name}`"
                    :disabled="blocked || !!pending"
                    @click="edit(tag, 'delete')"
                  >
                    Supprimer
                  </button>
                </div>
                <form
                  v-if="target?.id === tag.id && target.action === 'rename'"
                  class="mt-2 max-w-lg"
                  @submit.prevent="submitTarget"
                >
                  <label for="watchlist-tag-rename" class="account-label"
                    >Nouveau nom du tag</label
                  >
                  <input
                    id="watchlist-tag-rename"
                    v-model="renameDraft"
                    class="account-input w-full"
                    autocomplete="off"
                    :aria-invalid="!!renameError"
                    :aria-describedby="renameError ? 'watchlist-tag-rename-error' : undefined"
                    @input="renameEdit++"
                    @blur="renameError = tagNameError(renameDraft)"
                  >
                  <p
                    v-if="renameError"
                    id="watchlist-tag-rename-error"
                    role="alert"
                    class="mt-2 text-sm text-primary"
                  >
                    {{ renameError }}
                  </p>
                  <div class="mt-3 flex flex-wrap gap-3">
                    <button
                      type="submit"
                      class="account-secondary"
                      :disabled="blocked || !!pending"
                    >
                      {{
                        pending === 'rename' ? 'Enregistrement…' : 'Enregistrer'
                      }}
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
        </section>
      </div>
    </dialog>
  </div>
</template>
