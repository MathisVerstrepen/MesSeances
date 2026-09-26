<script setup lang="ts">
import { accountGoogleCallbackMessage } from '~/utils/accountState'

definePageMeta({ middleware: 'account-auth' })
const route = useRoute()
const account = useAccountSession()
const providerError = computed(() =>
  accountGoogleCallbackMessage(route.query.error),
)
useHead({ title: 'Connexion - MesSeances' })
</script>

<template>
  <AccountShell title="Connexion" compact>
    <p v-if="providerError" role="alert" class="account-alert mb-5">
      {{ providerError }}
    </p>
    <NuxtLink
      v-if="account.session.value?.account"
      to="/compte"
      :prefetch="false"
      class="account-primary"
      >Revenir à mon compte</NuxtLink
    >
    <AccountCredentialsForm v-else />
  </AccountShell>
</template>
