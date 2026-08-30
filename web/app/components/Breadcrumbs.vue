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
    class="mb-6 font-mono text-[0.68rem] font-black uppercase tracking-[0.1em] text-muted [&_a:hover]:text-primary"
    :class="variant === 'film' ? 'text-xs font-bold tracking-[0.08em] [&_[aria-current=page]]:text-ink' : undefined"
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
