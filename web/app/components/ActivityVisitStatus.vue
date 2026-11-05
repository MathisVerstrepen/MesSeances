<script setup lang="ts">
defineProps<{ failed: boolean }>()
defineEmits<{ retry: [] }>()
</script>

<template>
  <p v-if="failed" role="status" class="mt-6 max-w-prose text-sm text-muted">
    Impossible de mémoriser votre visite.
    <button
      type="button"
      class="min-h-11 font-bold underline underline-offset-4"
      @click="$emit('retry')"
    >
      Réessayer
    </button>
  </p>
  <p v-else class="mt-6 max-w-prose text-sm text-muted">
    « Nouveau » indique une activité ajoutée depuis votre dernière visite de
    cette page.
  </p>
</template>
