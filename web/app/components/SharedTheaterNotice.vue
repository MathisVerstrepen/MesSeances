<script setup lang="ts">
const { pending, errorMessage, restoreSavedTheaters } = useSharedTheaterRestoration()
</script>

<template>
  <div class="grid gap-2 border-2 border-ink bg-surface px-3 py-2 text-[0.8rem] leading-snug font-bold text-ink min-[400px]:grid-cols-[minmax(0,1fr)_auto] min-[400px]:items-center sm:flex sm:flex-wrap sm:gap-x-4 sm:gap-y-3 sm:px-4 sm:py-3 sm:text-sm sm:leading-normal" role="status" aria-live="polite">
    <p class="min-w-0 sm:basis-96 sm:grow">Cette page utilise les cinémas différents de votre sélection.</p>
    <button type="button" class="min-h-9 w-fit border-2 border-ink bg-surface px-2 py-1 font-mono text-[0.58rem] font-black tracking-[0.06em] uppercase enabled:hover:bg-[#ffcf3f] focus-visible:outline-[3px] focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-wait disabled:opacity-65 sm:min-h-11 sm:px-[0.8rem] sm:py-[0.6rem] sm:text-[0.65rem] sm:tracking-[0.08em]" :disabled="pending" :aria-busy="pending" @click="restoreSavedTheaters">
      {{ pending ? 'Restauration…' : 'Utiliser mes cinémas' }}
    </button>
    <p v-if="errorMessage" class="col-span-full text-primary sm:basis-full" role="alert">{{ errorMessage }}</p>
  </div>
</template>
