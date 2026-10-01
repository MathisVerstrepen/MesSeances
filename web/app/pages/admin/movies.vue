<script setup lang="ts">
import { ArrowLeft, LoaderCircle } from '@lucide/vue'

definePageMeta({ middleware: 'admin-auth' })

useHead({ title: 'Métadonnées des films - MesSeances' })
</script>

<template>
  <main class="mx-auto max-w-[1600px] px-4 py-8 sm:px-6 sm:py-10 lg:px-8">
    <div
      class="flex flex-col gap-4 border-b-2 border-ink pb-6 sm:flex-row sm:items-center sm:justify-between"
    >
      <div>
        <NuxtLink
          to="/admin"
          class="mb-1 inline-flex min-h-11 items-center gap-1 font-mono text-xs font-bold text-ink underline underline-offset-4 hover:text-primary"
        >
          <ArrowLeft :size="16" aria-hidden="true" />
          Administration
        </NuxtLink>
        <h1 class="editorial-title">Métadonnées des films</h1>
      </div>
    </div>

    <ClientOnly>
      <AdminMoviesGrid class="mt-5" />
      <template #fallback>
        <EditorialStatePanel
          class="mt-5"
          semantic="status"
          live="polite"
          size="compact"
          shadow="small"
        >
          <LoaderCircle
            :size="28"
            class="animate-spin text-accent"
            aria-hidden="true"
          />
          <p>Chargement de l’éditeur…</p>
        </EditorialStatePanel>
      </template>
    </ClientOnly>
  </main>
</template>
