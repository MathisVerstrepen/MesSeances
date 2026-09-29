<script setup lang="ts">
import { Plus, X } from '@lucide/vue'
import type { WatchlistTag } from '~/types/watchlist'
import { watchlistTagStyle } from '~/utils/watchlistTags'

const props = defineProps<{
  title: string
  tags: WatchlistTag[]
  tagIds: string[]
  contextTagId?: string
  open: boolean
  blocked: boolean
  error: string
  retryBlocked: boolean
}>()
const emit = defineEmits<{
  toggle: []
  close: []
  retry: []
  assign: [tagId: string, assigned: boolean, input: HTMLInputElement]
}>()
const regionId = useId()
const trigger = useTemplateRef('trigger')
const root = useTemplateRef('root')
const panel = useTemplateRef('panel')
const options = useTemplateRef('options')
const heading = useTemplateRef('heading')
const position = ref({
  left: '0px',
  top: '0px',
  width: '320px',
  maxHeight: '320px',
})
let opening = 0
const assignedTags = computed(() =>
  props.tags.filter(
    (tag) => props.tagIds.includes(tag.id) && tag.id !== props.contextTagId,
  ),
)
function change(event: Event, tagId: string) {
  const input = event.target
  if (!(input instanceof HTMLInputElement)) return
  const requested = input.checked
  input.checked = props.tagIds.includes(tagId)
  if (!props.open || props.blocked) return
  emit('assign', tagId, requested, input)
}
function close(restoreFocus = false) {
  if (!props.open) return
  opening++
  emit('close')
  if (restoreFocus && trigger.value?.isConnected)
    trigger.value.focus({ preventScroll: true })
}

function positionPanel() {
  if (!props.open || !panel.value || !trigger.value) return
  const viewport = window.visualViewport
  const left = (viewport?.offsetLeft ?? 0) + 8
  const top = (viewport?.offsetTop ?? 0) + 8
  const width = Math.max(0, (viewport?.width ?? window.innerWidth) - 16)
  const height = Math.max(0, (viewport?.height ?? window.innerHeight) - 16)
  const anchor = trigger.value.getBoundingClientRect()
  const panelWidth = Math.min(320, width)
  const panelHeight = Math.min(320, height)
  // Measure wrapped content at the new width before choosing above or below.
  panel.value.style.width = `${panelWidth}px`
  const desired = Math.min(
    panelHeight,
    (heading.value?.offsetHeight ?? 0) + (options.value?.scrollHeight ?? 0) + 4,
  )
  const below = top + height - anchor.bottom - 8
  const above = anchor.top - top - 8
  const target =
    below >= desired || below >= above
      ? anchor.bottom + 8
      : anchor.top - desired - 8
  position.value = {
    left: `${Math.max(left, Math.min(anchor.left, left + width - panelWidth))}px`,
    top: `${Math.max(top, Math.min(target, top + height - desired))}px`,
    width: `${panelWidth}px`,
    maxHeight: `${panelHeight}px`,
  }
}

function outside(event: Event) {
  if (event.target instanceof Node && !root.value?.contains(event.target))
    close()
}
function keydown(event: Event) {
  if (!(event instanceof KeyboardEvent) || event.key !== 'Escape') return
  event.preventDefault()
  close(true)
}
function listen(active: boolean) {
  const method = active ? 'addEventListener' : 'removeEventListener'
  document[method]('pointerdown', outside)
  document[method]('focusin', outside)
  document[method]('keydown', keydown)
  window[method]('resize', positionPanel)
  window[method]('scroll', positionPanel, true)
  window.visualViewport?.[method]('resize', positionPanel)
  window.visualViewport?.[method]('scroll', positionPanel)
}
watch(
  () => props.open,
  async (open) => {
    const current = ++opening
    listen(open)
    if (!open) return
    await nextTick()
    if (current !== opening || !props.open) return
    positionPanel()
    const first = panel.value?.querySelector<HTMLInputElement>(
      'input:not(:disabled)',
    )
    ;(first ?? panel.value?.querySelector<HTMLButtonElement>('button'))?.focus({
      preventScroll: true,
    })
  },
)
watch(
  () => [props.tags, props.tagIds, props.error],
  async () => {
    await nextTick()
    positionPanel()
  },
)
onBeforeUnmount(() => {
  opening++
  listen(false)
})
</script>

<template>
  <div ref="root" class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1">
    <ul
      v-if="assignedTags.length"
      aria-label="Tags associés"
      class="flex flex-wrap gap-1.5"
    >
      <li
        v-for="tag in assignedTags"
        :key="tag.id"
        class="max-w-full rounded-md border px-2 py-0.5 text-xs font-medium [overflow-wrap:anywhere]"
        :style="watchlistTagStyle(tag.color)"
      >
        {{ tag.name }}
      </li>
    </ul>
    <button
      ref="trigger"
      type="button"
      class="inline-flex min-h-11 min-w-11 items-center gap-1 rounded-md px-2 text-sm font-semibold text-primary hover:bg-subtle focus-visible:outline-2 focus-visible:outline-offset-2 disabled:opacity-50"
      :aria-label="`Modifier les tags de ${title}`"
      :disabled="blocked && !open"
      :aria-expanded="open"
      :aria-controls="open ? regionId : undefined"
      @click="emit('toggle')"
    >
      <Plus :size="16" aria-hidden="true" />
      Tag
    </button>
    <div
      v-if="open"
      :id="regionId"
      ref="panel"
      class="fixed z-40 flex flex-col overflow-hidden rounded-lg border-2 border-ink bg-surface shadow-lg"
      :style="position"
      :aria-labelledby="`${regionId}-title`"
      :aria-busy="blocked"
      role="group"
    >
      <div
        ref="heading"
        class="flex shrink-0 items-start gap-2 border-b border-ink/20 py-1 pl-3 pr-1"
      >
        <h3
          :id="`${regionId}-title`"
          class="min-w-0 flex-1 py-3 text-sm font-semibold [overflow-wrap:anywhere] line-clamp-2"
        >
          Tags de {{ title }}
        </h3>
        <button
          type="button"
          class="flex size-11 shrink-0 items-center justify-center rounded-md hover:bg-subtle focus-visible:outline-2 focus-visible:outline-offset-[-4px]"
          aria-label="Fermer les tags"
          @click="close(true)"
        >
          <X :size="20" aria-hidden="true" />
        </button>
      </div>
      <div ref="options" class="min-h-0 overflow-y-auto overscroll-contain p-2">
        <div v-if="error" role="alert" class="account-alert mb-2">
          <p>{{ error }}</p>
          <button
            type="button"
            class="account-link min-h-11"
            :disabled="retryBlocked"
            @click="emit('retry')"
          >
            Actualiser les tags
          </button>
        </div>
        <p v-if="!tags.length" class="p-2 text-sm">
          Créez un tag dans « Gérer les tags ».
        </p>
        <label
          v-for="tag in tags"
          :key="tag.id"
          class="flex min-h-11 cursor-pointer items-center gap-3 rounded-md px-2 py-2 text-sm hover:bg-subtle has-[:disabled]:cursor-default has-[:disabled]:opacity-60"
        >
          <input
            type="checkbox"
            class="size-5 shrink-0 accent-primary focus-visible:outline-2 focus-visible:outline-offset-2"
            :checked="tagIds.includes(tag.id)"
            :disabled="blocked"
            @change="change($event, tag.id)"
          >
          <span
            class="min-w-0 rounded-md border px-2 py-0.5 [overflow-wrap:anywhere]"
            :style="watchlistTagStyle(tag.color)"
            >{{
              tag.name
            }}</span
          >
        </label>
      </div>
    </div>
  </div>
</template>
