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

const props = defineProps<{ params: ICellRendererParams<AdminMovieItem> & { context: ActionsCellContext } }>()
const item = computed(() => props.params.data)

function run(event: MouseEvent, action: (item: AdminMovieItem) => void) {
  event.stopPropagation()
  if (item.value) action(item.value)
}
</script>

<template>
  <div v-if="item" class="flex h-full items-center gap-1.5">
    <button type="button" class="rounded border border-line bg-surface px-2 py-1 text-xs font-semibold text-ink hover:border-line-hover" :aria-expanded="params.context.isDetailsOpen(item)" :aria-controls="params.context.detailsId(item)" @click="run($event, params.context.toggleDetails)">Détails</button>
    <button type="button" class="rounded bg-primary px-2 py-1 text-xs font-semibold text-white disabled:opacity-40" :disabled="!params.context.isDirty(item) || params.context.isPending(item)" @click="run($event, params.context.saveMovie)">{{ params.context.isPending(item) ? 'Enregistrement…' : 'Enregistrer' }}</button>
    <button type="button" class="rounded px-2 py-1 text-xs font-semibold text-muted hover:bg-subtle disabled:opacity-40" :disabled="!params.context.isDirty(item) || params.context.isPending(item)" @click="run($event, params.context.cancelMovie)">Annuler</button>
    <span v-if="params.context.rowError(item)" class="text-xs font-bold text-red-700" :title="params.context.rowError(item)" role="alert">Erreur</span>
  </div>
</template>
