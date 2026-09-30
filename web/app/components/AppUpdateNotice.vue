<script setup lang="ts">
const { $appUpdate } = useNuxtApp()
</script>

<template>
  <div
    v-if="$appUpdate.state.available"
    class="pointer-events-none fixed inset-x-0 bottom-[calc(1rem+env(safe-area-inset-bottom))] z-40 flex justify-center px-4"
  >
    <div
      class="pointer-events-auto flex w-fit max-w-full flex-wrap items-center gap-x-4 gap-y-2 rounded-[0.35rem] border-2 border-ink bg-[#f8f7f2] px-3 py-2 text-ink shadow-[3px_3px_0_#27272a] sm:max-w-md sm:px-4"
      :aria-busy="$appUpdate.state.busy"
    >
      <p
        role="status"
        aria-live="polite"
        class="min-w-0 flex-1 break-words font-mono text-xs font-extrabold uppercase tracking-[0.08em]"
      >
        Nouvelle version disponible
      </p>
      <button
        type="button"
        class="inline-flex min-h-11 shrink-0 items-center justify-center rounded-[0.35rem] bg-ink px-4 font-mono text-[0.72rem] font-extrabold uppercase tracking-[0.08em] text-white transition-colors hover:bg-primary focus-visible:ring-ink focus-visible:ring-offset-[#f8f7f2] disabled:cursor-wait disabled:opacity-60"
        :disabled="$appUpdate.state.busy"
        @click="$appUpdate.refresh()"
      >
        {{ $appUpdate.state.busy ? 'Actualisation…' : 'Actualiser' }}
      </button>
      <p
        v-if="$appUpdate.state.error"
        role="status"
        aria-live="polite"
        class="w-full break-words text-sm font-medium text-primary"
      >
        {{ $appUpdate.state.error }}
      </p>
    </div>
  </div>
</template>
