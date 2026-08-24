<script setup lang="ts">
interface BreadcrumbItem {
  label: string
  to?: string
}

withDefaults(defineProps<{
  items: BreadcrumbItem[]
  variant?: 'detail' | 'film'
}>(), {
  variant: 'detail'
})
</script>

<template>
  <nav
    class="breadcrumbs"
    :class="variant === 'film' ? 'breadcrumbs--film' : undefined"
    aria-label="Fil d’Ariane"
  >
    <ol class="flex flex-wrap items-center gap-2">
      <template v-for="(item, index) in items" :key="`${item.label}-${index}`">
        <li v-if="index === items.length - 1" aria-current="page">{{ item.label }}</li>
        <li v-else>
          <NuxtLink v-if="item.to" :to="item.to">{{ item.label }}</NuxtLink>
          <span v-else>{{ item.label }}</span>
        </li>
        <li v-if="index < items.length - 1" aria-hidden="true">/</li>
      </template>
    </ol>
  </nav>
</template>

<style scoped>
.breadcrumbs {
  margin-bottom: 1.5rem;
  color: var(--color-muted);
  font-family: ui-monospace, monospace;
  font-size: 0.68rem;
  font-weight: 900;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.breadcrumbs a:hover {
  color: var(--color-primary);
}

.breadcrumbs--film {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.breadcrumbs--film [aria-current="page"] {
  color: #27272a;
}
</style>
