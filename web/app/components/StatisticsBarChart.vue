<script setup lang="ts">
import { statisticsBars, statisticsCount, type StatisticsBar } from '~/utils/statistics'

const props = defineProps<{ rows: StatisticsBar[]; label: string; total?: number; unit: 'séances' | 'films' | 'cinémas'; ordered?: boolean }>()
const bars = computed(() => statisticsBars(props.rows, props.total))
</script>

<template>
  <div>
    <p v-if="bars.length === 0" class="py-6 text-sm font-semibold">Aucune donnée pour ces filtres.</p>
    <component :is="ordered ? 'ol' : 'ul'" v-else class="space-y-5" :aria-label="label">
      <li v-for="(row, index) in bars" :key="row.value" class="min-w-0">
        <div class="mb-2 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 text-sm">
          <span class="min-w-0 break-words font-extrabold">
            <span v-if="ordered" class="mr-2 font-mono text-xs" aria-hidden="true">{{ String(index + 1).padStart(2, '0') }}</span>
            <NuxtLink v-if="row.href" :to="row.href" class="underline decoration-2 underline-offset-4 hover:text-primary focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink">{{ row.label }}</NuxtLink>
            <template v-else>{{ row.label }}</template>
          </span>
          <span class="font-mono text-xs tabular-nums">{{ statisticsCount(row.count) }} {{ unit }}<template v-if="row.share !== undefined"> · {{ row.share }}</template></span>
        </div>
        <div class="h-3 bg-ink/10" aria-hidden="true"><div class="h-full bg-ink" :style="{ width: `${row.width}%` }"></div></div>
        <p v-if="row.detail" class="mt-1 text-xs">{{ row.detail }}</p>
      </li>
    </component>
  </div>
</template>
