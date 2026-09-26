<script setup lang="ts">
import { Eye, EyeOff } from '@lucide/vue'
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
    <div
      v-if="$slots['label-action']"
      class="account-password-label-row mb-2 flex flex-wrap items-baseline justify-between gap-x-4"
    >
      <label :for="id" class="account-label">{{ label }}</label>
      <slot name="label-action" />
    </div>
    <label v-else :for="id" class="account-label">{{ label }}</label>
    <div class="relative">
      <input
        :id="id"
        v-model="model"
        :type="visible ? 'text' : 'password'"
        :autocomplete="newPassword ? 'new-password' : 'current-password'"
        required
        :disabled="disabled"
        :aria-invalid="invalid || undefined"
        :aria-describedby="newPassword ? `${id}-criteria` : undefined"
        class="account-input account-password-input w-full"
        @blur="touched = true"
      >
      <button
        type="button"
        class="absolute right-0.5 top-1/2 flex size-11 -translate-y-1/2 items-center justify-center text-ink hover:bg-highlight focus-visible:outline-solid"
        :aria-controls="id"
        :aria-pressed="visible"
        :aria-label="`${visible ? 'Masquer' : 'Afficher'} le mot de passe`"
        @click="visible = !visible"
      >
        <EyeOff v-if="visible" class="size-5" aria-hidden="true" />
        <Eye v-else class="size-5" aria-hidden="true" />
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
