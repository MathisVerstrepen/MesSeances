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
    <a
      v-if="account.session.value?.account"
      href="/compte"
      class="account-primary"
      >Revenir à mon compte</a
    >
    <AccountCredentialsForm v-else />
  </AccountShell>
</template>
