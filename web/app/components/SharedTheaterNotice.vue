<script setup lang="ts">
const { pending, errorMessage, restoreSavedTheaters } = useSharedTheaterRestoration()
</script>

<template>
  <div class="flex flex-wrap items-center gap-x-4 gap-y-3 border-2 border-ink bg-surface px-4 py-3 text-sm font-bold text-ink" role="status" aria-live="polite">
    <p class="basis-96 grow">Cette page utilise les cinémas partagés, différents de votre sélection.</p>
    <button type="button" class="min-h-11 border-2 border-ink bg-surface px-[0.8rem] py-[0.6rem] font-mono text-[0.65rem] font-black tracking-[0.08em] uppercase enabled:hover:bg-[#ffcf3f] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-wait disabled:opacity-65" :disabled="pending" :aria-busy="pending" @click="restoreSavedTheaters">
      {{ pending ? 'Restauration…' : 'Utiliser mes cinémas' }}
    </button>
    <p v-if="errorMessage" class="basis-full text-primary" role="alert">{{ errorMessage }}</p>
  </div>
</template>
