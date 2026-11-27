<script setup lang="ts">
import { Search, X } from '@lucide/vue'
import type {
  NotificationKind,
  NotificationRuleDraft,
  NotificationScope,
} from '~/types/notifications'
import {
  filterNotificationTheaters,
  notificationKindLabel,
} from '~/utils/notifications'

const props = defineProps<{
  initial: NotificationRuleDraft
  editing: boolean
  saving: boolean
  disabled: boolean
  maxCustomTheaters: number
  followedCount: number
  preferredCount: number
  pushAvailable?: boolean
  pushDeviceCount?: number
  theaters: readonly { id: string; name: string; city: string }[]
}>()
const emit = defineEmits<{
  submit: [rule: NotificationRuleDraft]
  cancel: []
}>()

const kind = ref<NotificationKind>(props.initial.kind)
const scope = ref<NotificationScope>(props.initial.theater_scope)
const custom = ref<string[]>([...props.initial.theater_ids])
const includeReturns = ref(props.initial.include_returns)
const push = ref(props.initial.push)
const query = ref('')
const formError = ref('')
const uid = useId()

const byId = computed(
  () => new Map(props.theaters.map((theater) => [theater.id, theater])),
)
const results = computed(() =>
  filterNotificationTheaters(props.theaters, query.value, 20),
)
const selected = computed(() =>
  custom.value.map(
    (id) => byId.value.get(id) ?? { id, name: id, city: 'Cinéma indisponible' },
  ),
)
const customFull = computed(
  () => custom.value.length >= props.maxCustomTheaters,
)
const scopeWarning = computed(() => {
  if (scope.value === 'followed' && props.followedCount === 0)
    return 'Vous ne suivez encore aucun cinéma : cette alerte ne se déclenchera pas tant que vous n’en suivez pas.'
  if (scope.value === 'preferred' && props.preferredCount === 0)
    return 'Aucun cinéma n’est sélectionné : cette alerte ne se déclenchera pas tant que votre sélection est vide.'
  return ''
})

function toggleTheater(id: string, checked: boolean) {
  formError.value = ''
  if (!checked) {
    custom.value = custom.value.filter((value) => value !== id)
    return
  }
  if (custom.value.includes(id) || customFull.value) return
  custom.value = [...custom.value, id]
}

function submit() {
  if (scope.value === 'custom' && custom.value.length === 0) {
    formError.value = 'Choisissez au moins un cinéma.'
    return
  }
  emit('submit', {
    kind: kind.value,
    theater_scope: scope.value,
    theater_ids: scope.value === 'custom' ? [...custom.value] : [],
    include_returns: kind.value === 'cinema_activity' && includeReturns.value,
    enabled: props.initial.enabled,
    push: !!props.pushAvailable && push.value,
  })
}
</script>

<template>
  <form
    class="space-y-6 border-2 border-ink bg-surface p-4 shadow-[4px_4px_0_#27272a] sm:p-6"
    :aria-busy="saving"
    @submit.prevent="submit"
  >
    <fieldset class="space-y-2" :disabled="editing || disabled">
      <legend class="account-label">Type d’alerte</legend>
      <label
        v-for="option in (['cinema_activity', 'watchlist_screening'] as const)"
        :key="option"
        class="account-choice"
      >
        <input
          v-model="kind"
          type="radio"
          :name="`${uid}-kind`"
          :value="option"
        >
        <span>
          {{ notificationKindLabel(option) }}
          <span class="mt-0.5 block text-xs font-normal text-muted">{{
            option === 'cinema_activity'
              ? 'Chaque nouveau film programmé dans les cinémas choisis.'
              : 'La première séance d’un film de votre watchlist dans les cinémas choisis.'
          }}</span>
        </span>
      </label>
      <p v-if="editing" class="text-xs text-muted">
        Le type d’une alerte existante ne peut pas être modifié.
      </p>
    </fieldset>

    <fieldset class="space-y-2" :disabled="disabled">
      <legend class="account-label">Cinémas concernés</legend>
      <label class="account-choice">
        <input
          v-model="scope"
          type="radio"
          :name="`${uid}-scope`"
          value="followed"
        >
        <span
          >Mes cinémas suivis
          <span class="font-normal text-muted"
            >({{ followedCount }})</span
          ></span
        >
      </label>
      <label class="account-choice">
        <input
          v-model="scope"
          type="radio"
          :name="`${uid}-scope`"
          value="preferred"
        >
        <span
          >Mes cinémas sélectionnés
          <span class="font-normal text-muted"
            >({{ preferredCount }})</span
          ></span
        >
      </label>
      <label class="account-choice">
        <input
          v-model="scope"
          type="radio"
          :name="`${uid}-scope`"
          value="custom"
        >
        <span>Une sélection personnalisée</span>
      </label>
      <p v-if="scopeWarning" role="status">{{ scopeWarning }}</p>
    </fieldset>

    <div v-if="scope === 'custom'" class="space-y-3">
      <label :for="`${uid}-search`" class="account-label"
        >Rechercher un cinéma ({{ custom.length }}/{{
          maxCustomTheaters
        }})</label
      >
      <div class="relative">
        <Search
          :size="18"
          class="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
          aria-hidden="true"
        />
        <input
          :id="`${uid}-search`"
          v-model="query"
          type="search"
          autocomplete="off"
          class="account-input w-full pl-10!"
          placeholder="Nom ou ville"
          :disabled="disabled"
        >
      </div>
      <ul
        v-if="selected.length"
        aria-label="Cinémas choisis"
        class="flex flex-wrap gap-2"
      >
        <li v-for="theater in selected" :key="theater.id">
          <button
            type="button"
            class="inline-flex min-h-9 items-center gap-1.5 border-2 border-ink bg-highlight px-2 text-xs font-semibold hover:bg-primary-soft"
            :aria-label="`Retirer ${theater.name}`"
            :disabled="disabled"
            @click="toggleTheater(theater.id, false)"
          >
            {{ theater.name }}
            <X :size="14" aria-hidden="true" />
          </button>
        </li>
      </ul>
      <ul
        v-if="results.length"
        aria-label="Résultats"
        class="max-h-64 divide-y divide-ink/15 overflow-y-auto border-2 border-ink/30"
      >
        <li v-for="theater in results" :key="theater.id">
          <label
            class="flex min-h-11 cursor-pointer items-center gap-3 px-3 py-2 text-sm hover:bg-subtle has-disabled:cursor-not-allowed has-disabled:opacity-60"
          >
            <input
              type="checkbox"
              class="size-4 shrink-0 accent-ink"
              :checked="custom.includes(theater.id)"
              :disabled="disabled || (customFull && !custom.includes(theater.id))"
              @change="toggleTheater(theater.id, ($event.target as HTMLInputElement).checked)"
            >
            <span class="min-w-0">
              <span class="font-semibold">{{ theater.name }}</span>
              <span class="text-muted"> · {{ theater.city }}</span>
            </span>
          </label>
        </li>
      </ul>
      <p v-else-if="query.trim()" class="text-sm text-muted">
        Aucun cinéma ne correspond à cette recherche.
      </p>
      <p v-if="customFull" class="text-xs text-muted">
        Nombre maximal de cinémas atteint pour cette alerte.
      </p>
    </div>

    <label
      v-if="kind === 'cinema_activity'"
      class="flex min-h-11 cursor-pointer items-start gap-3 text-sm font-semibold"
    >
      <input
        v-model="includeReturns"
        type="checkbox"
        class="mt-0.5 size-5 shrink-0 accent-ink"
        :disabled="disabled"
      >
      <span>
        Inclure les reprises
        <span class="block text-xs font-normal text-muted"
          >Films de retour à l’affiche après au moins 28 jours sans
          séance.</span
        >
      </span>
    </label>

    <label
      v-if="pushAvailable"
      class="flex min-h-11 cursor-pointer items-start gap-3 text-sm font-semibold"
    >
      <input
        v-model="push"
        type="checkbox"
        class="mt-0.5 size-5 shrink-0 accent-ink"
        :disabled="disabled"
      >
      <span>
        Recevoir aussi en notification push
        <span class="block text-xs font-normal text-muted">{{
          pushDeviceCount
            ? 'Sur les appareils où les notifications sont activées.'
            : 'Activez les notifications sur un appareil, plus bas sur cette page.'
        }}</span>
      </span>
    </label>

    <p v-if="formError" role="alert" class="account-alert">{{ formError }}</p>

    <div class="flex flex-wrap gap-3">
      <button
        type="submit"
        class="account-primary"
        :disabled="disabled || saving"
      >
        {{
          saving ? 'Enregistrement…' : editing ? 'Enregistrer' : 'Créer l’alerte'
        }}
      </button>
      <button
        type="button"
        class="account-secondary"
        :disabled="saving"
        @click="emit('cancel')"
      >
        Annuler
      </button>
    </div>
  </form>
</template>
