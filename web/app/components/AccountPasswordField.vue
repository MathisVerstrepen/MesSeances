<script setup lang="ts">
import { passwordCriteria } from '~/utils/accountState'

const props = withDefaults(
  defineProps<{
    id: string
    label?: string
    newPassword?: boolean
    disabled?: boolean
  }>(),
  { label: 'Mot de passe', newPassword: false, disabled: false },
)
const model = defineModel<string>({ required: true })
const visible = ref(false)
const touched = ref(false)
const criteria = computed(() => passwordCriteria(model.value))
const invalid = computed(
  () =>
    props.newPassword &&
    touched.value &&
    (!criteria.value.minimum || !criteria.value.maximum),
)
</script>

<template>
  <div>
    <label :for="id" class="mb-2 block text-sm font-bold">{{ label }}</label>
    <div class="flex gap-2">
      <input
        :id="id"
        v-model="model"
        :type="visible ? 'text' : 'password'"
        :autocomplete="newPassword ? 'new-password' : 'current-password'"
        required
        :disabled="disabled"
        :aria-invalid="invalid || undefined"
        :aria-describedby="newPassword ? `${id}-criteria` : undefined"
        class="min-h-12 min-w-0 flex-1 rounded border border-ink/60 bg-surface px-3 disabled:opacity-60"
        @blur="touched = true"
      >
      <button
        type="button"
        class="min-h-12 min-w-20 rounded border border-ink/60 px-3 text-sm font-semibold"
        :aria-controls="id"
        :aria-pressed="visible"
        :aria-label="`${visible ? 'Masquer' : 'Afficher'} le mot de passe`"
        @click="visible = !visible"
      >
        {{ visible ? 'Masquer' : 'Afficher' }}
      </button>
    </div>
    <ul
      v-if="newPassword"
      :id="`${id}-criteria`"
      class="mt-3 space-y-1 text-xs leading-relaxed"
      :class="invalid ? 'text-primary' : 'text-ink'"
    >
      <li>{{ criteria.minimum ? '✓' : '○' }} Au moins 10 caractères</li>
      <li>{{ criteria.maximum ? '✓' : '○' }} Au plus 128 caractères</li>
    </ul>
  </div>
</template>
