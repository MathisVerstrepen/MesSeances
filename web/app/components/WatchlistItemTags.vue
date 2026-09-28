<script setup lang="ts">
import type { WatchlistTag } from '~/types/watchlist'

const props = defineProps<{
  title: string
  tags: WatchlistTag[]
  tagIds: string[]
  open: boolean
  blocked: boolean
}>()
const emit = defineEmits<{
  toggle: []
  assign: [tagId: string, assigned: boolean, input: HTMLInputElement]
}>()
const regionId = useId()
const trigger = useTemplateRef('trigger')
const assignedTags = computed(() =>
  props.tags.filter((tag) => props.tagIds.includes(tag.id)),
)
function change(event: Event, tagId: string) {
  const input = event.target
  if (!(input instanceof HTMLInputElement)) return
  const requested = input.checked
  input.checked = props.tagIds.includes(tagId)
  emit('assign', tagId, requested, input)
}
function escape() {
  if (!props.open) return
  emit('toggle')
  trigger.value?.focus({ preventScroll: true })
}
</script>

<template>
  <div class="mt-2" @keydown.esc.stop.prevent="escape">
    <ul
      v-if="assignedTags.length"
      aria-label="Tags associés"
      class="mb-1 flex flex-wrap gap-1.5"
    >
      <li
        v-for="tag in assignedTags"
        :key="tag.id"
        class="max-w-full rounded-md bg-subtle px-2 py-0.5 text-xs font-medium [overflow-wrap:anywhere]"
      >
        {{ tag.name }}
      </li>
    </ul>
    <button
      ref="trigger"
      type="button"
      class="account-link min-h-11 min-w-11 text-left text-sm"
      :aria-label="`Modifier les tags de ${title}`"
      :disabled="blocked"
      :aria-expanded="open"
      :aria-controls="open ? regionId : undefined"
      @click="emit('toggle')"
    >
      Modifier les tags
    </button>
    <div
      v-if="open"
      :id="regionId"
      class="mt-1 max-h-64 overflow-y-auto overscroll-contain border-t border-ink/20"
      :aria-label="`Tags de ${title}`"
      role="group"
    >
      <p v-if="!tags.length" class="text-sm">
        Créez un tag dans « Gérer les tags ».
      </p>
      <label
        v-for="tag in tags"
        :key="tag.id"
        class="flex min-h-11 cursor-pointer items-center gap-3 py-2 text-sm"
      >
        <input
          type="checkbox"
          class="size-5 shrink-0 accent-primary focus-visible:outline-2 focus-visible:outline-offset-2"
          :checked="tagIds.includes(tag.id)"
          :disabled="blocked"
          @change="change($event, tag.id)"
        >
        <span class="min-w-0 break-words">{{ tag.name }}</span>
      </label>
    </div>
  </div>
</template>
