<script setup lang="ts">
import type { WatchedPreferences, WatchedSection } from '~/types/watched'
const watched = useWatched()
const draft = ref<WatchedPreferences | null>(null)
const dirty = ref(false)
const saved = ref(false)
const sections: { key: WatchedSection; label: string }[] = [
  { key: 'search', label: 'Recherche' },
  { key: 'planning', label: 'Planning' },
  { key: 'films', label: 'Films à l’affiche' },
  { key: 'upcoming', label: 'Prochaines sorties et historique' },
  { key: 'home', label: 'Accueil' },
  { key: 'cinema_city', label: 'Cinémas et villes' },
]
watch(
  watched.preferences,
  (value) => {
    if (value && !dirty.value) draft.value = { ...value }
  },
  { immediate: true },
)
watch(watched.scopeKey, () => {
  draft.value = null
  dirty.value = false
  saved.value = false
})
function edit() {
  dirty.value = true
  saved.value = false
}
async function submit() {
  if (!draft.value) return
  saved.value = false
  if (await watched.savePreferences({ ...draft.value })) {
    dirty.value = false
    saved.value = true
  }
}
</script>

<template>
  <form
    v-if="draft"
    class="space-y-6"
    aria-label="Préférences des films vus"
    @submit.prevent="submit"
    @change="edit"
  >
    <fieldset :disabled="watched.writesBlocked.value" class="space-y-5">
      <legend class="account-heading mb-4">Visibilité des films vus</legend>
      <fieldset class="min-w-0">
        <legend class="sr-only">Par défaut dans l’application</legend>
        <div
          class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center"
        >
          <span aria-hidden="true" class="font-semibold"
            >Par défaut dans l’application</span
          >
          <div
            class="grid grid-cols-2 justify-self-start divide-x divide-ink border-2 border-ink bg-surface sm:justify-self-end"
          >
            <label
              v-for="choice in (['hide', 'show'] as const)"
              :key="choice"
              class="relative cursor-pointer"
            >
              <input
                v-model="draft.default_visibility"
                type="radio"
                name="watched-default"
                :value="choice"
                class="peer absolute inset-0 z-20 m-0 size-full cursor-pointer opacity-0 disabled:cursor-not-allowed"
              >
              <span
                class="flex min-h-11 min-w-11 items-center justify-center px-3 text-sm font-semibold peer-checked:bg-ink peer-checked:text-white peer-focus-visible:relative peer-focus-visible:z-10 peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-ink peer-disabled:cursor-not-allowed peer-disabled:opacity-55"
                >{{
                  choice === 'hide' ? 'Masquer' : 'Afficher'
                }}</span
              >
            </label>
          </div>
        </div>
      </fieldset>
      <fieldset
        v-for="section in sections"
        :key="section.key"
        class="min-w-0 border-t border-ink/20 pt-3"
      >
        <legend class="sr-only">{{ section.label }}</legend>
        <div
          class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center"
        >
          <span aria-hidden="true" class="font-semibold">{{
            section.label
          }}</span>
          <div
            class="grid grid-cols-3 justify-self-start divide-x divide-ink border-2 border-ink bg-surface sm:justify-self-end"
          >
            <label
              v-for="choice in (['inherit', 'show', 'hide'] as const)"
              :key="choice"
              class="relative cursor-pointer"
            >
              <input
                v-model="draft[`${section.key}_visibility`]"
                type="radio"
                :name="`watched-${section.key}`"
                :value="choice"
                class="peer absolute inset-0 z-20 m-0 size-full cursor-pointer opacity-0 disabled:cursor-not-allowed"
              >
              <span
                class="flex min-h-11 min-w-11 items-center justify-center px-3 text-sm font-semibold peer-checked:bg-ink peer-checked:text-white peer-focus-visible:relative peer-focus-visible:z-10 peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-ink peer-disabled:cursor-not-allowed peer-disabled:opacity-55"
                >{{
                  choice === 'inherit' ? 'Par défaut' : choice === 'show' ? 'Afficher' : 'Masquer'
                }}</span
              >
            </label>
          </div>
        </div>
      </fieldset>
      <label
        class="flex min-h-11 cursor-pointer items-center gap-3 border-t border-ink/20 pt-4 font-semibold"
      >
        <input
          v-model="draft.auto_remove_watchlist"
          type="checkbox"
          class="size-5 shrink-0 accent-ink"
        >
        <span>Retirer de la watchlist quand je marque un film comme vu</span>
      </label>
      <p class="text-sm text-muted">
        Uniquement pour les prochains films marqués comme vus. Marquer comme non
        vu ne restaure pas la watchlist ni ses tags.
      </p>
    </fieldset>
    <button
      type="submit"
      class="account-primary"
      :disabled="watched.writesBlocked.value"
    >
      Enregistrer les préférences
    </button>
    <p v-if="saved" role="status" class="text-sm">Préférences enregistrées.</p>
  </form>
</template>
