<script setup lang="ts">
import type { HistoryOptionKind } from '~/types/api'
import { statisticsMaxSelections, type StatisticsSelectOption } from '~/utils/statistics'
import { createHistoryOptionsRequest, historySelectionOptions } from '~/utils/statisticsHistory'

const props = defineProps<{
  id: string
  label: string
  allLabel: string
  kind: HistoryOptionKind
  modelValue: string[]
  options: readonly StatisticsSelectOption[]
  hasMore?: boolean
  single?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const api = useMesSeancesApi()
const items = shallowRef<readonly StatisticsSelectOption[]>(props.options)
const known = shallowRef<readonly StatisticsSelectOption[]>([])
const resolved = shallowRef<ReadonlySet<string>>(new Set())
const pending = ref(false)
const error = ref('')
const hasMore = ref(props.hasMore ?? false)
const search = ref('')
let requestedSelections: string[] = []
const request = createHistoryOptionsRequest({
  start() { pending.value = true; error.value = '' },
  success(result) {
    items.value = result.items
    known.value = result.selected
    resolved.value = new Set(requestedSelections)
    hasMore.value = result.has_more
  },
  error() { error.value = 'Impossible de charger les options. Vos sélections sont conservées.' },
  finish() { pending.value = false }
})
const options = computed(() => historySelectionOptions(items.value, [...props.options, ...known.value], props.modelValue, resolved.value))
function load(immediate = false) {
  const selected = [...new Set(props.modelValue)].slice(0, statisticsMaxSelections)
  const q = search.value.trim() || undefined
  void request.schedule(signal => {
    requestedSelections = selected
    return api.historyStatisticsOptions({ kind: props.kind, q, selected }, signal)
  }, immediate)
}
function onSearch(value: string) { search.value = value; load() }
// Single-choice bindings create arrays during parent renders. Only changed values need lookup.
watch(() => JSON.stringify(props.modelValue), () => {
  // Retain labels for a newly checked search result before replacing the search inventory.
  known.value = options.value.filter(option => props.modelValue.includes(option.value))
  load()
})
watch(() => props.options, value => { if (!search.value) items.value = value })
watch(() => props.hasMore, value => { if (!search.value) hasMore.value = value ?? false })
onMounted(() => { if (props.modelValue.length) load(true) })
onBeforeUnmount(() => request.cancel())
</script>

<template>
  <StatisticsMultiSelect :id="id" :model-value="modelValue" :label="label" :all-label="allLabel" :options="options" :max-selections="statisticsMaxSelections" :single="single" external-search @update:model-value="emit('update:modelValue', $event)" @search="onSearch" @open="load(true)">
    <template #feedback>
      <p v-if="pending" role="status" class="mb-2 text-sm">Chargement des options…</p>
      <div v-if="error" role="alert" class="mb-2 text-sm">
        <p>{{ error }}</p>
        <button type="button" class="min-h-11 px-2 font-extrabold underline focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink" @click="load(true)">Réessayer</button>
      </div>
      <p v-if="hasMore" class="mb-2 text-sm font-bold">Affinez la recherche pour voir les autres options.</p>
    </template>
  </StatisticsMultiSelect>
</template>
