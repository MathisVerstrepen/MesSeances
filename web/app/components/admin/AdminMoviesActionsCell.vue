<script setup lang="ts">
import type { ICellRendererParams } from 'ag-grid-community'
import type { AdminMovieItem } from '~/types/api'

interface ActionsCellContext {
  detailsId: (item: AdminMovieItem) => string
  isDetailsOpen: (item: AdminMovieItem) => boolean
  isDirty: (item: AdminMovieItem) => boolean
  isPending: (item: AdminMovieItem) => boolean
  rowError: (item: AdminMovieItem) => string
  toggleDetails: (item: AdminMovieItem) => void
  saveMovie: (item: AdminMovieItem) => void
  cancelMovie: (item: AdminMovieItem) => void
}

const props = defineProps<{
  params: ICellRendererParams<AdminMovieItem> & { context: ActionsCellContext }
}>()
const item = computed(() => props.params.data)

function run(event: MouseEvent, action: (item: AdminMovieItem) => void) {
  event.stopPropagation()
  if (item.value) action(item.value)
}
</script>

<template>
  <div v-if="item" class="flex h-full items-center gap-1.5">
    <button
      type="button"
      class="inline-flex min-h-8 items-center border-2 border-ink bg-surface px-2 py-1 font-mono text-xs font-bold text-ink hover:bg-highlight"
      :aria-expanded="params.context.isDetailsOpen(item)"
      :aria-controls="params.context.detailsId(item)"
      @click="run($event, params.context.toggleDetails)"
    >
      Détails
    </button>
    <button
      type="button"
      class="inline-flex min-h-8 items-center border-2 border-ink bg-ink px-2 py-1 font-mono text-xs font-bold text-white enabled:hover:bg-primary disabled:cursor-not-allowed disabled:opacity-40"
      :disabled="!params.context.isDirty(item) || params.context.isPending(item)"
      @click="run($event, params.context.saveMovie)"
    >
      {{ params.context.isPending(item) ? 'Enregistrement…' : 'Enregistrer' }}
    </button>
    <button
      type="button"
      class="inline-flex min-h-8 items-center border-2 border-ink bg-surface px-2 py-1 font-mono text-xs font-bold text-ink enabled:hover:bg-highlight disabled:cursor-not-allowed disabled:opacity-40"
      :disabled="!params.context.isDirty(item) || params.context.isPending(item)"
      @click="run($event, params.context.cancelMovie)"
    >
      Annuler
    </button>
    <span
      v-if="params.context.rowError(item)"
      class="text-xs font-bold text-primary"
      :title="params.context.rowError(item)"
      role="alert"
      >Erreur</span
    >
  </div>
</template>
