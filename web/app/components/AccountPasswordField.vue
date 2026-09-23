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
    <label :for="id" class="account-label">{{ label }}</label>
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
        class="account-input w-full flex-1"
        @blur="touched = true"
      >
      <button
        type="button"
        class="account-secondary shrink-0"
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
