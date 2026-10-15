<script setup lang="ts">
const watched = useWatched()
</script>

<template>
  <div
    class="my-5 border-2 border-ink bg-surface p-5"
    :aria-busy="watched.loading.value"
  >
    <template v-if="watched.error.value">
      <p role="alert">
        Impossible de préparer vos résultats. {{ watched.error.value }}
      </p>
      <button
        type="button"
        class="editorial-button-outline mt-3"
        @click="watched.retry"
      >
        Réessayer
      </button>
    </template>
    <template v-else>
      <p role="status" class="sr-only">
        Préparation des résultats selon vos films vus…
      </p>
      <div
        class="h-24 bg-subtle motion-safe:animate-pulse"
        aria-hidden="true"
      />
    </template>
  </div>
</template>
