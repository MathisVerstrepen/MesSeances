<script setup lang="ts">
import type { ICellRendererParams } from 'ag-grid-community'
import type { AdminMovieField, AdminMovieItem } from '~/types/api'
import type { AdminMovieDraftValue } from '~/utils/adminMovies'

interface MetadataCellContext {
  formatFieldValue: (field: AdminMovieField, value: AdminMovieDraftValue) => string
  isFieldOverridden: (item: AdminMovieItem, field: AdminMovieField) => boolean
  restoreField: (item: AdminMovieItem, field: AdminMovieField) => void
}

const props = defineProps<{
  params: ICellRendererParams<AdminMovieItem> & { field: AdminMovieField, context: MetadataCellContext }
}>()

const item = computed(() => props.params.data)
const overridden = computed(() => item.value ? props.params.context.isFieldOverridden(item.value, props.params.field) : false)
// SAFETY: Grid value getters for metadata columns return AdminMovieDraftValue.
const displayValue = computed(() => props.params.context.formatFieldValue(props.params.field, props.params.value as AdminMovieDraftValue))

function restore(event: MouseEvent) {
  event.stopPropagation()
  if (item.value) props.params.context.restoreField(item.value, props.params.field)
}
</script>

<template>
  <div v-if="item" class="flex h-full min-w-0 items-center gap-2">
    <span v-if="overridden" class="size-2 shrink-0 rounded-full bg-primary" title="Valeur manuelle"><span class="sr-only">Valeur manuelle</span></span>
    <span class="min-w-0 flex-1 truncate" :class="!displayValue ? 'text-muted' : ''">{{ displayValue || 'Non renseigné' }}</span>
    <button v-if="overridden" type="button" class="shrink-0 rounded px-1.5 py-1 text-xs font-semibold text-accent hover:bg-accent-soft" :title="`Valeur automatique : ${params.context.formatFieldValue(params.field, item.automatic[params.field]) || 'non renseignée'}`" @click="restore">
      Restaurer
    </button>
  </div>
</template>
