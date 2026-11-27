<script setup lang="ts">
import type { DigestFrequency } from '~/types/notifications'

const props = defineProps<{
  state: ReturnType<typeof useNotificationDigest>
}>()
const digest = props.state
const account = useAccountSession()
const uid = useId()
const options: { value: DigestFrequency; label: string; hint: string }[] = [
  { value: 'off', label: 'Désactivé', hint: 'Aucun e-mail.' },
  {
    value: 'daily',
    label: 'Chaque matin',
    hint: 'Vers 8 h, seulement s’il y a du nouveau.',
  },
  {
    value: 'weekly',
    label: 'Chaque mercredi',
    hint: 'Le jour des sorties, vers 8 h, seulement s’il y a du nouveau.',
  },
]
const next = computed(() => {
  const at = digest.digest.value?.next_due_at
  if (!at) return ''
  return new Intl.DateTimeFormat('fr-FR', {
    timeZone: 'Europe/Paris',
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(at))
})
</script>

<template>
  <section
    v-if="digest.digest.value?.available"
    aria-labelledby="digest-heading"
    class="space-y-4 border-t-2 border-ink pt-6"
    :aria-busy="digest.saving.value"
  >
    <h2 id="digest-heading" class="account-heading">Résumé par e-mail</h2>
    <p class="max-w-prose text-sm leading-relaxed">
      Recevez les notifications non lues des alertes marquées « E-mail »,
      regroupées dans un seul message.
    </p>
    <fieldset
      class="space-y-2"
      :disabled="digest.saving.value || account.writesBlocked.value"
    >
      <legend class="sr-only">Fréquence du résumé</legend>
      <label
        v-for="option in options"
        :key="option.value"
        class="account-choice"
      >
        <input
          type="radio"
          :name="`${uid}-digest`"
          :value="option.value"
          :checked="digest.digest.value.frequency === option.value"
          @change="digest.save(option.value)"
        >
        <span>
          {{ option.label }}
          <span class="mt-0.5 block text-xs font-normal text-muted">{{
            option.hint
          }}</span>
        </span>
      </label>
    </fieldset>
    <p v-if="next" class="text-sm text-muted">
      Prochain envoi possible : {{ next }}.
    </p>
    <p v-if="digest.error.value" role="alert" class="account-alert">
      {{ digest.error.value }}
    </p>
  </section>
</template>
